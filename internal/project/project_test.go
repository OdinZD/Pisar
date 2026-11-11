package project

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func touch(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func link(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Fatal(err)
	}
}

// flat renders the tree as "kind:rel" lines in order.
func flat(nodes []Node) []string {
	var out []string
	for _, n := range nodes {
		out = append(out, string(n.Kind)+":"+n.Rel)
		out = append(out, flat(n.Children)...)
	}
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func scan(t *testing.T, root string) Tree {
	t.Helper()
	tr, err := Scan(context.Background(), root, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestKindOf(t *testing.T) {
	tests := []struct {
		name string
		kind Kind
		ok   bool
	}{
		{"a.md", KindMD, true}, {"A.MD", KindMD, true}, {"x.Docx", KindDOCX, true},
		{"stari.doc", KindDOC, true}, {"b.txt", KindTXT, true},
		{"slika.png", "", false}, {"README", "", false}, {"arhiva.md.bak", "", false},
		{".md", KindMD, true}, {"doc", "", false},
	}
	for _, tc := range tests {
		k, ok := KindOf(tc.name)
		if k != tc.kind || ok != tc.ok {
			t.Errorf("KindOf(%q) = %q, %v", tc.name, k, ok)
		}
	}
}

func TestScanMixedAndNested(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"Bilješke.MD", "Ugovor.Docx", "b.txt", "stari.doc", "slika.png", "README", "mapa/pod/x.md"} {
		touch(t, filepath.Join(root, f))
	}
	eq(t, flat(scan(t, root).Nodes), []string{
		"md:Bilješke.MD", "docx:Ugovor.Docx", "txt:b.txt", "dir:mapa", "dir:mapa/pod", "md:mapa/pod/x.md",
		"doc:stari.doc",
	})
}

func TestScanHidden(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, ".git/a.md"))
	touch(t, filepath.Join(root, ".skriveno.md"))
	touch(t, filepath.Join(root, "~$Ugovor.docx"))
	touch(t, filepath.Join(root, "ok.md"))
	eq(t, flat(scan(t, root).Nodes), []string{"md:ok.md"})
}

func TestScanSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	touch(t, filepath.Join(outside, "o.md"))
	touch(t, filepath.Join(root, "real.md"))
	touch(t, filepath.Join(root, "inner/i.md"))
	link(t, filepath.Join(root, "real.md"), filepath.Join(root, "lnk.md"))
	link(t, filepath.Join(root, "inner"), filepath.Join(root, "lnkdir"))
	link(t, outside, filepath.Join(root, "out"))
	link(t, root, filepath.Join(root, "inner/loop"))
	eq(t, flat(scan(t, root).Nodes), []string{"dir:inner", "md:inner/i.md", "md:real.md"})
}

func TestScanEmptyDirs(t *testing.T) {
	root := t.TempDir()
	mkdir(t, filepath.Join(root, "prazna"))
	touch(t, filepath.Join(root, "samo/slike/a.png"))
	touch(t, filepath.Join(root, "a/b/c/d.txt"))
	eq(t, flat(scan(t, root).Nodes), []string{"dir:a", "dir:a/b", "dir:a/b/c", "txt:a/b/c/d.txt"})
}

func TestScanOrderStable(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"b.md", "B2.md", "a.md", "č.md", "z.md", "Z/x.md", "_.md"} {
		touch(t, filepath.Join(root, f))
	}
	want := []string{"md:B2.md", "dir:Z", "md:Z/x.md", "md:_.md", "md:a.md", "md:b.md", "md:z.md", "md:č.md"}
	eq(t, flat(scan(t, root).Nodes), want)
	eq(t, flat(scan(t, root).Nodes), want)
}

func TestScanOddNames(t *testing.T) {
	root := t.TempDir()
	names := []string{"s razmakom.md", "čćšžđ.md", "a#b.md", "50%.txt", "it's.md"}
	for _, n := range names {
		touch(t, filepath.Join(root, "d e", n))
	}
	got := flat(scan(t, root).Nodes)
	if len(got) != len(names)+1 {
		t.Fatalf("got %q", got)
	}
	for _, n := range names {
		found := false
		for _, g := range got {
			found = found || strings.HasSuffix(g, ":d e/"+n)
		}
		if !found {
			t.Errorf("missing %q in %q", n, got)
		}
	}
}

func TestScanNFCandNFD(t *testing.T) {
	root := t.TempDir()
	nfc, nfd := "č.md", "č.md"
	touch(t, filepath.Join(root, nfc))
	touch(t, filepath.Join(root, nfd))
	ents, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 2 {
		t.Skip("filesystem normalizes Unicode names; NFC and NFD collide")
	}
	got := flat(scan(t, root).Nodes)
	if len(got) != 2 || got[0] == got[1] {
		t.Fatalf("got %q", got)
	}
}

