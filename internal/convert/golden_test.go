package convert

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// -update rewrites testdata/docx/golden/*.md from what the converter makes now.
var update = flag.Bool("update", false, "rewrite the golden Markdown files in testdata/docx/golden")

var (
	fixtureDir = filepath.Join("..", "..", "testdata", "docx")
	goldenDir  = filepath.Join(fixtureDir, "golden")

	realLimits = InspectLimits{MaxEntries: 10000, MaxPartBytes: 256 << 20}
)

// fixtureNames are the seven sample documents; legacy is a .doc.
var fixtureNames = []string{"basic", "complex-table", "comments", "tracked-changes", "textbox", "header-image", "legacy"}

func fixturePath(name string) string {
	if name == "legacy" {
		return filepath.Join(fixtureDir, "legacy.doc")
	}
	return filepath.Join(fixtureDir, name+".docx")
}

// recorder wraps the exec runner and remembers every call, so a test can tell
// how many times pandoc ran.
type recorder struct {
	mu    sync.Mutex
	calls []call
}

func (r *recorder) run(ctx context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, call{dir: dir, name: name, args: append([]string(nil), args...)})
	r.mu.Unlock()
	return execRunner(ctx, dir, name, args...)
}

// readsOf counts the pandoc calls that read a .docx.
func (r *recorder) readsOf() int {
	n := 0
	for _, c := range r.calls {
		if slices.Contains(c.args, "docx") && c.args[0] == "-f" && c.args[1] == "docx" {
			n++
		}
	}
	return n
}

// realConverter returns a Converter on the real pandoc and textutil, or skips
// the test when either is missing.
func realConverter(t *testing.T, needTextutil bool) (*Converter, *recorder, string) {
	t.Helper()
	pandoc, err := exec.LookPath("pandoc")
	if err != nil {
		t.Skip("pandoc is not installed")
	}
	textutil, err := exec.LookPath("textutil")
	if err != nil {
		if needTextutil {
			t.Skip("textutil is not installed")
		}
		textutil = "textutil"
	}
	rec := &recorder{}
	c, err := New(Options{PandocBin: pandoc, TextutilBin: textutil, Timeout: time.Minute, Limits: realLimits, Run: rec.run})
	if err != nil {
		t.Fatal(err)
	}
	return c, rec, pandoc
}

// pandocVersion is the first line of `pandoc --version`, printed when a golden
// file does not match: a different pandoc is the usual reason.
func pandocVersion(pandoc string) string {
	out, err := exec.Command(pandoc, "--version").Output()
	if err != nil {
		return "unknown (" + err.Error() + ")"
	}
	line, _, _ := strings.Cut(string(out), "\n")
	return line
}

// imageRef matches the path of an extracted image in the Markdown.
var imageRef = regexp.MustCompile(`\./media/([^)\s]+)`)

// imageNames lists the extracted image names in the order they appear.
func imageNames(md string) []string {
	var names []string
	for _, m := range imageRef.FindAllStringSubmatch(md, -1) {
		names = append(names, m[1])
	}
	return names
}

// normalizeImages renames the images (rId13.png, rId26.png...) to image1.png,
// image2.png... in order of appearance. pandoc picks the name from an id that
// changes on every round trip, so the names say nothing about the content.
func normalizeImages(md string) string {
	seen := map[string]string{}
	return imageRef.ReplaceAllStringFunc(md, func(m string) string {
		name := imageRef.FindStringSubmatch(m)[1]
		if _, ok := seen[name]; !ok {
			seen[name] = fmt.Sprintf("image%d%s", len(seen)+1, path.Ext(name))
		}
		return "./media/" + seen[name]
	})
}

func readMarkdown(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, MarkdownName))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// toDocx returns the path of a .docx for the fixture: the fixture itself, or
// for the .doc a conversion of it in a temp folder.
func toDocx(t *testing.T, c *Converter, name string) string {
	t.Helper()
	if name != "legacy" {
		return fixturePath(name)
	}
	dst := filepath.Join(t.TempDir(), "legacy.docx")
	if err := c.DocToDocx(t.Context(), fixturePath(name), dst); err != nil {
		t.Fatal(err)
	}
	return dst
}

