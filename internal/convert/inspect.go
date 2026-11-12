// Package convert turns Word documents into Markdown and back, and tells
// beforehand what such a conversion would lose.
package convert

import (
	"archive/zip"
	"compress/flate"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

// Loss is something the conversion to Markdown would drop without a trace.
type Loss string

const (
	LossComments       Loss = "comments"
	LossTrackedChanges Loss = "tracked-changes"
	LossTextBoxes      Loss = "text-boxes"

	// LossObjects: a chart, a SmartArt diagram or an embedded (OLE) object.
	// pandoc drops the object itself and keeps only the text around it (an
	// embedded object that has a preview picture comes back as that picture).
	LossObjects Loss = "embedded-objects"

	// LossTables: a table pandoc cannot write in Markdown at all, so it comes
	// out as raw HTML, which is lost when the Markdown is written back to .docx.
	// Only DocxToMarkdown reports it (it takes the pandoc output to know);
	// Inspect never does.
	LossTables Loss = "tables"
)

// InspectLimits bound the work done on one archive. A zero value refuses
// everything; it does not mean "unlimited".
type InspectLimits struct {
	MaxEntries   int   // archive entries; more is refused
	MaxPartBytes int64 // uncompressed bytes read from one XML part; more is refused
}

var (
	// ErrNotDocx: not a zip, no word/document.xml, or an XML part that is not well formed.
	ErrNotDocx = errors.New("convert: not a Word document")
	// ErrTooLarge: an InspectLimits value was exceeded.
	ErrTooLarge = errors.New("convert: document exceeds a limit")
)

// The two WordprocessingML namespaces: the transitional one Word writes and
// the strict one of ISO 29500.
const (
	nsTransitional = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsStrict       = "http://purl.oclc.org/ooxml/wordprocessingml/main"
)

// DrawingML namespaces of the objects that live in a w:drawing: transitional
// and strict (ISO 29500) spelling of the chart and the diagram (SmartArt) ones.
const (
	nsChart         = "http://schemas.openxmlformats.org/drawingml/2006/chart"
	nsChartStrict   = "http://purl.oclc.org/ooxml/drawingml/chart"
	nsDiagram       = "http://schemas.openxmlformats.org/drawingml/2006/diagram"
	nsDiagramStrict = "http://purl.oclc.org/ooxml/drawingml/diagram"
)

// maxXMLDepth bounds element nesting. encoding/xml keeps a stack entry per open
// element, so without it a few hundred MiB of "<a>" would cost gigabytes of
// memory before MaxPartBytes is reached. Real documents nest a few dozen
// levels at most. It is a safety net for hostile input, not a user-facing
// limit, so it is not in internal/config.
const maxXMLDepth = 1024

// lossSet is a bit set of what has been found so far.
type lossSet uint8

const (
	hasComments lossSet = 1 << iota
	hasTrackedChanges
	hasTextBoxes
	hasObjects

	hasAll = hasComments | hasTrackedChanges | hasTextBoxes | hasObjects
)

// reportOrder is the fixed order of the result.
var reportOrder = [...]struct {
	bit  lossSet
	loss Loss
}{
	{hasComments, LossComments},
	{hasTrackedChanges, LossTrackedChanges},
	{hasTextBoxes, LossTextBoxes},
	{hasObjects, LossObjects},
}

// classify maps the local name of a WordprocessingML element to the loss it
// stands for, or to 0.
func classify(local string) lossSet {
	switch local {
	case "comment":
		return hasComments
	case "ins", "del", "moveFrom", "moveTo",
		"rPrChange", "pPrChange", "sectPrChange", "tblPrChange",
		"tblGridChange", "trPrChange", "tcPrChange", "numberingChange":
		return hasTrackedChanges
	case "txbxContent":
		return hasTextBoxes
	case "object":
		return hasObjects
	}
	return 0
}

// classifyDrawing is classify for the elements of the DrawingML namespaces: the
// reference to a chart is c:chart, the one to a SmartArt diagram is dgm:relIds.
func classifyDrawing(space, local string) lossSet {
	switch {
	case local == "chart" && (space == nsChart || space == nsChartStrict),
		local == "relIds" && (space == nsDiagram || space == nsDiagramStrict):
		return hasObjects
	}
	return 0
}

// Report is what Examine found in a document.
type Report struct {
	// Losses is what the conversion would lose; the document is opened read-only
	// because of them. In the fixed order comments, tracked-changes, text-boxes,
	// embedded-objects, without duplicates; nil when there is nothing.
	Losses []Loss
	// Simplified is the direct formatting the conversion would flatten. It does
	// not block anything: it is the warning that saving simplifies the document.
	// In the order of the Simplification constants, without duplicates; nil when
	// there is nothing.
	Simplified []Simplification
}

// Inspect reports what the document contains that the conversion would lose.
// The result is in the fixed order comments, tracked-changes, text-boxes,
// embedded-objects, without duplicates; it is nil when there is nothing. It
// stops reading as soon as everything is found; Examine is for the caller that
// also wants the formatting that would be simplified.
//
// The archive is only read, never extracted, and nothing in it is trusted:
// sizes come from the bytes actually read, and entry names are only compared.
func Inspect(ctx context.Context, path string, lim InspectLimits) ([]Loss, error) {
	r, err := examine(ctx, path, lim, false)
	return r.Losses, err
}

// Examine is Inspect plus the formatting that saving would simplify, in one
// pass over the archive. The formatting is read from word/document.xml only:
// what headers and styles carry survives through the reference document.
func Examine(ctx context.Context, path string, lim InspectLimits) (Report, error) {
	return examine(ctx, path, lim, true)
}

func examine(ctx context.Context, path string, lim InspectLimits, wantSimplified bool) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, fmt.Errorf("convert: inspect: %w", err)
	}

	// Stat first: opening a FIFO would block until somebody writes to it.
	info, err := os.Stat(path)
	if err != nil {
		return Report{}, fmt.Errorf("convert: inspect: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Report{}, fmt.Errorf("%w: not a regular file", ErrNotDocx)
	}
	f, err := os.Open(path)
	if err != nil {
		return Report{}, fmt.Errorf("convert: inspect: %w", err)
	}
	defer func() { _ = f.Close() }() // read-only handle: a failed Close loses nothing

	fi, err := f.Stat()
	if err != nil {
		return Report{}, fmt.Errorf("convert: inspect: %w", err)
	}
	zr, err := zip.NewReader(sanePositions{f}, fi.Size())
	// ErrInsecurePath comes with a usable reader; names are never used as paths here.
	if errors.Is(err, zip.ErrInsecurePath) && zr != nil {
		err = nil
	}
	if err != nil {
		return Report{}, wrapRead("open archive", err)
	}
	if len(zr.File) > lim.MaxEntries {
		return Report{}, fmt.Errorf("%w: %d archive entries, limit %d", ErrTooLarge, len(zr.File), lim.MaxEntries)
	}

	var parts []*zip.File
	var document *zip.File
	for _, e := range zr.File {
		name := strings.ToLower(e.Name) // part names are case-insensitive in OPC
		rest, ok := strings.CutPrefix(name, "word/")
		if !ok || rest == "" || strings.Contains(rest, "/") || !strings.HasSuffix(rest, ".xml") {
			continue
		}
		if rest == "document.xml" && document == nil {
			document = e
			continue
		}
		parts = append(parts, e)
	}
	if document == nil {
		return Report{}, fmt.Errorf("%w: no word/document.xml", ErrNotDocx)
	}
	// The document goes first: it is the only part the formatting is read from,
	// so reading it first is what lets the others be skipped once all losses are found.
	parts = append([]*zip.File{document}, parts...)

	st := scanState{wantSimplified: wantSimplified}
	for _, p := range parts {
		if err := ctx.Err(); err != nil {
			return Report{}, fmt.Errorf("convert: inspect: %w", err)
		}
		if err := st.scanPart(ctx, p, lim.MaxPartBytes, p == document); err != nil {
			return Report{}, err
		}
		if st.losses == hasAll {
			break // the document was read first, so nothing is left to look for
		}
	}

	var rep Report
	for _, r := range reportOrder {
		if st.losses&r.bit != 0 {
			rep.Losses = append(rep.Losses, r.loss)
		}
	}
	if st.doc != nil {
		rep.Simplified = st.doc.result()
	}
	return rep, nil
}