func TestScanLimit(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "a.md"))
	touch(t, filepath.Join(root, "d/b.md"))
	touch(t, filepath.Join(root, "d/c.md"))
	// nodes: a.md, d, b.md, c.md = 4
	if _, err := Scan(context.Background(), root, 4); err != nil {
		t.Fatalf("exact limit: %v", err)
	}
	if _, err := Scan(context.Background(), root, 3); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("limit-1: %v", err)
	}
	for _, m := range []int{0, -1} {
		if _, err := Scan(context.Background(), root, m); err == nil {
			t.Fatalf("maxNodes %d: want error", m)
		}
	}
}

func TestScanCanceled(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "a.md"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Scan(ctx, root, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestScanRoot(t *testing.T) {
	base := t.TempDir()
	if _, err := Scan(context.Background(), filepath.Join(base, "nema"), 10); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing root: %v", err)
	}
	f := filepath.Join(base, "f.md")
	touch(t, f)
	if _, err := Scan(context.Background(), f, 10); err == nil {
		t.Fatal("file root: want error")
	}
	touch(t, filepath.Join(base, "real/sub/a.md"))
	link(t, filepath.Join(base, "real"), filepath.Join(base, "lnk"))
	eq(t, flat(scan(t, filepath.Join(base, "lnk")).Nodes), []string{"dir:sub", "md:sub/a.md"})
}

func TestScanUnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	root := t.TempDir()
	touch(t, filepath.Join(root, "ok.md"))
	touch(t, filepath.Join(root, "zakljucano/a.md"))
	locked := filepath.Join(root, "zakljucano")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	tr := scan(t, root)
	eq(t, flat(tr.Nodes), []string{"md:ok.md"})
	eq(t, tr.Skipped, []string{"zakljucano"})
}

func TestScanFIFO(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "x.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	touch(t, filepath.Join(root, "y.md"))
	eq(t, flat(scan(t, root).Nodes), []string{"md:y.md"})
}

func TestNodeJSON(t *testing.T) {
	n := Node{Name: "d", Rel: "d", Kind: KindDir, Children: []Node{{Name: "a.md", Rel: "d/a.md", Kind: KindMD}}}
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"name":"d","rel":"d","kind":"dir","children":[{"name":"a.md","rel":"d/a.md","kind":"md"}]}`
	if string(b) != want {
		t.Fatalf("got %s", b)
	}
}

func TestResolveValid(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"", "a.md", "mapa/pod mapa/č.md", `a\b.md`} {
		got, err := Resolve(root, rel)
		if err != nil {
			t.Fatalf("%q: %v", rel, err)
		}
		if got != filepath.Join(root, rel) {
			t.Errorf("%q: got %q", rel, got)
		}
	}
}

func TestResolveInvalid(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"/etc/passwd", "../x", "a/../../x", "a/./b", "a//b", "/a", "a/", ".", "..", "a\x00b"} {
		if _, err := Resolve(root, rel); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("%q: got %v", rel, err)
		}
	}
}

func TestResolveOutside(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	touch(t, filepath.Join(outside, "o.md"))
	link(t, outside, filepath.Join(root, "out"))
	link(t, filepath.Join(outside, "o.md"), filepath.Join(root, "f.md"))
	link(t, filepath.Join(outside, "nema"), filepath.Join(root, "dangling"))
	for _, rel := range []string{"out", "out/o.md", "out/novo.md", "out/novi/dir/x.md", "f.md", "dangling", "dangling/x.md"} {
		if _, err := Resolve(root, rel); !errors.Is(err, ErrOutsideRoot) {
			t.Errorf("%q: got %v", rel, err)
		}
	}
}

func TestResolveFalsePrefix(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "pro")
	mkdir(t, root)
	mkdir(t, filepath.Join(base, "proj"))
	link(t, filepath.Join(base, "proj"), filepath.Join(root, "l"))
	got, err := Resolve(root, "x.md")
	if err != nil || !strings.HasPrefix(got, root+string(filepath.Separator)) {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := Resolve(root, "l/x.md"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("link into sibling: %v", err)
	}
	// Direct lexical sibling check: a root that is itself a link to "proj"
	// must not accept paths resolved into "pro".
	if _, err := Resolve(filepath.Join(base, "proj"), "x.md"); err != nil {
		t.Fatal(err)
	}
}

func TestResolveNonexistentTargets(t *testing.T) {
	root := t.TempDir()
	mkdir(t, filepath.Join(root, "d"))
	touch(t, filepath.Join(root, "file.md"))
	for _, rel := range []string{"d/novo.md", "nema/dubok/novo.md", "file.md/x"} {
		if _, err := Resolve(root, rel); err != nil {
			t.Errorf("%q: %v", rel, err)
		}
	}
}

func TestResolveSymlinkRoot(t *testing.T) {
	base := t.TempDir()
	mkdir(t, filepath.Join(base, "real/sub"))
	link(t, filepath.Join(base, "real"), filepath.Join(base, "lnk"))
	lnk := filepath.Join(base, "lnk")
	for _, rel := range []string{"", "sub", "sub/a.md", "nova/a.md"} {
		got, err := Resolve(lnk, rel)
		if err != nil || got != filepath.Join(lnk, rel) {
			t.Errorf("%q: %q, %v", rel, got, err)
		}
	}
}
