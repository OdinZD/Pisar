package project

import (
	"fmt"
	"path"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// Decomposed (NFD) spellings, written with escapes so an editor cannot
// normalize them away. "Češalj": Č = C + U+030C, š = s + U+030C.
const (
	cesaljNFC = "Češalj"
	cesaljNFD = "Češalj"
)

func sdoc(rel string) Node {
	name := path.Base(rel)
	kind, ok := KindOf(name)
	if !ok {
		kind = KindMD
	}
	return Node{Name: name, Rel: rel, Kind: kind}
}

func sdir(rel string, children ...Node) Node {
	return Node{Name: path.Base(rel), Rel: rel, Kind: KindDir, Children: children}
}

func srcOf(id, name string, nodes ...Node) SearchSource {
	return SearchSource{ProjectID: id, ProjectName: name, Nodes: nodes}
}

// one is a single project with the given documents at the top level.
func one(names ...string) []SearchSource {
	nodes := make([]Node, len(names))
	for i, n := range names {
		nodes[i] = sdoc(n)
	}
	return []SearchSource{srcOf("p1", "Prvi", nodes...)}
}

func hitNames(hits []Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Name
	}
	return out
}

func TestSearchFolding(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		query string
		want  []string
	}{
		{"ascii query finds NFC name", []string{cesaljNFC + ".docx"}, "cesalj", []string{cesaljNFC + ".docx"}},
		{"ascii query finds NFD name", []string{cesaljNFD + ".docx"}, "cesalj", []string{cesaljNFD + ".docx"}},
		{"NFC query finds NFC name", []string{cesaljNFC + ".docx"}, "češalj", []string{cesaljNFC + ".docx"}},
		{"NFC query finds NFD name", []string{cesaljNFD + ".docx"}, "češalj", []string{cesaljNFD + ".docx"}},
		{"NFD query finds NFC name", []string{cesaljNFC + ".docx"}, cesaljNFD, []string{cesaljNFC + ".docx"}},
		{"NFD query finds NFD name", []string{cesaljNFD + ".docx"}, cesaljNFD, []string{cesaljNFD + ".docx"}},
		{"diacritic query finds plain name", []string{"Cesalj.md"}, "češalj", []string{"Cesalj.md"}},
		{"upper case query", []string{cesaljNFC + ".docx"}, "CESALJ", []string{cesaljNFC + ".docx"}},
		{"upper case diacritics in query", []string{"cesalj.md"}, "ČEŠALJ", []string{"cesalj.md"}},
		{"partial word", []string{cesaljNFC + ".docx"}, "sal", []string{cesaljNFC + ".docx"}},
		{"ć ž", []string{"Ćosić Žarko.md"}, "cosic zarko", []string{"Ćosić Žarko.md"}},
		{"d stroke finds name", []string{"Đurđevak.md"}, "durdevak", []string{"Đurđevak.md"}},
		{"d stroke query", []string{"Durdevak.md"}, "đurđevak", []string{"Durdevak.md"}},
		{"capital D stroke", []string{"Đurđevak.md"}, "ĐURĐEVAK", []string{"Đurđevak.md"}},
		{"dj does not find d stroke", []string{"Đurđevak.md"}, "djurdjevak", nil},
		{"d stroke does not find dj", []string{"Djurdjevak.md"}, "đurđevak", nil},
		{"umlauts", []string{"Müller Größe.md"}, "muller grosse", []string{"Müller Größe.md"}},
		{"sharp s expands", []string{"Straße.md"}, "strasse", []string{"Straße.md"}},
		{"stroke l", []string{"Łódź.md"}, "lodz", []string{"Łódź.md"}},
		{"tilde and acute", []string{"Español São.md"}, "espanol sao", []string{"Español São.md"}},
		{"ligature", []string{"Æsir.md"}, "aesir", []string{"Æsir.md"}},
		{"diaeresis", []string{"naïve.md"}, "naive", []string{"naïve.md"}},
		{"unrelated letters do not fold together", []string{"Zub.md"}, "sub", nil},
		{"lone combining mark query is empty", []string{"abc.md"}, "̌", nil},
		{"cyrillic is only lower-cased", []string{"Привет.md"}, "привет", []string{"Привет.md"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := hitNames(Search(one(tc.files...), tc.query, 10))
			if !slices.Equal(got, tc.want) {
				t.Errorf("Search(%q) = %q, want %q", tc.query, got, tc.want)
			}
		})
	}
}