// docxToMarkdown reads a .docx that is expected to convert completely: no
// error, and no loss reported.
func docxToMarkdown(t *testing.T, c *Converter, src, dir string) {
	t.Helper()
	losses, err := c.DocxToMarkdown(t.Context(), src, dir)
	if err != nil {
		t.Fatal(err)
	}
	if losses != nil {
		t.Errorf("DocxToMarkdown(%s) reported %v", filepath.Base(src), losses)
	}
}

func TestGoldenDocxToMarkdown(t *testing.T) {
	c, _, pandoc := realConverter(t, true)
	for _, name := range fixtureNames {
		t.Run(name, func(t *testing.T) {
			docx := toDocx(t, c, name)
			work := t.TempDir()
			docxToMarkdown(t, c, docx, work)
			md := readMarkdown(t, work)
			if strings.Contains(md, work) || strings.Contains(md, "/private/") {
				t.Errorf("an absolute path got into the Markdown:\n%s", md)
			}
			got := normalizeImages(md)

			golden := filepath.Join(goldenDir, name+".md")
			if *update {
				if err := os.MkdirAll(goldenDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run the test with -update to create it)", err)
			}
			if got != string(want) {
				t.Errorf("%s differs from %s (pandoc: %s; run with -update if the change is expected)\n%s",
					name, golden, pandocVersion(pandoc), firstDifference(string(want), got))
			}
		})
	}
}

// firstDifference describes where two texts first differ.
func firstDifference(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < max(len(w), len(g)); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			return fmt.Sprintf("line %d:\n  golden: %q\n  now:    %q", i+1, a, b)
		}
	}
	return "no difference"
}

// docxRel is a Relationship of a .rels part, read without any of this package's code.
type docxRel struct {
	Type       string `xml:"Type,attr"`
	Target     string `xml:"Target,attr"`
	TargetMode string `xml:"TargetMode,attr"`
}

// relsSource is one relationships part of an archive with the folder its
// targets are relative to.
type relsSource struct {
	name string
	base string
	rels []docxRel
}

func readDocxParts(t *testing.T, docx string) (names []string, files map[string][]byte, rels []relsSource) {
	t.Helper()
	names, files = readZip(t, docx)
	for _, n := range names {
		if !strings.HasSuffix(n, ".rels") {
			continue
		}
		var doc struct {
			Rels []docxRel `xml:"Relationship"`
		}
		if err := xml.Unmarshal(files[n], &doc); err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		dir := path.Dir(n)
		rels = append(rels, relsSource{name: n, base: path.Dir(dir), rels: doc.Rels})
	}
	return names, files, rels
}

func targetName(base, target string) string {
	if rest, ok := strings.CutPrefix(target, "/"); ok {
		return path.Clean(rest)
	}
	return path.Join(base, target)
}

// dangling lists the relationships that point at an entry the archive does not have.
func dangling(t *testing.T, docx string) []string {
	t.Helper()
	names, _, rels := readDocxParts(t, docx)
	var bad []string
	for _, src := range rels {
		for _, r := range src.rels {
			if r.TargetMode == "External" {
				continue
			}
			if want := targetName(src.base, r.Target); !slices.Contains(names, want) {
				bad = append(bad, src.name+" -> "+want)
			}
		}
	}
	return bad
}

// bodyImages returns the images the document body points at, by content hash.
func bodyImages(t *testing.T, docx string) [][32]byte {
	t.Helper()
	_, files, rels := readDocxParts(t, docx)
	var out [][32]byte
	for _, src := range rels {
		if src.name != documentRelsPart {
			continue
		}
		for _, r := range src.rels {
			if strings.HasSuffix(r.Type, "/image") && r.TargetMode != "External" {
				out = append(out, sha256.Sum256(files[targetName(src.base, r.Target)]))
			}
		}
	}
	return out
}

func sortedHashes(h [][32]byte) [][32]byte {
	h = slices.Clone(h)
	slices.SortFunc(h, func(a, b [32]byte) int { return bytes.Compare(a[:], b[:]) })
	return h
}

// zipContents is every entry of an archive, decompressed, by name.
func zipContents(t *testing.T, p string) map[string][]byte {
	t.Helper()
	_, files := readZip(t, p)
	return files
}

// fixedDate makes pandoc stamp a fixed time into core.xml, as scripts/make-fixtures.sh does.
func fixedDate(t *testing.T) {
	t.Helper()
	t.Setenv("SOURCE_DATE_EPOCH", "1767225600")
}