// scanState is what one pass over the archive has found so far.
type scanState struct {
	wantSimplified bool
	losses         lossSet
	doc            *docScanner // nil unless the formatting is wanted; filled while word/document.xml is read
}

// scanPart reads one XML part as a token stream and records what it finds. It
// returns early only when there is nothing more to find in this part.
func (st *scanState) scanPart(ctx context.Context, zf *zip.File, maxBytes int64, isDocument bool) error {
	rc, err := zf.Open()
	if err != nil {
		return wrapRead(zf.Name, err)
	}
	defer func() { _ = rc.Close() }() // read-only; nothing to lose on a failed Close

	var ds *docScanner
	if isDocument && st.wantSimplified {
		ds = newDocScanner()
		st.doc = ds
	}
	// complete: nothing in this part can change the result any more.
	complete := func() bool { return st.losses == hasAll && (ds == nil || ds.complete()) }
	if complete() {
		return nil
	}

	pr := newPartReader(ctx, rc, maxBytes)
	dec := xml.NewDecoder(pr)
	// Only UTF-8 (which the decoder handles itself) is understood. Any other
	// declared encoding fails closed instead of risking a loss that goes unseen.
	dec.CharsetReader = func(label string, _ io.Reader) (io.Reader, error) {
		return nil, fmt.Errorf("%w %q", errEncoding, label)
	}
	depth := 0
	sawRoot := false
	for {
		tok, err := dec.Token()
		if err != nil {
			// The limit and the context win over whatever the decoder made of
			// the error: it may turn a failed read into a syntax error.
			if pr.tooLarge {
				return fmt.Errorf("%w: %s is larger than %d bytes", ErrTooLarge, zf.Name, maxBytes)
			}
			if cerr := ctx.Err(); cerr != nil {
				return fmt.Errorf("convert: inspect: %w", cerr)
			}
			if errors.Is(err, io.EOF) { // Token returns io.EOF at the end of input
				if !sawRoot {
					return fmt.Errorf("%w: %s has no root element", ErrNotDocx, zf.Name)
				}
				return nil
			}
			if pr.readErr != nil {
				return wrapRead(zf.Name, pr.readErr)
			}
			// The bytes came out fine, so the decoder is the one that objects:
			// malformed XML, an unsupported encoding or XML version, and so on.
			return fmt.Errorf("%w: %s: %w", ErrNotDocx, zf.Name, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth > maxXMLDepth {
				return fmt.Errorf("%w: %s nests deeper than %d elements", ErrTooLarge, zf.Name, maxXMLDepth)
			}
			sawRoot = true
			switch t.Name.Space {
			case nsTransitional, nsStrict:
				st.losses |= classify(t.Name.Local)
			default:
				st.losses |= classifyDrawing(t.Name.Space, t.Name.Local)
			}
			if ds != nil {
				ds.start(t)
			}
			if complete() {
				return nil
			}
		case xml.CharData:
			if ds != nil {
				ds.text(t)
			}
		case xml.EndElement:
			depth--
			if ds != nil {
				ds.end()
			}
		}
	}
}

// wrapRead sorts an error of reading the archive: a damaged archive is
// ErrNotDocx, anything else (a failing disk) stays a plain error.
func wrapRead(what string, err error) error {
	var corrupt flate.CorruptInputError
	switch {
	case errors.Is(err, zip.ErrFormat), errors.Is(err, zip.ErrAlgorithm), errors.Is(err, zip.ErrChecksum),
		errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF),
		errors.As(err, &corrupt):
		return fmt.Errorf("%w: %s: %w", ErrNotDocx, what, err)
	}
	return fmt.Errorf("convert: inspect %s: %w", what, err)
}

