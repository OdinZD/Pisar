package convert

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	relImage       = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
	relImageStrict = "http://purl.oclc.org/ooxml/officeDocument/relationships/image"
	relHeader      = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/header"
	relStyles      = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
	relOLE         = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/oleObject"
	relHyperlink   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"
)

func relEl(id, typ, target string, extra ...string) string {
	return `<Relationship Id="` + id + `" Type="` + typ + `" Target="` + target + `"` + strings.Join(extra, "") + `/>`
}

func relsOf(items ...string) string {
	return xmlDecl + `<Relationships xmlns="` + nsPackageRels + `">` + strings.Join(items, "") + `</Relationships>`
}

func refConverter(t *testing.T, lim InspectLimits) *Converter {
	t.Helper()
	c, err := New(Options{PandocBin: "pandoc", TextutilBin: "textutil", Timeout: time.Minute, Limits: lim})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// readZip returns the entries of an archive in order.
func readZip(t *testing.T, path string) ([]string, map[string][]byte) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	var names []string
	data := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		if err := rc.Close(); err != nil {
			t.Fatal(err)
		}
		names = append(names, f.Name)
		data[f.Name] = b
	}
	return names, data
}

func TestMakeReference(t *testing.T) {
	png := "\x89PNG body"
	logo := "\x89PNG logo"
	hdr := part("hdr", `<w:p/>`)

	tests := []struct {
		name    string
		entries []entry
		// gone: entries that must be absent; the rest must be present with the same content.
		gone []string
		// wantRels is the exact content of word/_rels/document.xml.rels, "" to expect it unchanged.
		wantRels string
	}{
		{
			name: "body image is removed with its relationship",
			entries: []entry{plainDoc(),
				{documentRelsPart, relsOf(relEl("rId1", relStyles, "styles.xml"), relEl("rId2", relImage, "media/a.png"))},
				{"word/styles.xml", part("styles", "")}, {"word/media/a.png", png}},
			gone:     []string{"word/media/a.png"},
			wantRels: relsOf(relEl("rId1", relStyles, "styles.xml")),
		},
		{
			name: "header image stays, body image goes",
			entries: []entry{plainDoc(),
				{documentRelsPart, relsOf(relEl("rId1", relHeader, "header1.xml"), relEl("rId2", relImage, "media/body.png"))},
				{"word/header1.xml", hdr},
				{"word/_rels/header1.xml.rels", relsOf(relEl("rIdL", relImage, "media/logo.png"))},
				{"word/media/body.png", png}, {"word/media/logo.png", logo}},
			gone:     []string{"word/media/body.png"},
			wantRels: relsOf(relEl("rId1", relHeader, "header1.xml")),
		},
		{
			name: "an image used by both body and header stays, and the relationship with it",
			entries: []entry{plainDoc(),
				{documentRelsPart, relsOf(relEl("rId1", relImage, "media/shared.png"), relEl("rId2", relImage, "media/body.png"))},
				{"word/footer1.xml", part("ftr", "")},
				{"word/_rels/footer1.xml.rels", relsOf(relEl("rIdF", relImage, "media/shared.png"))},
				{"word/media/shared.png", logo}, {"word/media/body.png", png}},
			gone:     []string{"word/media/body.png"},
			wantRels: relsOf(relEl("rId1", relImage, "media/shared.png")),
		},
		{
			name: "absolute targets",
			entries: []entry{plainDoc(),
				{documentRelsPart, relsOf(relEl("rId1", relImage, "/word/media/body.png"), relEl("rId2", relImage, "/word/media/shared.png"))},
				{"word/_rels/header1.xml.rels", relsOf(relEl("rIdL", relImage, "/word/media/logo.png"), relEl("rIdS", relImage, "media/shared.png"))},
				{"word/media/body.png", png}, {"word/media/logo.png", logo}, {"word/media/shared.png", logo}},
			gone:     []string{"word/media/body.png"},
			wantRels: relsOf(relEl("rId2", relImage, "/word/media/shared.png")),
		},
		{
			name: "a target with .. and a different case",
			entries: []entry{plainDoc(),
				{documentRelsPart, relsOf(relEl("rId1", relImage, "../word/Media/BODY.png"))},
				{"Word/Media/Body.PNG", png}},
			gone:     []string{"Word/Media/Body.PNG"},
			wantRels: relsOf(),
		},
		{
			name: "a percent-encoded target",
			entries: []entry{plainDoc(),
				{documentRelsPart, relsOf(relEl("rId1", relImage, "media/my%20pic.png"))},
				{"word/media/my pic.png", png}},
			gone:     []string{"word/media/my pic.png"},
			wantRels: relsOf(),
		},
		{
			name: "the strict image type",
			entries: []entry{plainDoc(),
				{documentRelsPart, relsOf(relEl("rId1", relImageStrict, "media/a.png"))},
				{"word/media/a.png", png}},
			gone:     []string{"word/media/a.png"},
			wantRels: relsOf(),
		},
		{
			name: "an external link is untouched, even one that looks like a media path",
			entries: []entry{plainDoc(),
				{documentRelsPart, relsOf(
					relEl("rId1", relImage, "https://example.com/pic.png", ` TargetMode="External"`),
					relEl("rId2", relImage, "media/a.png", ` TargetMode="External"`),
					relEl("rId3", relImage, "media/b.png"))},
				{"word/media/a.png", png}, {"word/media/b.png", png}},
			gone: []string{"word/media/b.png"},
			wantRels: relsOf(
				relEl("rId1", relImage, "https://example.com/pic.png", ` TargetMode="External"`),
				relEl("rId2", relImage, "media/a.png", ` TargetMode="External"`)),
		},
		{
			name: "unknown attributes and the bytes around the removed element are kept",
			entries: []entry{plainDoc(),
				{documentRelsPart, xmlDecl + "\n<Relationships xmlns=\"" + nsPackageRels + "\" xmlns:x=\"urn:x\">\n  " +
					`<Relationship Id="rId1" Type="` + relStyles + `" Target="styles.xml" x:note="keep &amp; me"/>` + "\n  " +
					`<Relationship Id="rId2" Type="` + relImage + `" Target="media/a.png" x:note="drop"/>` + "\n  " +
					`<Relationship Type="` + relHyperlink + `" Id="rId3" Target="https://e.com/?a=1&amp;b=2" TargetMode="External"/>` + "\n" +
					"</Relationships>"},
				{"word/styles.xml", part("styles", "")}, {"word/media/a.png", png}},
			gone: []string{"word/media/a.png"},
			wantRels: xmlDecl + "\n<Relationships xmlns=\"" + nsPackageRels + "\" xmlns:x=\"urn:x\">\n  " +
				`<Relationship Id="rId1" Type="` + relStyles + `" Target="styles.xml" x:note="keep &amp; me"/>` + "\n  \n  " +
				`<Relationship Type="` + relHyperlink + `" Id="rId3" Target="https://e.com/?a=1&amp;b=2" TargetMode="External"/>` + "\n" +
				"</Relationships>",
		},
		{
			name: "an element with a separate end tag",
			entries: []entry{plainDoc(),
				{documentRelsPart, xmlDecl + `<Relationships xmlns="` + nsPackageRels + `"><Relationship Id="rId1" Type="` + relImage +
					`" Target="media/a.png"></Relationship><Relationship Id="rId2" Type="` + relStyles + `" Target="styles.xml"/></Relationships>`},
				{"word/media/a.png", png}},
			gone:     []string{"word/media/a.png"},
			wantRels: xmlDecl + `<Relationships xmlns="` + nsPackageRels + `"><Relationship Id="rId2" Type="` + relStyles + `" Target="styles.xml"/></Relationships>`,
		},
		{
			name: "a non-image relationship to the same file keeps it",
			entries: []entry{plainDoc(),
				{documentRelsPart, relsOf(relEl("rId1", relImage, "media/a.png"), relEl("rId2", relOLE, "media/a.png"))},
				{"word/media/a.png", png}},
		},
		{
			name: "a picture of a chart (another folder's relationships) keeps it",
			entries: []entry{plainDoc(),
				{documentRelsPart, relsOf(relEl("rId1", relImage, "media/a.png"), relEl("rId2", relImage, "media/b.png"))},
				{"word/charts/_rels/chart1.xml.rels", relsOf(relEl("rIdC", relImage, "../media/a.png"))},
				{"word/media/a.png", png}, {"word/media/b.png", png}},
			gone:     []string{"word/media/b.png"},
			wantRels: relsOf(relEl("rId1", relImage, "media/a.png")),
		},
		{
			name: "a picture named in the package relationships keeps it",
			entries: []entry{plainDoc(),
				{documentRelsPart, relsOf(relEl("rId1", relImage, "media/a.png"))},
				{"_rels/.rels", relsOf(relEl("rId9", relImage, "word/media/a.png"))},
				{"word/media/a.png", png}},
		},
		{
			name: "media that no relationship names is not touched",
			entries: []entry{plainDoc(),
				{documentRelsPart, relsOf(relEl("rId1", relImage, "media/a.png"))},
				{"word/media/a.png", png}, {"word/media/orphan.png", png}},
			gone:     []string{"word/media/a.png"},
			wantRels: relsOf(),
		},
		{
			name: "only word/media is thinned",
			entries: []entry{plainDoc(),
				{documentRelsPart, relsOf(relEl("rId1", relImage, "images/a.png"), relEl("rId2", relImage, "media/b.png"))},
				{"word/images/a.png", png}, {"word/media/b.png", png}},
			gone:     []string{"word/media/b.png"},
			wantRels: relsOf(relEl("rId1", relImage, "images/a.png")),
		},
		{
			name: "a relationship whose picture is not in the archive is left alone",
			entries: []entry{plainDoc(),
				{documentRelsPart, relsOf(relEl("rId1", relImage, "media/missing.png"), relEl("rId2", relImage, "media/b.png"))},
				{"word/media/b.png", png}},
			gone:     []string{"word/media/b.png"},
			wantRels: relsOf(relEl("rId1", relImage, "media/missing.png")),
		},
		{
			name: "a Relationship in another namespace is not a relationship",
			entries: []entry{plainDoc(),
				{documentRelsPart, xmlDecl + `<Relationships xmlns="urn:other">` + relEl("rId1", relImage, "media/a.png") + `</Relationships>`},
				{"word/media/a.png", png}},
		},
		{
			name: "no document relationships: nothing to thin",
			entries: []entry{plainDoc(),
				{"word/media/a.png", png}, {"word/styles.xml", part("styles", "")}},
		},
		{
			name: "directory entries and other files pass through",
			entries: []entry{plainDoc(), {"word/", ""}, {"word/media/", ""},
				{documentRelsPart, relsOf(relEl("rId1", relImage, "media/a.png"))},
				{"word/media/a.png", png}, {"docProps/core.xml", part("coreProperties", "")}},
			gone:     []string{"word/media/a.png"},
			wantRels: relsOf(),
		},
	}

	c := refConverter(t, testLimits)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := writeFile(t, mustZip(t, tt.entries))
			srcBytes, err := os.ReadFile(src)
			if err != nil {
				t.Fatal(err)
			}
			dst := filepath.Join(t.TempDir(), "reference.docx")

			if err := c.MakeReference(t.Context(), src, dst); err != nil {
				t.Fatal(err)
			}
			assertUnchanged(t, src, srcBytes)
			if _, err := Inspect(t.Context(), dst, testLimits); err != nil {
				t.Fatalf("the reference is not a valid document: %v", err)
			}

			names, data := readZip(t, dst)
			var wantNames []string
			for _, e := range tt.entries {
				if slices.Contains(tt.gone, e.name) {
					continue
				}
				wantNames = append(wantNames, e.name)
				want := e.body
				if e.name == documentRelsPart && tt.wantRels != "" {
					want = tt.wantRels
				}
				if got := string(data[e.name]); got != want {
					t.Errorf("%s = %q, want %q", e.name, got, want)
				}
			}
			if !slices.Equal(names, wantNames) {
				t.Errorf("entries = %q, want %q", names, wantNames)
			}
		})
	}
}

