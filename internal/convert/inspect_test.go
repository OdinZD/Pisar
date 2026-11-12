package convert

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	nsOther = "http://example.com/not-word"
	xmlDecl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
)

var testLimits = InspectLimits{MaxEntries: 100, MaxPartBytes: 1 << 20}

type entry struct {
	name string
	body string
}

// part wraps inner in a root element that declares the prefix w.
func part(root, inner string) string {
	return xmlDecl + `<w:` + root + ` xmlns:w="` + nsTransitional + `">` + inner + `</w:` + root + `>`
}

// docWith is a minimal word/document.xml whose body is inner.
func docWith(inner string) entry {
	return entry{"word/document.xml", part("document", "<w:body>"+inner+"</w:body>")}
}

func plainDoc() entry { return docWith(`<w:p><w:r><w:t>text</w:t></w:r></w:p>`) }

func mustZip(tb testing.TB, entries []entry) []byte {
	tb.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			tb.Fatal(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			tb.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

func writeFile(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "doc.docx")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInspect(t *testing.T) {
	deep := docWith(strings.Repeat("<w:a>", maxXMLDepth+1) + strings.Repeat("</w:a>", maxXMLDepth+1))
	all := docWith(`<w:p><w:r><w:txbxContent/></w:r><w:del/><w:ins/><w:comment/><w:txbxContent/><w:ins/><w:comment/></w:p>`)
	// allFour has the fourth loss as well, which is what "everything is found" means.
	allFour := docWith(`<w:p><w:r><w:txbxContent/></w:r><w:del/><w:ins/><w:comment/><w:object/></w:p>`)

	tests := []struct {
		name    string
		entries []entry
		lim     InspectLimits
		want    []Loss
		wantErr error
	}{
		{name: "plain document", entries: []entry{plainDoc()}},
		{name: "comment element", entries: []entry{docWith(`<w:comment/>`)}, want: []Loss{LossComments}},
		{name: "other prefix", entries: []entry{{"word/document.xml",
			xmlDecl + `<x:document xmlns:x="` + nsTransitional + `"><x:body><x:ins/></x:body></x:document>`}},
			want: []Loss{LossTrackedChanges}},
		{name: "default namespace", entries: []entry{{"word/document.xml",
			xmlDecl + `<document xmlns="` + nsTransitional + `"><body><txbxContent/></body></document>`}},
			want: []Loss{LossTextBoxes}},
		{name: "strict namespace", entries: []entry{{"word/document.xml",
			xmlDecl + `<w:document xmlns:w="` + nsStrict + `"><w:body><w:del/></w:body></w:document>`}},
			want: []Loss{LossTrackedChanges}},
		{name: "empty comments part has no comment", entries: []entry{plainDoc(),
			{"word/comments.xml", part("comments", "")}}},
		{name: "footnotes part without footnotes", entries: []entry{plainDoc(),
			{"word/footnotes.xml", part("footnotes", `<w:footnote w:type="separator"/>`)}}},
		{name: "all three, in fixed order and once", entries: []entry{all},
			want: []Loss{LossComments, LossTrackedChanges, LossTextBoxes}},
		{name: "all three spread over parts", entries: []entry{plainDoc(),
			{"word/header1.xml", part("hdr", `<w:txbxContent/>`)},
			{"word/footer1.xml", part("ftr", `<w:moveTo/>`)},
			{"word/comments.xml", part("comments", `<w:comment/>`)}},
			want: []Loss{LossComments, LossTrackedChanges, LossTextBoxes}},
		{name: "only in the header", entries: []entry{plainDoc(),
			{"word/header1.xml", part("hdr", `<w:p><w:ins/></w:p>`)}},
			want: []Loss{LossTrackedChanges}},
		{name: "part name case", entries: []entry{plainDoc(),
			{"Word/Header1.XML", part("hdr", `<w:ins/>`)}},
			want: []Loss{LossTrackedChanges}},
		{name: "moveFrom", entries: []entry{docWith(`<w:moveFrom/>`)}, want: []Loss{LossTrackedChanges}},
		{name: "rPrChange", entries: []entry{docWith(`<w:rPr><w:rPrChange/></w:rPr>`)}, want: []Loss{LossTrackedChanges}},
		{name: "numberingChange", entries: []entry{docWith(`<w:numberingChange/>`)}, want: []Loss{LossTrackedChanges}},
		{name: "same local name in a foreign namespace", entries: []entry{{"word/document.xml",
			xmlDecl + `<w:document xmlns:w="` + nsTransitional + `" xmlns:o="` + nsOther + `"><w:body>` +
				`<o:ins/><o:comment/><o:txbxContent/><ins xmlns="` + nsOther + `"/>` +
				`<w:p xmlns:w="` + nsOther + `"><w:del/></w:p></w:body></w:document>`}}},
		{name: "unbound prefix is not Word", entries: []entry{{"word/document.xml",
			xmlDecl + `<document><w:ins/></document>`}}},
		{name: "markup-looking text and CDATA", entries: []entry{docWith(
			`<w:t>&lt;w:ins &gt; &lt;w:comment&gt;</w:t><w:t><![CDATA[<w:ins w:id="1"/><w:txbxContent>]]></w:t><!-- <w:del/> -->`)}},
		{name: "parts outside word/ and below it are not read", entries: []entry{plainDoc(),
			{"word/sub/header.xml", part("hdr", `<w:ins/>`)},
			{"other/word/header.xml", part("hdr", `<w:ins/>`)},
			{"word/data.bin", "<w:ins/>"},
			{"customXml/item1.xml", part("x", `<w:ins/>`)}}},
		{name: "reading stops once all four are found", entries: []entry{allFour,
			{"word/header1.xml", "this is not XML"}},
			want: []Loss{LossComments, LossTrackedChanges, LossTextBoxes, LossObjects}},
		{name: "three found are not all: the next part is read", entries: []entry{all,
			{"word/header1.xml", "this is not XML"}}, wantErr: ErrNotDocx},
		{name: "reading stops inside the part", entries: []entry{{"word/document.xml",
			xmlDecl + `<w:document xmlns:w="` + nsTransitional + `"><w:comment/><w:ins/><w:txbxContent/><w:object/><w:oops></w:document>`}},
			want: []Loss{LossComments, LossTrackedChanges, LossTextBoxes, LossObjects}},

		{name: "no word/document.xml", entries: []entry{{"word/comments.xml", part("comments", `<w:comment/>`)}}, wantErr: ErrNotDocx},
		{name: "document.xml only below word/", entries: []entry{{"word/sub/document.xml", part("document", "")}}, wantErr: ErrNotDocx},
		{name: "empty archive", wantErr: ErrNotDocx},
		{name: "malformed XML", entries: []entry{{"word/document.xml",
			xmlDecl + `<w:document xmlns:w="` + nsTransitional + `"><w:body></w:document>`}}, wantErr: ErrNotDocx},
		{name: "malformed XML in another part", entries: []entry{plainDoc(),
			{"word/header1.xml", `<w:hdr xmlns:w="` + nsTransitional + `"><w:p></w:hdr>`}}, wantErr: ErrNotDocx},
		{name: "XML part with no content", entries: []entry{plainDoc(), {"word/comments.xml", ""}}, wantErr: ErrNotDocx},
		{name: "XML part that is plain text", entries: []entry{{"word/document.xml", "hello"}}, wantErr: ErrNotDocx},
		{name: "unsupported XML version", entries: []entry{{"word/document.xml",
			`<?xml version="2.0"?><w:document xmlns:w="` + nsTransitional + `"><w:ins/></w:document>`}}, wantErr: ErrNotDocx},
		{name: "unknown entity", entries: []entry{docWith(`<w:t>&nbsp;</w:t>`)}, wantErr: ErrNotDocx},
		{name: "unsupported encoding fails closed", entries: []entry{{"word/document.xml",
			`<?xml version="1.0" encoding="UTF-16"?><w:document xmlns:w="` + nsTransitional + `"><w:ins/></w:document>`}}, wantErr: ErrNotDocx},

		{name: "too many entries", entries: []entry{plainDoc(), {"a", ""}, {"b", ""}, {"c", ""}},
			lim: InspectLimits{MaxEntries: 3, MaxPartBytes: 1 << 20}, wantErr: ErrTooLarge},
		{name: "entries exactly at the limit", entries: []entry{plainDoc(), {"a", ""}, {"b", ""}},
			lim: InspectLimits{MaxEntries: 3, MaxPartBytes: 1 << 20}},
		{name: "part too large", entries: []entry{docWith(strings.Repeat("<w:p/>", 100))},
			lim: InspectLimits{MaxEntries: 10, MaxPartBytes: 100}, wantErr: ErrTooLarge},
		{name: "part too large, found before the limit", entries: []entry{
			docWith(`<w:comment/><w:ins/><w:txbxContent/><w:object/>` + strings.Repeat("<w:p/>", 100))},
			lim:  InspectLimits{MaxEntries: 10, MaxPartBytes: 1 << 20},
			want: []Loss{LossComments, LossTrackedChanges, LossTextBoxes, LossObjects}},
		{name: "part too large, in a later part", entries: []entry{plainDoc(),
			{"word/header1.xml", part("hdr", strings.Repeat("<w:p/>", 100))}},
			lim: InspectLimits{MaxEntries: 10, MaxPartBytes: 200}, wantErr: ErrTooLarge},
		{name: "zero limits refuse everything", entries: []entry{plainDoc()}, lim: InspectLimits{}, wantErr: ErrTooLarge},
		{name: "nesting too deep", entries: []entry{deep}, wantErr: ErrTooLarge},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lim := tc.lim
			if lim == (InspectLimits{}) && tc.name != "zero limits refuse everything" {
				lim = testLimits
			}
			data := mustZip(t, tc.entries)
			p := writeFile(t, data)

			got, err := Inspect(context.Background(), p, lim)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}
				if got != nil {
					t.Fatalf("result %v alongside an error", got)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("losses = %v, want %v", got, tc.want)
			}
			assertUnchanged(t, p, data)
		})
	}
}

