package convert

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// nsPackageRels is the namespace of the elements of a .rels part.
const nsPackageRels = "http://schemas.openxmlformats.org/package/2006/relationships"

const (
	documentPart     = "word/document.xml"
	documentRelsPart = "word/_rels/document.xml.rels"
	bodyMediaPrefix  = "word/media/"
)

// relationship is one Relationship element of a .rels part.
type relationship struct {
	target     string // resolved part name, lower case; empty for an external target
	image      bool   // Type ends with /image
	topLevel   bool   // a direct child of a Relationships root in the package namespace
	start, end int    // byte range of the whole element in the part
}

// MakeReference writes dst: a copy of src without the images of the document body.
//
// pandoc carries every image of the reference document into each output, so
// the reference must not have the body's images, or the picture count grows
// with every save. Images of headers and footers have to stay: pandoc keeps
// those parts and their relationships, and a reference without the picture
// leaves a relationship to nothing. The archive is read and written, never
// extracted, and the entries that are kept are copied without being
// decompressed.
func (c *Converter) MakeReference(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("convert: %w", err)
	}
	srcAbs, err := absPath(src)
	if err != nil {
		return err
	}
	dstAbs, err := absPath(dst)
	if err != nil {
		return err
	}
	if dstAbs == srcAbs {
		return fmt.Errorf("convert: output %s is also the input", dstAbs)
	}
	if _, err := checkRegular(srcAbs); err != nil {
		return err
	}
	if err := checkOutputFree(dstAbs); err != nil {
		return err
	}

	in, err := os.Open(srcAbs)
	if err != nil {
		return fmt.Errorf("convert: %w", err)
	}
	defer func() { _ = in.Close() }() // read-only handle: a failed Close loses nothing

	fi, err := in.Stat()
	if err != nil {
		return fmt.Errorf("convert: %w", err)
	}
	zr, err := zip.NewReader(sanePositions{in}, fi.Size())
	// ErrInsecurePath comes with a usable reader; entry names are only compared.
	if errors.Is(err, zip.ErrInsecurePath) && zr != nil {
		err = nil
	}
	if err != nil {
		return wrapRead("open archive", err)
	}
	if len(zr.File) > c.limits.MaxEntries {
		return fmt.Errorf("%w: %d archive entries, limit %d", ErrTooLarge, len(zr.File), c.limits.MaxEntries)
	}

	plan, err := c.planReference(ctx, zr)
	if err != nil {
		return err
	}

	err = c.writeReference(ctx, zr, plan, dstAbs)
	if err == nil {
		err = c.checkDocx(ctx, dstAbs)
	}
	if err != nil {
		return removePartial(dstAbs, err)
	}
	return nil
}

// referencePlan says what leaves the copy.
type referencePlan struct {
	omit      map[string]bool // lower-case names of the entries left out
	rels      *zip.File       // word/_rels/document.xml.rels, when it changes
	relsBytes []byte          // its new content
}

// planReference reads every relationships part and decides which body images
// no other part needs.
func (c *Converter) planReference(ctx context.Context, zr *zip.Reader) (referencePlan, error) {
	var plan referencePlan

	hasDocument := false
	var docRels *zip.File
	for _, e := range zr.File {
		switch strings.ToLower(e.Name) {
		case documentPart:
			hasDocument = true
		case documentRelsPart:
			if docRels == nil {
				docRels = e
			}
		}
	}
	if !hasDocument {
		return plan, fmt.Errorf("%w: no %s", ErrNotDocx, documentPart)
	}

	// body: targets of internal image relationships of the document body.
	// kept: targets of every other relationship in the package. A picture in
	// both is shared and stays. Parts other than the document's are read from
	// every folder, not only word/_rels: a chart or a diagram has its own
	// pictures, and wrongly removing one costs more than keeping a spare.
	body := map[string]bool{}
	kept := map[string]bool{}
	var docRelsList []relationship
	var docRelsData []byte

	for _, e := range zr.File {
		if err := ctx.Err(); err != nil {
			return plan, fmt.Errorf("convert: %w", err)
		}
		base, ok := relsBase(e.Name)
		if !ok {
			continue
		}
		data, rels, err := readRelationships(ctx, e, base, c.limits.MaxPartBytes)
		if err != nil {
			return plan, err
		}
		if e == docRels {
			docRelsList, docRelsData = rels, data
			for _, r := range rels {
				switch {
				case r.target == "":
				case r.image && r.topLevel:
					body[r.target] = true
				default:
					kept[r.target] = true
				}
			}
			continue
		}
		for _, r := range rels {
			if r.target != "" {
				kept[r.target] = true
			}
		}
	}

	plan.omit = map[string]bool{}
	for _, e := range zr.File {
		name := strings.ToLower(e.Name)
		if strings.HasPrefix(name, bodyMediaPrefix) && !strings.HasSuffix(name, "/") &&
			body[name] && !kept[name] {
			plan.omit[name] = true
		}
	}
	if len(plan.omit) == 0 || docRels == nil {
		return plan, nil
	}

	// Cut the relationships of the left-out pictures out of the part as bytes,
	// so everything else in it, unknown attributes included, stays as it was.
	var cuts [][2]int
	for _, r := range docRelsList {
		if r.image && r.topLevel && plan.omit[r.target] {
			cuts = append(cuts, [2]int{r.start, r.end})
		}
	}
	slices.SortFunc(cuts, func(a, b [2]int) int { return a[0] - b[0] })
	var out bytes.Buffer
	pos := 0
	for _, cut := range cuts {
		out.Write(docRelsData[pos:cut[0]])
		pos = cut[1]
	}
	out.Write(docRelsData[pos:])
	plan.rels, plan.relsBytes = docRels, out.Bytes()
	return plan, nil
}