func TestSearchWords(t *testing.T) {
	files := []string{"Plan rada 2026.md", "Rada.md", "Plan.md", "Godišnji plan.txt"}
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"one word", "rada", []string{"Rada.md", "Plan rada 2026.md"}},
		{"two words in order", "plan rada", []string{"Plan rada 2026.md"}},
		{"two words reversed", "rada plan", []string{"Plan rada 2026.md"}},
		{"mixed case", "RaDa PLAN", []string{"Plan rada 2026.md"}},
		{"extra whitespace", "  plan \t rada\n", []string{"Plan rada 2026.md"}},
		{"missing word", "plan xyz", nil},
		{"all words missing", "xyz abc", nil},
		{"same word twice", "plan plan", []string{"Plan.md", "Plan rada 2026.md", "Godišnji plan.txt"}},
		{"number", "2026", []string{"Plan rada 2026.md"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := hitNames(Search(one(files...), tc.query, 10))
			if !slices.Equal(got, tc.want) {
				t.Errorf("Search(%q) = %q, want %q", tc.query, got, tc.want)
			}
		})
	}
}

func TestSearchScope(t *testing.T) {
	sources := []SearchSource{
		srcOf("a", "Alfa",
			sdoc("a.docx"), sdoc("b.docx"), sdoc("docx-upute.md"),
			sdir("Projekt",
				sdoc("Projekt/bilješke.md"),
				sdir("Projekt/Plan", sdoc("Projekt/Plan/x.md"), sdoc("Projekt/Plan/plan.md")),
			),
		),
	}
	tests := []struct {
		name  string
		query string
		want  []string // Rel
	}{
		{"extension is not searched", "docx", []string{"docx-upute.md"}},
		{"extension md is not searched", "md", nil},
		{"folder in path is not searched", "projekt", nil},
		{"nested document with full rel", "biljes", []string{"Projekt/bilješke.md"}},
		{"folders are never returned", "plan", []string{"Projekt/Plan/plan.md"}},
		{"deep document", "x", []string{"Projekt/Plan/x.md", "docx-upute.md"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, h := range Search(sources, tc.query, 10) {
				if h.Kind == KindDir {
					t.Errorf("folder returned: %+v", h)
				}
				if h.ProjectID != "a" || h.ProjectName != "Alfa" {
					t.Errorf("wrong project in %+v", h)
				}
				got = append(got, h.Rel)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("Search(%q) rels = %q, want %q", tc.query, got, tc.want)
			}
		})
	}
}