var errPartTooLarge = errors.New("convert: part too large")

// errEncoding is what CharsetReader answers; it never leaves this package.
var errEncoding = errors.New("unsupported XML encoding")

// partReader hands bytes to the XML decoder and enforces the part limit on the
// bytes actually decompressed, not on the size the archive claims. It also
// checks the context on every read, which is how a long part stays cancellable.
type partReader struct {
	ctx      context.Context
	r        io.Reader
	left     int64 // bytes that may still be read, plus one so that "too much" is detectable
	tooLarge bool
	readErr  error // first error of the underlying reader other than io.EOF
}

func newPartReader(ctx context.Context, r io.Reader, maxBytes int64) *partReader {
	left := maxBytes
	if left < math.MaxInt64 {
		left++
	}
	return &partReader{ctx: ctx, r: r, left: left}
}

func (p *partReader) Read(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	if len(b) == 0 {
		return 0, nil
	}
	if p.left <= 0 {
		p.tooLarge = true
		return 0, errPartTooLarge
	}
	if int64(len(b)) > p.left {
		b = b[:p.left]
	}
	n, err := p.r.Read(b)
	if err != nil && !errors.Is(err, io.EOF) && p.readErr == nil {
		p.readErr = err
	}
	p.left -= int64(n)
	if p.left <= 0 {
		// One byte more than allowed has been read.
		p.tooLarge = true
		return 0, errPartTooLarge
	}
	return n, err
}

// sanePositions turns an offset that no file can have into a format error. A
// hostile archive can carry an entry offset above MaxInt64; archive/zip passes
// it on as a negative number and os.File answers with a bare error, which would
// otherwise look like a failing disk.
type sanePositions struct{ io.ReaderAt }

func (s sanePositions) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("%w: negative offset", zip.ErrFormat)
	}
	return s.ReaderAt.ReadAt(p, off)
}