// relsBase tells whether an entry is a relationships part and returns the
// folder its targets are relative to: for dir/_rels/name.rels that is dir.
func relsBase(name string) (string, bool) {
	name = strings.ToLower(name)
	if !strings.HasSuffix(name, ".rels") || strings.HasSuffix(name, "/") {
		return "", false
	}
	dir := path.Dir(name)
	if path.Base(dir) == "_rels" {
		return path.Dir(dir), true
	}
	return dir, true
}

// resolveTarget turns the Target of a relationship into the lower-case name of
// the part it points at. A target is relative to the folder of the source part
// ("media/x.png") or absolute from the package root ("/word/media/x.png").
// The result is only ever compared with entry names.
func resolveTarget(base, target string) string {
	if t, err := url.PathUnescape(target); err == nil {
		target = t
	}
	if i := strings.IndexByte(target, '#'); i >= 0 {
		target = target[:i]
	}
	var p string
	if rest, ok := strings.CutPrefix(target, "/"); ok {
		p = path.Clean(rest)
	} else {
		p = path.Join(base, target)
	}
	return strings.ToLower(p)
}

// readRelationships reads one .rels part into memory (bounded by maxBytes) and
// lists its relationships with their byte ranges.
func readRelationships(ctx context.Context, zf *zip.File, base string, maxBytes int64) ([]byte, []relationship, error) {
	rc, err := zf.Open()
	if err != nil {
		return nil, nil, wrapRead(zf.Name, err)
	}
	defer func() { _ = rc.Close() }() // read-only; nothing to lose on a failed Close

	pr := newPartReader(ctx, rc, maxBytes)
	data, err := io.ReadAll(pr)
	if err != nil {
		if pr.tooLarge {
			return nil, nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrTooLarge, zf.Name, maxBytes)
		}
		if cerr := ctx.Err(); cerr != nil {
			return nil, nil, fmt.Errorf("convert: %w", cerr)
		}
		return nil, nil, wrapRead(zf.Name, err)
	}

	dec := xml.NewDecoder(bytes.NewReader(data))
	// Same rule as Inspect: only UTF-8 is understood, anything else fails
	// closed instead of risking a picture removed that something still needs.
	dec.CharsetReader = func(label string, _ io.Reader) (io.Reader, error) {
		return nil, fmt.Errorf("%w %q", errEncoding, label)
	}
	var (
		rels     []relationship
		depth    int
		sawRoot  bool
		inRel    int // depth of the Relationship element being read, 0 when none
		relStart int
		cur      relationship
	)
	for {
		before := int(dec.InputOffset())
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			if !sawRoot {
				return nil, nil, fmt.Errorf("%w: %s has no root element", ErrNotDocx, zf.Name)
			}
			return data, rels, nil
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %s: %w", ErrNotDocx, zf.Name, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth > maxXMLDepth {
				return nil, nil, fmt.Errorf("%w: %s nests deeper than %d elements", ErrTooLarge, zf.Name, maxXMLDepth)
			}
			sawRoot = true
			if t.Name.Local != "Relationship" || inRel != 0 {
				continue
			}
			inRel, relStart = depth, before
			cur = relationship{topLevel: depth == 2 && t.Name.Space == nsPackageRels}
			var target, mode string
			for _, a := range t.Attr {
				switch a.Name.Local {
				case "Type":
					cur.image = strings.HasSuffix(a.Value, "/image")
				case "Target":
					target = a.Value
				case "TargetMode":
					mode = a.Value
				}
			}
			if !strings.EqualFold(mode, "External") {
				cur.target = resolveTarget(base, target)
			}
		case xml.EndElement:
			if inRel == depth {
				cur.start, cur.end = relStart, int(dec.InputOffset())
				rels = append(rels, cur)
				inRel = 0
			}
			depth--
		}
	}
}

// writeReference writes the copy to dst. The write handle is closed with its
// error checked: a failed close can mean a truncated archive.
func (c *Converter) writeReference(ctx context.Context, zr *zip.Reader, plan referencePlan, dst string) (err error) {
	// O_TRUNC: dst is either new or an empty placeholder (checkOutputFree).
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("convert: %w", err)
	}
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("convert: close %s: %w", filepath.Base(dst), cerr)
		}
	}()

	zw := zip.NewWriter(out)
	if err := zw.SetComment(zr.Comment); err != nil {
		return fmt.Errorf("convert: %w", err)
	}
	for _, e := range zr.File {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("convert: %w", err)
		}
		switch {
		case plan.omit[strings.ToLower(e.Name)]:
			continue
		case e == plan.rels:
			h := &zip.FileHeader{
				Name:           e.Name,
				Comment:        e.Comment,
				NonUTF8:        e.NonUTF8,
				Method:         zip.Deflate,
				Modified:       e.Modified,
				CreatorVersion: e.CreatorVersion,
				ExternalAttrs:  e.ExternalAttrs,
			}
			w, err := zw.CreateHeader(h)
			if err != nil {
				return fmt.Errorf("convert: %w", err)
			}
			if _, err := w.Write(plan.relsBytes); err != nil {
				return fmt.Errorf("convert: %w", err)
			}
		default:
			// Raw copy: no decompression, so a hostile entry costs nothing here.
			if err := zw.Copy(e); err != nil {
				return fmt.Errorf("convert: copy %q: %w", e.Name, err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("convert: %w", err)
	}
	return nil
}