func TestSearchHitFields(t *testing.T) {
	sources := []SearchSource{srcOf("id-7", "Sedmi", sdir("Mapa", sdoc("Mapa/Ugovor.docx")))}
	got := Search(sources, "ugov", 5)
	want := []Hit{{
		ProjectID: "id-7", ProjectName: "Sedmi", Rel: "Mapa/Ugovor.docx",
		Name: "Ugovor.docx", Kind: KindDOCX, Matches: [][2]int{{0, 4}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestSearchMatches(t *testing.T) {
	tests := []struct {
		name  string
		file  string
		query string
		want  [][2]int
	}{
		{"ascii", "Plan rada.md", "rad", [][2]int{{5, 8}}},
		{"ascii whole name", "plan.md", "plan", [][2]int{{0, 4}}},
		{"first occurrence only", "banana.txt", "an", [][2]int{{1, 3}}},
		{"case in query", "Plan rada.md", "PLAN", [][2]int{{0, 4}}},
		{"NFC diacritics", cesaljNFC + ".docx", "sal", [][2]int{{2, 5}}},
		{"NFC diacritics from start", cesaljNFC + ".docx", "ces", [][2]int{{0, 3}}},
		{"NFD range includes the mark of a matched letter", cesaljNFD + ".docx", "c", [][2]int{{0, 2}}},
		{"NFD range includes marks inside the match", cesaljNFD + ".docx", "ces", [][2]int{{0, 5}}},
		{"NFD range starts at the base letter", cesaljNFD + ".docx", "sal", [][2]int{{3, 7}}},
		{"NFD range of the last letters", cesaljNFD + ".docx", "alj", [][2]int{{5, 8}}},
		{"NFD query on NFC name", cesaljNFC + ".docx", cesaljNFD, [][2]int{{0, 6}}},
		{"touching words merge", "abcdef.md", "abc def", [][2]int{{0, 6}}},
		{"touching words merge in reverse order", "abcdef.md", "def abc", [][2]int{{0, 6}}},
		{"overlapping words merge", "abcdef.md", "abcd cdef", [][2]int{{0, 6}}},
		{"same word twice is one range", "abcdef.md", "abc abc", [][2]int{{0, 3}}},
		{"separated words stay apart", "Plan rada.md", "ra plan", [][2]int{{0, 4}, {5, 7}}},
		{"contained word", "abcdef.md", "abcdef cd", [][2]int{{0, 6}}},
		{"emoji before the match is two units", "😀abc.md", "abc", [][2]int{{2, 5}}},
		{"emoji inside the match counts as two units", "a😀b.md", "a😀b", [][2]int{{0, 4}}},
		{"emoji between words", "a😀b.md", "a b", [][2]int{{0, 1}, {3, 4}}},
		{"emoji query word", "x😀y.md", "😀", [][2]int{{1, 3}}},
		{"never reaches into the extension", "docx-upute.docx", "docx", [][2]int{{0, 4}}},
		{"extension-looking text in the stem", "a.md.md", "md", [][2]int{{2, 4}}},
		{"match ends at the end of the stem", "abc.md", "bc", [][2]int{{1, 3}}},
		{"sharp s covers the whole letter", "Straße.md", "ss", [][2]int{{4, 5}}},
		{"sharp s query", "strasse.md", "ß", [][2]int{{4, 6}}},
		{"special characters are literal", "a.b[1]*\\c.md", "[1]*\\", [][2]int{{3, 8}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Search(one(tc.file), tc.query, 5)
			if len(got) != 1 {
				t.Fatalf("Search(%q) in %q: %d hits", tc.query, tc.file, len(got))
			}
			if !reflect.DeepEqual(got[0].Matches, tc.want) {
				t.Errorf("Matches = %v, want %v", got[0].Matches, tc.want)
			}
			if got[0].Name != tc.file {
				t.Errorf("Name = %q, want it unchanged: %q", got[0].Name, tc.file)
			}
		})
	}
}

func TestSearchRanking(t *testing.T) {
	t.Run("three tiers", func(t *testing.T) {
		got := hitNames(Search(one(
			"xplan.md", "Notes plan.md", "planet earth.md", "Big plan.md", "plan.md", "mplanner.md",
		), "plan", 10))
		want := []string{"plan.md", "planet earth.md", "Big plan.md", "Notes plan.md", "xplan.md", "mplanner.md"}
		if !slices.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("word boundaries", func(t *testing.T) {
		got := hitNames(Search(one(
			"xplan.md", "a-plan.md", "a_plan.md", "a.plan.md", "(plan).md", "[plan].md", "a plan.md",
		), "plan", 10))
		// Every name but xplan has a word that starts with "plan"; "(plan)" and
		// "[plan]" start with a bracket, so their "plan" is a word start too.
		if got[len(got)-1] != "xplan.md" {
			t.Errorf("xplan.md must be last, got %q", got)
		}
		if len(got) != 7 {
			t.Errorf("got %d hits: %q", len(got), got)
		}
	})
	t.Run("tier follows the first word of the query", func(t *testing.T) {
		got := hitNames(Search(one("plan rada.md", "rada plan.md"), "rada plan", 10))
		want := []string{"rada plan.md", "plan rada.md"}
		if !slices.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("shorter name first within a tier", func(t *testing.T) {
		got := hitNames(Search(one("plan godisnji.md", "plan.md", "plan rada.md"), "plan", 10))
		want := []string{"plan.md", "plan rada.md", "plan godisnji.md"}
		if !slices.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("alphabetical at equal length, by folded name", func(t *testing.T) {
		got := hitNames(Search(one("plan c.md", "plan b.md", "plan á.md", "plan Z.md"), "plan", 10))
		want := []string{"plan á.md", "plan b.md", "plan c.md", "plan Z.md"}
		if !slices.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("length ignores the extension", func(t *testing.T) {
		got := hitNames(Search(one("plan.docx", "plan b.md"), "plan", 10))
		want := []string{"plan.docx", "plan b.md"}
		if !slices.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("NFC and NFD spelling rank the same", func(t *testing.T) {
		a := Search(one(cesaljNFC+" a.md", "Cesalj.md"), "cesalj", 10)
		b := Search(one(cesaljNFD+" a.md", "Cesalj.md"), "cesalj", 10)
		if hitNames(a)[0] != "Cesalj.md" || hitNames(b)[0] != "Cesalj.md" {
			t.Errorf("a=%q b=%q", hitNames(a), hitNames(b))
		}
	})
	t.Run("same name in two projects: by project name, then rel", func(t *testing.T) {
		sources := []SearchSource{
			srcOf("2", "Beta", sdoc("x.md")),
			srcOf("1", "Alfa", sdir("m", sdoc("m/x.md")), sdoc("x.md")),
		}
		var got []string
		for _, h := range Search(sources, "x", 10) {
			got = append(got, h.ProjectName+":"+h.Rel)
		}
		want := []string{"Alfa:m/x.md", "Alfa:x.md", "Beta:x.md"}
		if !slices.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("same name and rel: by project id", func(t *testing.T) {
		sources := []SearchSource{srcOf("b", "Isti", sdoc("x.md")), srcOf("a", "Isti", sdoc("x.md"))}
		got := Search(sources, "x", 10)
		if len(got) != 2 || got[0].ProjectID != "a" || got[1].ProjectID != "b" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("order does not depend on the order of the input", func(t *testing.T) {
		build := func(perm []int) []SearchSource {
			all := []SearchSource{
				srcOf("1", "Alfa", sdoc("plan.md"), sdoc("Plan b.md"), sdir("d", sdoc("d/plan.txt"))),
				srcOf("2", "Beta", sdoc("plan.md"), sdoc("a plan.md"), sdoc("xplan.md")),
				srcOf("3", "Gama", sdoc("plan.docx")),
			}
			out := make([]SearchSource, len(all))
			for i, p := range perm {
				out[i] = all[p]
			}
			// Reverse the documents of each project as well.
			for i := range out {
				nodes := slices.Clone(out[i].Nodes)
				slices.Reverse(nodes)
				out[i].Nodes = nodes
			}
			return out
		}
		want := Search(build([]int{0, 1, 2}), "plan", 20)
		for _, perm := range [][]int{{2, 1, 0}, {1, 2, 0}, {2, 0, 1}, {1, 0, 2}, {0, 2, 1}} {
			got := Search(build(perm), "plan", 20)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("perm %v: got %v, want %v", perm, got, want)
			}
		}
	})
}

func TestSearchLimits(t *testing.T) {
	files := []string{"e plan.md", "plan ccc.md", "plan.md", "plan bb.md", "d plan.md"}
	t.Run("empty and blank query", func(t *testing.T) {
		for _, q := range []string{"", " ", " \t\n ", " "} {
			if got := Search(one(files...), q, 10); got != nil {
				t.Errorf("Search(%q) = %v, want nil", q, got)
			}
		}
	})
	t.Run("limit zero and negative", func(t *testing.T) {
		for _, l := range []int{0, -1, -1000} {
			if got := Search(one(files...), "plan", l); got != nil {
				t.Errorf("limit %d: got %v, want nil", l, got)
			}
		}
	})
	t.Run("limit keeps the best hits", func(t *testing.T) {
		all := hitNames(Search(one(files...), "plan", 100))
		for l := 1; l <= len(all); l++ {
			got := hitNames(Search(one(files...), "plan", l))
			if !slices.Equal(got, all[:l]) {
				t.Errorf("limit %d: got %q, want %q", l, got, all[:l])
			}
		}
		if want := []string{"plan.md", "plan bb.md"}; !slices.Equal(all[:2], want) {
			t.Errorf("best two = %q, want %q", all[:2], want)
		}
	})
	t.Run("limit larger than hits", func(t *testing.T) {
		if got := Search(one(files...), "plan", 1_000_000); len(got) != len(files) {
			t.Errorf("got %d hits", len(got))
		}
	})
	t.Run("no hits is nil", func(t *testing.T) {
		if got := Search(one(files...), "zzz", 10); got != nil {
			t.Errorf("got %v", got)
		}
	})
	t.Run("empty sources", func(t *testing.T) {
		if got := Search(nil, "plan", 10); got != nil {
			t.Errorf("nil sources: %v", got)
		}
		if got := Search([]SearchSource{}, "plan", 10); got != nil {
			t.Errorf("empty sources: %v", got)
		}
		if got := Search([]SearchSource{srcOf("a", "A"), {ProjectID: "b", Nodes: []Node{}}}, "plan", 10); got != nil {
			t.Errorf("sources without nodes: %v", got)
		}
	})
	t.Run("empty and extension-only names", func(t *testing.T) {
		sources := []SearchSource{srcOf("a", "A", Node{Name: "", Rel: "", Kind: KindMD}, sdoc(".md"), sdoc("noext"))}
		if got := hitNames(Search(sources, "e", 10)); !slices.Equal(got, []string{"noext"}) {
			t.Errorf("got %q", got)
		}
	})
	t.Run("very long name", func(t *testing.T) {
		long := strings.Repeat("ab", 50_000) + "KRAJ.md"
		got := Search(one(long), "kraj", 5)
		if len(got) != 1 {
			t.Fatalf("got %d hits", len(got))
		}
		if want := [][2]int{{100_000, 100_004}}; !reflect.DeepEqual(got[0].Matches, want) {
			t.Errorf("Matches = %v, want %v", got[0].Matches, want)
		}
	})
	t.Run("query longer than the name", func(t *testing.T) {
		if got := Search(one("ab.md"), "abc", 5); got != nil {
			t.Errorf("got %v", got)
		}
		if got := Search(one("ab.md"), strings.Repeat("ab", 10_000), 5); got != nil {
			t.Errorf("got %v", got)
		}
	})
	t.Run("special characters are literal", func(t *testing.T) {
		files := []string{"abc.md", "a.b.md", "a*c.md", "a[1].md", "a\\b.md", "x.y.md"}
		tests := []struct {
			query string
			want  []string
		}{
			{".", []string{"a.b.md", "x.y.md"}}, // literal dot, not "any character"
			{"*", []string{"a*c.md"}},
			{"a*c", []string{"a*c.md"}},
			{"[", []string{"a[1].md"}},
			{"[1]", []string{"a[1].md"}},
			{"\\", []string{"a\\b.md"}},
			{".*", nil},
			{"(", nil},
		}
		for _, tc := range tests {
			got := hitNames(Search(one(files...), tc.query, 10))
			if !slices.Equal(got, tc.want) {
				t.Errorf("Search(%q) = %q, want %q", tc.query, got, tc.want)
			}
		}
	})
}

// bigSources builds 5 projects of nodes/5 nodes each, spread over nested folders.
func bigSources(nodes int) []SearchSource {
	words := []string{"izvješće", "Plan", "Češalj", "ugovor", "Đurđica", "bilješke", "Report", "Müller", "projekt", "račun"}
	exts := []string{".md", ".docx", ".txt", ".doc"}
	const projects = 5
	per := nodes / projects
	sources := make([]SearchSource, projects)
	for p := range sources {
		var top []Node
		for d := 0; d < per/100; d++ {
			dirRel := fmt.Sprintf("Mapa %d", d)
			children := make([]Node, 0, 100)
			for f := 0; f < 100; f++ {
				i := p*per + d*100 + f
				name := fmt.Sprintf("%s %s %d%s", words[i%len(words)], words[(i/7)%len(words)], i, exts[i%len(exts)])
				children = append(children, Node{Name: name, Rel: dirRel + "/" + name, Kind: KindMD})
			}
			top = append(top, Node{Name: dirRel, Rel: dirRel, Kind: KindDir, Children: children})
		}
		sources[p] = SearchSource{ProjectID: fmt.Sprint(p), ProjectName: fmt.Sprintf("Projekt %d", p), Nodes: top}
	}
	return sources
}

func BenchmarkSearch(b *testing.B) {
	sources := bigSources(100_000)
	for _, q := range []string{"izvjesce plan", "cesalj", "a"} {
		b.Run(q, func(b *testing.B) {
			for b.Loop() {
				Search(sources, q, 50)
			}
		})
	}
}

func TestSearchLargeTreeIsFast(t *testing.T) {
	sources := bigSources(100_000)
	start := time.Now()
	got := Search(sources, "izvjesce plan", 50)
	elapsed := time.Since(start)
	t.Logf("100000 nodes, two words, %d hits returned: %v", len(got), elapsed)
	if len(got) != 50 {
		t.Fatalf("got %d hits, want 50", len(got))
	}
	// A generous bound: about 11 ms plain, but ~500 ms under -race, and the
	// test must not flake on a loaded machine. It only catches quadratic code.
	if elapsed > 3*time.Second {
		t.Errorf("search took %v", elapsed)
	}
}

func FuzzSearch(f *testing.F) {
	f.Add(cesaljNFC+".docx", "cesalj", 5)
	f.Add(cesaljNFD+".md", "č š", 5)
	f.Add("😀abc.md", "abc 😀", 3)
	f.Add("Đurđevak.txt", "dj", 1)
	f.Add("Straße.md", "ß ss", 2)
	f.Add("a.b[1]*\\c.md", ".*[\\", 10)
	f.Add("\xff\xfe.md", "\xff", 4)
	f.Add("́́x.md", "́ x", 4)
	f.Add("", "", 0)
	f.Add(".md", "md", -1)

	f.Fuzz(func(t *testing.T, name, query string, limit int) {
		sources := []SearchSource{
			srcOf("a", "A", sdoc(name), sdir("d", Node{Name: name + "x", Rel: "d/" + name + "x", Kind: KindTXT})),
			srcOf("b", "B", Node{Name: name, Rel: name, Kind: KindDOCX}),
		}
		got := Search(sources, query, limit)
		if limit <= 0 {
			if got != nil {
				t.Fatalf("limit %d: got %v", limit, got)
			}
			return
		}
		if len(got) > limit {
			t.Fatalf("got %d hits for limit %d", len(got), limit)
		}
		for _, h := range got {
			limitUnits := utf16Units(stemOf(h.Name))
			prevEnd := -1
			if len(h.Matches) == 0 {
				t.Fatalf("hit without matches: %+v", h)
			}
			for _, m := range h.Matches {
				if m[0] < 0 || m[0] >= m[1] || m[1] > limitUnits {
					t.Fatalf("range %v out of bounds (stem has %d units) in %q", m, limitUnits, h.Name)
				}
				// Touching ranges are merged, so there is always a gap.
				if m[0] <= prevEnd {
					t.Fatalf("ranges not sorted/disjoint: %v", h.Matches)
				}
				prevEnd = m[1]
			}
		}
	})
}

// utf16Units counts UTF-16 units the way the JSON/JavaScript side sees the
// string: a bad byte becomes one U+FFFD.
func utf16Units(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}