func TestRoundTrip(t *testing.T) {
	for _, name := range []string{"basic", "complex-table", "header-image"} {
		t.Run(name, func(t *testing.T) {
			fixedDate(t)
			c, rec, _ := realConverter(t, false)
			src := fixturePath(name)

			// docx -> md
			work1 := t.TempDir()
			docxToMarkdown(t, c, src, work1)
			md1 := readMarkdown(t, work1)
			ref := filepath.Join(work1, "reference.docx")
			if err := c.MakeReference(t.Context(), src, ref); err != nil {
				t.Fatal(err)
			}

			// md -> docx -> md
			out1 := filepath.Join(t.TempDir(), "out1.docx")
			if err := c.MarkdownToDocx(t.Context(), work1, ref, out1); err != nil {
				t.Fatal(err)
			}
			work2 := t.TempDir()
			docxToMarkdown(t, c, out1, work2)
			md2 := readMarkdown(t, work2)
			if a, b := normalizeImages(md1), normalizeImages(md2); a != b {
				t.Errorf("the Markdown changed in a round trip:\n%s", firstDifference(a, b))
			}

			// The pictures of the body: as many, and the same bytes.
			wantImages, gotImages := bodyImages(t, src), bodyImages(t, out1)
			wantCount := 1
			if name == "complex-table" {
				wantCount = 0 // no picture in that document
			}
			if len(wantImages) != wantCount || len(gotImages) != len(wantImages) {
				t.Errorf("images in the body: %d before, %d after", len(wantImages), len(gotImages))
			}
			if !slices.Equal(sortedHashes(wantImages), sortedHashes(gotImages)) {
				t.Error("the body image is not byte-identical after the round trip")
			}
			// The extracted file is the image of the source too.
			for _, n := range imageNames(md1) {
				b, err := os.ReadFile(filepath.Join(work1, MediaDirName, n))
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Contains(wantImages, sha256.Sum256(b)) {
					t.Errorf("media/%s is not an image of the source", n)
				}
			}

			// No relationship to nothing, in any part.
			if bad := dangling(t, out1); len(bad) != 0 {
				t.Errorf("relationships to missing entries: %q", bad)
			}

			// The same Markdown saved again: the same document, the same Markdown.
			out2 := filepath.Join(t.TempDir(), "out2.docx")
			if err := c.MarkdownToDocx(t.Context(), work1, ref, out2); err != nil {
				t.Fatal(err)
			}
			a, b := zipContents(t, out1), zipContents(t, out2)
			for n := range a {
				if !bytes.Equal(a[n], b[n]) {
					t.Errorf("a second save of the same Markdown differs in %s", n)
				}
			}
			if len(a) != len(b) {
				t.Errorf("a second save has %d entries, the first %d", len(b), len(a))
			}
			work3 := t.TempDir()
			docxToMarkdown(t, c, out2, work3)
			if got := normalizeImages(readMarkdown(t, work3)); got != normalizeImages(md2) {
				t.Errorf("a second save reads differently:\n%s", firstDifference(normalizeImages(md2), got))
			}

			// A reference made from the saved document, as the next session would.
			ref2 := filepath.Join(work2, "reference.docx")
			if err := c.MakeReference(t.Context(), out1, ref2); err != nil {
				t.Fatal(err)
			}
			out3 := filepath.Join(t.TempDir(), "out3.docx")
			if err := c.MarkdownToDocx(t.Context(), work2, ref2, out3); err != nil {
				t.Fatal(err)
			}
			work4 := t.TempDir()
			docxToMarkdown(t, c, out3, work4)
			md4 := readMarkdown(t, work4)
			if got := normalizeImages(md4); got != normalizeImages(md2) {
				t.Errorf("a save with a reference made from a saved document reads differently:\n%s", firstDifference(normalizeImages(md2), got))
			}
			if got := bodyImages(t, out3); !slices.Equal(sortedHashes(got), sortedHashes(wantImages)) {
				t.Errorf("after three generations the body has %d images", len(got))
			}
			if bad := dangling(t, out3); len(bad) != 0 {
				t.Errorf("relationships to missing entries after three generations: %q", bad)
			}
			t.Logf("image names: source md %q, saved md %q, third md %q", imageNames(md1), imageNames(md2), imageNames(md4))

			// Tables: grid tables only when a cell needs them.
			wantReads := 1
			if name == "complex-table" {
				wantReads = 2
			}
			// Per round trip of this test: 4 reads (md1, md2, md3, md4), each of which
			// reads a document with the same tables.
			if got := rec.readsOf(); got != 4*wantReads {
				t.Errorf("pandoc read documents %d times, want %d", got, 4*wantReads)
			}
		})
	}
}