// assertUnchanged checks that Inspect only reads: same bytes, no new files next to it.
func assertUnchanged(t *testing.T, p string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("Inspect changed the file")
	}
	es, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 1 {
		t.Fatalf("directory has %d entries after Inspect, want 1", len(es))
	}
}

func TestInspectFixtures(t *testing.T) {
	tests := []struct {
		file    string
		want    []Loss
		wantErr error
	}{
		{file: "basic.docx"},
		{file: "complex-table.docx"},
		{file: "comments.docx", want: []Loss{LossComments}},
		{file: "tracked-changes.docx", want: []Loss{LossTrackedChanges}},
		{file: "textbox.docx", want: []Loss{LossTextBoxes}},
		{file: "chart.docx", want: []Loss{LossObjects}},
		{file: "formatting.docx"}, // formatting is not a loss: Inspect does not report it
		{file: "legacy.doc", wantErr: ErrNotDocx},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			// Read-only: the fixture is opened in place, never written.
			p := filepath.Join("..", "..", "testdata", "docx", tc.file)
			before, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			limits := InspectLimits{MaxEntries: 10000, MaxPartBytes: 256 << 20}
			got, err := Inspect(context.Background(), p, limits)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("losses = %v, want %v", got, tc.want)
			}
			after, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("fixture changed")
			}
		})
	}
}