// Entries that are kept are copied as they are stored: same method, same
// checksum, same compressed bytes.
func TestMakeReferenceCopiesRaw(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, method uint16, body string) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("word/document.xml", zip.Deflate, part("document", "<w:body>"+strings.Repeat("<w:p/>", 200)+"</w:body>"))
	add("word/_rels/document.xml.rels", zip.Deflate, relsOf(relEl("rId1", relImage, "media/a.png")))
	add("word/media/a.png", zip.Store, "\x89PNG body")
	add("word/media/keep.png", zip.Store, "\x89PNG kept")
	add("word/theme/theme1.xml", zip.Store, part("theme", ""))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	src := writeFile(t, buf.Bytes())
	// keep.png is named by no relationship, so it is not a body image and stays.
	dst := filepath.Join(t.TempDir(), "reference.docx")
	if err := refConverter(t, testLimits).MakeReference(t.Context(), src, dst); err != nil {
		t.Fatal(err)
	}

	in, err := zip.OpenReader(src)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	out, err := zip.OpenReader(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Close() }()

	byName := map[string]*zip.File{}
	for _, f := range out.File {
		byName[f.Name] = f
	}
	raw := func(f *zip.File) []byte {
		r, err := f.OpenRaw()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for _, name := range []string{"word/document.xml", "word/media/keep.png", "word/theme/theme1.xml"} {
		var a *zip.File
		for _, f := range in.File {
			if f.Name == name {
				a = f
			}
		}
		b := byName[name]
		if b == nil {
			t.Fatalf("%s missing from the copy", name)
		}
		if a.Method != b.Method || a.CRC32 != b.CRC32 || a.CompressedSize64 != b.CompressedSize64 ||
			a.UncompressedSize64 != b.UncompressedSize64 || !a.Modified.Equal(b.Modified) || !bytes.Equal(raw(a), raw(b)) {
			t.Errorf("%s was not copied as stored", name)
		}
	}
	if byName["word/media/a.png"] != nil {
		t.Error("the body image is still there")
	}
}