func TestHeaderImageSurvivesSave(t *testing.T) {
	c, _, _ := realConverter(t, false)
	src := fixturePath("header-image")
	logo, err := os.ReadFile(filepath.Join(fixtureDir, "src", "logo.png"))
	if err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	docxToMarkdown(t, c, src, work)
	ref := filepath.Join(work, "reference.docx")
	if err := c.MakeReference(t.Context(), src, ref); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.docx")
	if err := c.MarkdownToDocx(t.Context(), work, ref, out); err != nil {
		t.Fatal(err)
	}

	names, files, rels := readDocxParts(t, out)
	var header string
	for _, n := range names {
		if strings.HasPrefix(n, "word/header") && strings.HasSuffix(n, ".xml") {
			header = n
		}
	}
	if header == "" {
		t.Fatalf("no header in the saved document: %q", names)
	}
	if !strings.Contains(string(files["word/document.xml"]), "headerReference") {
		t.Error("the section does not reference the header any more")
	}
	found := false
	for _, src := range rels {
		if src.name != "word/_rels/"+path.Base(header)+".rels" {
			continue
		}
		for _, r := range src.rels {
			if strings.HasSuffix(r.Type, "/image") && bytes.Equal(files[targetName(src.base, r.Target)], logo) {
				found = true
			}
		}
	}
	if !found {
		t.Error("the logo in the header is gone or changed")
	}
	if bad := dangling(t, out); len(bad) != 0 {
		t.Errorf("relationships to missing entries: %q", bad)
	}
	if n := len(bodyImages(t, out)); n != 1 {
		t.Errorf("%d images in the body, want 1", n)
	}
}

func TestBasicHasNoDanglingRelationshipsAfterSave(t *testing.T) {
	c, _, _ := realConverter(t, false)
	src := fixturePath("basic")
	work := t.TempDir()
	docxToMarkdown(t, c, src, work)
	ref := filepath.Join(work, "reference.docx")
	if err := c.MakeReference(t.Context(), src, ref); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.docx")
	if err := c.MarkdownToDocx(t.Context(), work, ref, out); err != nil {
		t.Fatal(err)
	}
	if bad := dangling(t, out); len(bad) != 0 {
		t.Errorf("relationships to missing entries: %q", bad)
	}
	// The source itself must be clean too, or the check proves nothing.
	if bad := dangling(t, src); len(bad) != 0 {
		t.Errorf("the fixture has dangling relationships: %q", bad)
	}
}

func TestComplexTableSecondPass(t *testing.T) {
	c, rec, _ := realConverter(t, false)
	src := fixturePath("complex-table")
	ref := filepath.Join(t.TempDir(), "reference.docx")
	if err := c.MakeReference(t.Context(), src, ref); err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	docxToMarkdown(t, c, src, work)
	if got := rec.readsOf(); got != 2 {
		t.Errorf("pandoc read the document %d times, want 2 (pipe tables, then grid tables)", got)
	}
	if last := rec.calls[len(rec.calls)-1]; !slices.Contains(last.args, formatGridTables) {
		t.Errorf("the second pass did not use the grid dialect: %q", last.args)
	}
	md := readMarkdown(t, work)
	check := func(md, when string) {
		t.Helper()
		if strings.Contains(md, "<table") {
			t.Errorf("%s: raw HTML table in the Markdown:\n%s", when, md)
		}
		for _, want := range []string{"Prvi odlomak ćelije.", "Drugi odlomak ćelije.", "prva stavka", "druga stavka", "Jedan redak."} {
			if !strings.Contains(md, want) {
				t.Errorf("%s: %q is gone from the Markdown", when, want)
			}
		}
		if !strings.Contains(md, "+---") {
			t.Errorf("%s: no grid table in the Markdown", when)
		}
	}
	check(md, "first read")

	out := filepath.Join(t.TempDir(), "out.docx")
	if err := c.MarkdownToDocx(t.Context(), work, ref, out); err != nil {
		t.Fatal(err)
	}
	// The saved document has the two paragraphs in one cell and the list.
	_, files := readZip(t, out)
	doc := string(files["word/document.xml"])
	for _, want := range []string{"Prvi odlomak ćelije.", "Drugi odlomak ćelije.", "<w:numPr>", "<w:tbl>"} {
		if !strings.Contains(doc, want) {
			t.Errorf("%s is missing from the saved document", want)
		}
	}
	work2 := t.TempDir()
	docxToMarkdown(t, c, out, work2)
	check(readMarkdown(t, work2), "after the round trip")
}