func TestInspectNotAnArchive(t *testing.T) {
	good := mustZip(t, []entry{plainDoc(), {"word/header1.xml", part("hdr", `<w:ins/>`)}})
	tests := []struct {
		name string
		data []byte
	}{
		{"text", []byte("this is not a zip file")},
		{"empty file", nil},
		{"truncated in half", good[:len(good)/2]},
		{"truncated by one byte", good[:len(good)-1]},
		{"only the zip signature", []byte("PK\x03\x04")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := writeFile(t, tc.data)
			got, err := Inspect(context.Background(), p, testLimits)
			if !errors.Is(err, ErrNotDocx) {
				t.Fatalf("error = %v, want ErrNotDocx", err)
			}
			if got != nil {
				t.Fatalf("result %v alongside an error", got)
			}
		})
	}
}

// The size in the archive header is not believed. archive/zip itself stops a
// stream that outgrows its declared size and reports a mismatch at the end, so
// either way a lying header ends in an error and never in an unbounded read.
func TestInspectLyingHeader(t *testing.T) {
	body := []byte(part("document", strings.Repeat("<w:p/>", 2000)))
	var comp bytes.Buffer
	fw, err := flate.NewWriter(&comp, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := fw.Close(); err != nil {
		t.Fatal(err)
	}

	for _, declared := range []uint64{0, 10, uint64(len(body)) - 1, uint64(len(body)) + 1, 1 << 40} {
		t.Run(fmt.Sprint(declared), func(t *testing.T) {
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			w, err := zw.CreateRaw(&zip.FileHeader{
				Name:               "word/document.xml",
				Method:             zip.Deflate,
				CompressedSize64:   uint64(comp.Len()),
				UncompressedSize64: declared,
				CRC32:              crc32.ChecksumIEEE(body),
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(comp.Bytes()); err != nil {
				t.Fatal(err)
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			p := writeFile(t, buf.Bytes())
			// A small limit as well: neither error kind may be a success.
			_, err = Inspect(context.Background(), p, InspectLimits{MaxEntries: 10, MaxPartBytes: 1000})
			if !errors.Is(err, ErrNotDocx) && !errors.Is(err, ErrTooLarge) {
				t.Fatalf("error = %v, want ErrNotDocx or ErrTooLarge", err)
			}
		})
	}
}

func TestInspectCorruptCompressedData(t *testing.T) {
	data := mustZip(t, []entry{docWith(strings.Repeat("<w:p><w:r><w:t>abc</w:t></w:r></w:p>", 50))})
	// Flip bytes in the middle of the compressed stream; the directory at the end stays intact.
	for i := 40; i < 60 && i < len(data); i++ {
		data[i] ^= 0xff
	}
	p := writeFile(t, data)
	_, err := Inspect(context.Background(), p, testLimits)
	if !errors.Is(err, ErrNotDocx) {
		t.Fatalf("error = %v, want ErrNotDocx", err)
	}
}

// The regression input found by fuzzing (an entry offset above MaxInt64) is
// kept in testdata/fuzz/FuzzInspect and runs with every go test.
func TestSanePositions(t *testing.T) {
	r := sanePositions{bytes.NewReader([]byte("abc"))}
	if _, err := r.ReadAt(make([]byte, 1), -1); !errors.Is(err, zip.ErrFormat) {
		t.Fatalf("error = %v, want zip.ErrFormat", err)
	}
	b := make([]byte, 2)
	if n, err := r.ReadAt(b, 1); err != nil || string(b[:n]) != "bc" {
		t.Fatalf("ReadAt = %q, %v", b[:n], err)
	}
}

func TestInspectContext(t *testing.T) {
	data := mustZip(t, []entry{plainDoc()})
	p := writeFile(t, data)

	t.Run("canceled before the call", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		got, err := Inspect(ctx, p, testLimits)
		if !errors.Is(err, context.Canceled) || got != nil {
			t.Fatalf("got %v, %v; want context.Canceled", got, err)
		}
	})

	t.Run("canceled while a part is read", func(t *testing.T) {
		big := mustZip(t, []entry{docWith(strings.Repeat("<w:p><w:r><w:t>x</w:t></w:r></w:p>", 50000))})
		p := writeFile(t, big)
		// Err() is called twice before the first byte is read (entry and part
		// checks); the third call, inside the part, reports the cancellation.
		ctx := &cancelAfter{Context: context.Background(), okCalls: 2}
		_, err := Inspect(ctx, p, testLimits)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if ctx.calls < 3 {
			t.Fatalf("context was asked %d times, want a check inside the part", ctx.calls)
		}
	})
}

// cancelAfter reports context.Canceled from Err after okCalls calls.
type cancelAfter struct {
	context.Context
	okCalls int
	calls   int
}

func (c *cancelAfter) Err() error {
	c.calls++
	if c.calls > c.okCalls {
		return context.Canceled
	}
	return nil
}

func TestInspectFileErrors(t *testing.T) {
	dir := t.TempDir()

	t.Run("missing file", func(t *testing.T) {
		_, err := Inspect(context.Background(), filepath.Join(dir, "nope.docx"), testLimits)
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("error = %v, want fs.ErrNotExist", err)
		}
		if errors.Is(err, ErrNotDocx) {
			t.Fatal("a missing file is not ErrNotDocx")
		}
	})

	t.Run("directory", func(t *testing.T) {
		_, err := Inspect(context.Background(), dir, testLimits)
		if !errors.Is(err, ErrNotDocx) {
			t.Fatalf("error = %v, want ErrNotDocx", err)
		}
	})
}

func FuzzInspect(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte("PK\x03\x04"))
	f.Add(mustZip(f, []entry{plainDoc()}))
	f.Add(mustZip(f, []entry{docWith(`<w:comment/><w:ins/><w:txbxContent/><w:object/>`)}))
	f.Add(mustZip(f, []entry{plainDoc(), {"word/header1.xml", part("hdr", `<w:rPrChange/>`)},
		{"word/comments.xml", ""}}))
	f.Add(mustZip(f, []entry{docWith(`<w:t><![CDATA[<w:ins/>]]>&nbsp;</w:t>`)}))
	for _, name := range []string{"basic.docx", "comments.docx", "textbox.docx", "chart.docx", "formatting.docx", "legacy.doc"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "docx", name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}

	dir := f.TempDir()
	p := filepath.Join(dir, "doc.docx")
	lim := InspectLimits{MaxEntries: 200, MaxPartBytes: 1 << 20}

	f.Fuzz(func(t *testing.T, data []byte) {
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := Inspect(context.Background(), p, lim)
		if err != nil && !errors.Is(err, ErrNotDocx) && !errors.Is(err, ErrTooLarge) {
			t.Fatalf("unexpected error kind: %v", err)
		}
		if err != nil && got != nil {
			t.Fatalf("result %v alongside an error", got)
		}
		// Fixed order, no duplicates.
		rank := map[Loss]int{LossComments: 0, LossTrackedChanges: 1, LossTextBoxes: 2, LossObjects: 3}
		last := -1
		for _, l := range got {
			r, ok := rank[l]
			if !ok || r <= last {
				t.Fatalf("bad result %v", got)
			}
			last = r
		}
		// Read-only: the input is intact and nothing else appeared.
		after, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(after, data) {
			t.Fatalf("input changed (err %v)", err)
		}
		if es, err := os.ReadDir(dir); err != nil || len(es) != 1 {
			t.Fatalf("directory holds %d entries (err %v), want 1", len(es), err)
		}
	})
}