func TestMakeReferenceErrors(t *testing.T) {
	many := []entry{plainDoc()}
	for i := range 10 {
		many = append(many, entry{"word/extra" + string(rune('a'+i)) + ".bin", ""})
	}
	deepRels := xmlDecl + `<Relationships xmlns="` + nsPackageRels + `">` + strings.Repeat("<a>", maxXMLDepth+1) + strings.Repeat("</a>", maxXMLDepth+1) + `</Relationships>`
	bigRels := relsOf(strings.Repeat(relEl("rId1", relStyles, "styles.xml"), 50))

	tests := []struct {
		name    string
		data    []byte
		lim     InspectLimits
		wantErr error
	}{
		{"not a zip", []byte("this is not an archive"), testLimits, ErrNotDocx},
		{"empty file", []byte{}, testLimits, ErrNotDocx},
		{"empty archive", mustZip(t, nil), testLimits, ErrNotDocx},
		{"no word/document.xml", mustZip(t, []entry{{documentRelsPart, relsOf()}}), testLimits, ErrNotDocx},
		{"malformed relationships", mustZip(t, []entry{plainDoc(), {documentRelsPart, `<Relationships><Relationship`}}), testLimits, ErrNotDocx},
		{"relationships with no content", mustZip(t, []entry{plainDoc(), {documentRelsPart, ""}}), testLimits, ErrNotDocx},
		{"malformed relationships of another part", mustZip(t, []entry{plainDoc(), {"word/_rels/header1.xml.rels", `<Relationships>`}}), testLimits, ErrNotDocx},
		{"relationships in a foreign encoding", mustZip(t, []entry{plainDoc(),
			{documentRelsPart, `<?xml version="1.0" encoding="windows-1250"?><Relationships xmlns="` + nsPackageRels + `"/>`}}), testLimits, ErrNotDocx},
		{"too many entries", mustZip(t, many), InspectLimits{MaxEntries: 5, MaxPartBytes: 1 << 20}, ErrTooLarge},
		{"relationships part over the limit", mustZip(t, []entry{plainDoc(), {documentRelsPart, bigRels}}), InspectLimits{MaxEntries: 100, MaxPartBytes: 1 << 10}, ErrTooLarge},
		{"another relationships part over the limit", mustZip(t, []entry{plainDoc(), {"word/_rels/header1.xml.rels", bigRels}}), InspectLimits{MaxEntries: 100, MaxPartBytes: 1 << 10}, ErrTooLarge},
		{"relationships nested too deep", mustZip(t, []entry{plainDoc(), {documentRelsPart, deepRels}}), testLimits, ErrTooLarge},
		{"the copy would not be a valid document", mustZip(t, []entry{plainDoc(), {"word/styles.xml", "not xml"}}), testLimits, ErrNotDocx},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := writeFile(t, tt.data)
			dst := filepath.Join(t.TempDir(), "reference.docx")
			err := refConverter(t, tt.lim).MakeReference(t.Context(), src, dst)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if exists(dst) {
				t.Error("the output of a failed run was left behind")
			}
			assertUnchanged(t, src, tt.data)
		})
	}

	t.Run("a truncated archive", func(t *testing.T) {
		full := mustZip(t, []entry{plainDoc(), {documentRelsPart, relsOf()}})
		for _, n := range []int{len(full) / 2, len(full) - 5} {
			src := writeFile(t, full[:n])
			dst := filepath.Join(t.TempDir(), "reference.docx")
			if err := refConverter(t, testLimits).MakeReference(t.Context(), src, dst); !errors.Is(err, ErrNotDocx) {
				t.Errorf("%d bytes: err = %v, want ErrNotDocx", n, err)
			}
			if exists(dst) {
				t.Error("the output of a failed run was left behind")
			}
		}
	})

	t.Run("bad paths", func(t *testing.T) {
		src := writeFile(t, mustZip(t, []entry{plainDoc()}))
		dir := t.TempDir()
		c := refConverter(t, testLimits)
		tests := []struct{ name, src, dst string }{
			{"empty src", "", filepath.Join(dir, "o.docx")},
			{"empty dst", src, ""},
			{"missing src", filepath.Join(dir, "missing.docx"), filepath.Join(dir, "o.docx")},
			{"src is a directory", dir, filepath.Join(dir, "o.docx")},
			{"dst is src", src, src},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if err := c.MakeReference(t.Context(), tt.src, tt.dst); err == nil {
					t.Error("expected an error")
				}
			})
		}
		if _, err := Inspect(t.Context(), src, testLimits); err != nil {
			t.Errorf("the source was damaged: %v", err)
		}
	})

	t.Run("an existing output is refused", func(t *testing.T) {
		src := writeFile(t, mustZip(t, []entry{plainDoc()}))
		dst := filepath.Join(t.TempDir(), "reference.docx")
		writeAt(t, dst, []byte("keep me"))
		if err := refConverter(t, testLimits).MakeReference(t.Context(), src, dst); err == nil {
			t.Error("expected an error")
		}
		assertUnchanged(t, dst, []byte("keep me"))
	})

	t.Run("an empty placeholder is filled", func(t *testing.T) {
		src := writeFile(t, mustZip(t, []entry{plainDoc()}))
		dst := filepath.Join(t.TempDir(), "reference.docx")
		writeAt(t, dst, nil)
		if err := refConverter(t, testLimits).MakeReference(t.Context(), src, dst); err != nil {
			t.Fatal(err)
		}
		if _, err := Inspect(t.Context(), dst, testLimits); err != nil {
			t.Error(err)
		}
	})

	t.Run("a cancelled context", func(t *testing.T) {
		src := writeFile(t, mustZip(t, []entry{plainDoc()}))
		dst := filepath.Join(t.TempDir(), "reference.docx")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := refConverter(t, testLimits).MakeReference(ctx, src, dst); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v", err)
		}
		if exists(dst) {
			t.Error("output left behind")
		}
	})
}