func TestLiteralTableTagDoesNotTriggerSecondPass(t *testing.T) {
	c, rec, pandoc := realConverter(t, false)
	dir := t.TempDir()
	in := filepath.Join(dir, "in.md")
	writeAt(t, in, []byte("Odlomak.\n\n\\<table border=\"1\">\n\nI još jedan: \\<table>\n"))
	docx := filepath.Join(dir, "literal.docx")
	if _, stderr, err := execRunner(t.Context(), dir, pandoc, "-f", "markdown", "-t", "docx", "-o", docx, in); err != nil {
		t.Fatalf("pandoc: %v: %s", err, stderr)
	}

	work := t.TempDir()
	docxToMarkdown(t, c, docx, work)
	md := readMarkdown(t, work)
	if !strings.Contains(md, `\<table`) {
		t.Fatalf("the literal text is not escaped in the Markdown:\n%s", md)
	}
	if strings.Contains(strings.ReplaceAll(md, `\<table`, ""), "<table") {
		t.Errorf("an unescaped tag in the Markdown:\n%s", md)
	}
	if got := rec.readsOf(); got != 1 {
		t.Errorf("pandoc read the document %d times, want 1", got)
	}
}

// Reading with markdown-smart must give the same document as reading with the
// dialect the Markdown was written in.
func TestReadDialectEquivalence(t *testing.T) {
	for _, name := range []string{"basic", "complex-table", "header-image"} {
		t.Run(name, func(t *testing.T) {
			fixedDate(t)
			c, _, pandoc := realConverter(t, false)
			src := fixturePath(name)
			work := t.TempDir()
			docxToMarkdown(t, c, src, work)
			ref := filepath.Join(work, "reference.docx")
			if err := c.MakeReference(t.Context(), src, ref); err != nil {
				t.Fatal(err)
			}
			viaConverter := filepath.Join(t.TempDir(), "a.docx")
			if err := c.MarkdownToDocx(t.Context(), work, ref, viaConverter); err != nil {
				t.Fatal(err)
			}

			written := formatPipeTables
			if name == "complex-table" {
				written = formatGridTables
			}
			direct := filepath.Join(t.TempDir(), "b.docx")
			if _, stderr, err := execRunner(t.Context(), work, pandoc, "-f", written, "-t", "docx",
				"--reference-doc="+ref, "--resource-path="+work, "-o", direct, filepath.Join(work, MarkdownName)); err != nil {
				t.Fatalf("pandoc: %v: %s", err, stderr)
			}
			a, b := zipContents(t, viaConverter), zipContents(t, direct)
			if len(a) != len(b) {
				t.Errorf("%d entries with markdown-smart, %d with %s", len(a), len(b), written)
			}
			for n := range a {
				if !bytes.Equal(a[n], b[n]) {
					t.Errorf("%s differs between reading with markdown-smart and with %s", n, written)
				}
			}
		})
	}
}

// sameBytes checks a file still has the content it had; unlike assertUnchanged
// it does not care what else is in the folder.
func sameBytes(t *testing.T, p string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s changed", p)
	}
}

// listDir returns the sorted names of a folder's entries.
func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestLegacyDocImport(t *testing.T) {
	c, _, _ := realConverter(t, true)
	textutil := c.textutilBin
	src := fixturePath("legacy")
	before, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dirBefore := listDir(t, filepath.Dir(src))

	dir := t.TempDir()
	docx := filepath.Join(dir, "legacy.docx")
	if err := c.DocToDocx(t.Context(), src, docx); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(t.Context(), docx, c.limits); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dir, "work")
	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}
	docxToMarkdown(t, c, docx, work)
	md := readMarkdown(t, work)
	for _, want := range []string{"Naslov starog dokumenta", "prva stavka liste", "Pojam"} {
		if !strings.Contains(md, want) {
			t.Errorf("%q is not in the Markdown:\n%s", want, md)
		}
	}
	after, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the .doc changed")
	}
	// Nothing was written next to the source.
	if got := listDir(t, filepath.Dir(src)); !slices.Equal(got, dirBefore) {
		t.Errorf("the source folder changed: %q, was %q", got, dirBefore)
	}

	t.Run("diacritics", func(t *testing.T) {
		dir := t.TempDir()
		txt := filepath.Join(dir, "tekst.txt")
		const text = "Hrvatski znakovi: č ć ž š đ, Č Ć Ž Š Đ.\n\nTipografski: „Dobar dan” i crtica — kraj.\n"
		writeAt(t, txt, []byte(text))
		doc := filepath.Join(dir, "tekst.doc")
		// A .doc with diacritics, made by textutil itself from a UTF-8 text file.
		if _, stderr, err := execRunner(t.Context(), dir, textutil, "-convert", "doc",
			"-inputencoding", "UTF-8", "-output", doc, txt); err != nil {
			t.Fatalf("textutil: %v: %s", err, stderr)
		}
		if info, err := os.Stat(doc); err != nil || info.Size() == 0 {
			t.Fatalf("textutil made no .doc: %v", err)
		}
		docBefore, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, "tekst.docx")
		if err := c.DocToDocx(t.Context(), doc, out); err != nil {
			t.Fatal(err)
		}
		work := filepath.Join(dir, "work")
		if err := os.Mkdir(work, 0o700); err != nil {
			t.Fatal(err)
		}
		docxToMarkdown(t, c, out, work)
		md := readMarkdown(t, work)
		for _, want := range []string{"č ć ž š đ, Č Ć Ž Š Đ", "„Dobar dan” i crtica — kraj"} {
			if !strings.Contains(md, want) {
				t.Errorf("%q is not in the Markdown:\n%s", want, md)
			}
		}
		sameBytes(t, doc, docBefore)
	})
}

// Real tools, real failures: what the default Runner and the classification of
// errors do with a pandoc and a textutil that are not what we expect.
func TestRealToolFailures(t *testing.T) {
	c, _, pandoc := realConverter(t, true)
	dir := t.TempDir()

	t.Run("pandoc on a file that is not a docx", func(t *testing.T) {
		junk := filepath.Join(dir, "junk.docx")
		writeAt(t, junk, []byte("junk"))
		work := t.TempDir()
		_, err := c.DocxToMarkdown(t.Context(), junk, work)
		var te *ToolError
		if !errors.As(err, &te) || te.Tool != "pandoc" || te.Output == "" {
			t.Fatalf("err = %v, want *ToolError with stderr", err)
		}
		sameBytes(t, junk, []byte("junk"))
		if exists(filepath.Join(work, MarkdownName)) {
			t.Error("doc.md was written for a failed conversion")
		}
	})

	t.Run("textutil on a missing file succeeds with exit code 0 and no output", func(t *testing.T) {
		// textutil answers "The file doesn't exist" with exit code 0; the file check is the only thing that notices.
		src := filepath.Join(dir, "x.doc")
		writeAt(t, src, []byte("x"))
		if err := os.Remove(src); err != nil {
			t.Fatal(err)
		}
		if err := c.DocToDocx(t.Context(), src, filepath.Join(dir, "x.docx")); err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("missing tools", func(t *testing.T) {
		missing, err := New(Options{PandocBin: filepath.Join(dir, "no-pandoc"), TextutilBin: "no-such-textutil-xyz",
			Timeout: time.Minute, Limits: realLimits})
		if err != nil {
			t.Fatal(err)
		}
		work := t.TempDir()
		if _, err := missing.DocxToMarkdown(t.Context(), fixturePath("basic"), work); !errors.Is(err, ErrToolMissing) {
			t.Errorf("pandoc: err = %v, want ErrToolMissing", err)
		}
		if err := missing.DocToDocx(t.Context(), fixturePath("legacy"), filepath.Join(dir, "m.docx")); !errors.Is(err, ErrToolMissing) {
			t.Errorf("textutil: err = %v, want ErrToolMissing", err)
		}
	})

	t.Run("the version is reported", func(t *testing.T) {
		if v := pandocVersion(pandoc); !strings.HasPrefix(v, "pandoc") {
			t.Errorf("version line = %q", v)
		}
	})
}