func TestMakeReferenceFixtures(t *testing.T) {
	c := refConverter(t, InspectLimits{MaxEntries: 10000, MaxPartBytes: 256 << 20})
	src := filepath.Join("..", "..", "testdata", "docx", "header-image.docx")
	logoBytes, err := os.ReadFile(filepath.Join("..", "..", "testdata", "docx", "src", "logo.png"))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "reference.docx")
	if err := c.MakeReference(t.Context(), src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(t.Context(), dst, c.limits); err != nil {
		t.Fatal(err)
	}

	_, in := readZip(t, src)
	names, out := readZip(t, dst)
	if !bytes.Equal(out["word/media/logo.png"], logoBytes) {
		t.Error("the header logo is not byte-identical")
	}
	if !strings.Contains(string(out[documentRelsPart]), `Target="header1.xml"`) {
		t.Error("the header relationship is gone")
	}
	var media []string
	for _, n := range names {
		if strings.HasPrefix(n, "word/media/") {
			media = append(media, n)
		}
	}
	if !slices.Equal(media, []string{"word/media/logo.png"}) {
		t.Errorf("media in the reference = %q, want only the logo", media)
	}
	for _, n := range []string{"word/header1.xml", "word/_rels/header1.xml.rels", "word/document.xml", "word/styles.xml"} {
		if !bytes.Equal(in[n], out[n]) {
			t.Errorf("%s changed", n)
		}
	}
	if bytes.Contains(out[documentRelsPart], []byte("media/rId")) {
		t.Error("a relationship to the removed body image is left")
	}
}
