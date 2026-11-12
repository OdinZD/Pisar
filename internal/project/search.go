package project

import (
	"bytes"
	"cmp"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SearchSource is one project to search: its identity and its scanned tree.
type SearchSource struct {
	ProjectID   string
	ProjectName string
	Nodes       []Node
}

// Hit is one document that matches a query.
type Hit struct {
	ProjectID   string   `json:"projectId"`
	ProjectName string   `json:"projectName"`
	Rel         string   `json:"rel"`
	Name        string   `json:"name"` // file name with extension, exactly as in the Node
	Kind        Kind     `json:"kind"`
	Matches     [][2]int `json:"matches"` // [start, end) ranges in Name, in UTF-16 code units, sorted, not overlapping
}

// Ranking groups, best first.
const (
	tierPrefix    = iota // the name starts with the first query word
	tierWordStart        // some word of the name starts with the first query word
	tierOther            // the first query word is somewhere inside a word
)

// Search returns the documents whose names match query, best first, at most limit.
//
// Only documents are searched, by their name without the extension. The query
// is split on whitespace and every word must occur in the name as a substring,
// in any order. Comparison ignores case and diacritics (see foldTable for what
// is covered). An empty query or a limit <= 0 yields nil.
func Search(sources []SearchSource, query string, limit int) []Hit {
	if limit <= 0 {
		return nil
	}
	words := queryWords(query)
	if len(words) == 0 {
		return nil
	}

	s := searcher{words: words}
	for i := range sources {
		s.walk(&sources[i], sources[i].Nodes)
	}
	if len(s.cands) == 0 {
		return nil
	}

	// Rank everything before cutting, so the best hits survive the limit.
	slices.SortFunc(s.cands, s.compare)
	if len(s.cands) > limit {
		s.cands = s.cands[:limit]
	}

	// Highlight ranges need the slower position-tracking fold, so they are only
	// computed for the hits that are actually returned.
	hits := make([]Hit, len(s.cands))
	for i, c := range s.cands {
		hits[i] = Hit{
			ProjectID:   c.src.ProjectID,
			ProjectName: c.src.ProjectName,
			Rel:         c.node.Rel,
			Name:        c.node.Name,
			Kind:        c.node.Kind,
			Matches:     matchRanges(stemOf(c.node.Name), words),
		}
	}
	return hits
}

// candidate is a matching document. Its folded name lives in searcher.arena so
// that a query matching every node does not cost one allocation per node.
type candidate struct {
	src       *SearchSource
	node      *Node
	tier      int
	off, size int // folded stem in the arena
	runes     int // rune count of the folded stem, the "length" used for ranking
}

type searcher struct {
	words [][]byte // folded, non-empty
	arena []byte
	cands []candidate
}

func (s *searcher) walk(src *SearchSource, nodes []Node) {
	for i := range nodes {
		n := &nodes[i]
		if n.Kind == KindDir {
			s.walk(src, n.Children)
			continue
		}
		s.consider(src, n)
	}
}

// consider folds the name once, straight into the arena, and rolls the arena
// back when the node does not match.
func (s *searcher) consider(src *SearchSource, n *Node) {
	off := len(s.arena)
	s.arena = foldAppend(s.arena, stemOf(n.Name))
	folded := s.arena[off:]
	for _, w := range s.words {
		if !bytes.Contains(folded, w) {
			s.arena = s.arena[:off]
			return
		}
	}
	s.cands = append(s.cands, candidate{
		src:   src,
		node:  n,
		tier:  tierOf(folded, s.words[0]),
		off:   off,
		size:  len(folded),
		runes: utf8.RuneCount(folded),
	})
}

func (s *searcher) folded(c candidate) []byte { return s.arena[c.off : c.off+c.size] }

// compare is a total order, so the result does not depend on the order of the
// sources or on the (unstable) sort.
func (s *searcher) compare(a, b candidate) int {
	if c := cmp.Compare(a.tier, b.tier); c != 0 {
		return c
	}
	if c := cmp.Compare(a.runes, b.runes); c != 0 {
		return c
	}
	if c := bytes.Compare(s.folded(a), s.folded(b)); c != 0 {
		return c
	}
	if c := strings.Compare(a.src.ProjectName, b.src.ProjectName); c != 0 {
		return c
	}
	if c := strings.Compare(a.node.Rel, b.node.Rel); c != 0 {
		return c
	}
	return strings.Compare(a.src.ProjectID, b.src.ProjectID)
}

// tierOf ranks a folded name that is already known to contain first.
func tierOf(folded, first []byte) int {
	tier := tierOther
	for from := 0; from < len(folded); {
		i := bytes.Index(folded[from:], first)
		if i < 0 {
			break
		}
		pos := from + i
		if pos == 0 {
			return tierPrefix
		}
		// Boundaries are ASCII, so looking at one byte is safe in UTF-8.
		if isWordBoundary(folded[pos-1]) {
			tier = tierWordStart
		}
		from = pos + 1
	}
	return tier
}

func isWordBoundary(b byte) bool {
	switch b {
	case ' ', '-', '_', '.', '(', ')', '[', ']', '{', '}':
		return true
	}
	return false
}

// stemOf is the name without its extension, the only part that is searched.
func stemOf(name string) string {
	return name[:len(name)-len(filepath.Ext(name))]
}

// queryWords splits the query on whitespace and folds each word. A word that
// folds to nothing (a lone combining mark) is dropped.
func queryWords(query string) [][]byte {
	fields := strings.Fields(query)
	words := make([][]byte, 0, len(fields))
	for _, f := range fields {
		if w := foldAppend(nil, f); len(w) > 0 {
			words = append(words, w)
		}
	}
	return words
}

// matchRanges returns the highlight ranges for stem: the first occurrence of
// every word, as UTF-16 ranges in the original text, merged where they touch.
func matchRanges(stem string, words [][]byte) [][2]int {
	folded, starts, ends := foldMapped(stem)
	ranges := make([][2]int, 0, len(words))
	for _, w := range words {
		i := bytes.Index(folded, w)
		if i < 0 {
			continue // cannot happen for a candidate; never index with -1
		}
		ranges = append(ranges, [2]int{starts[i], ends[i+len(w)-1]})
	}
	if len(ranges) == 0 {
		return nil
	}
	slices.SortFunc(ranges, func(a, b [2]int) int { return cmp.Compare(a[0], b[0]) })
	merged := ranges[:1]
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		if r[0] <= last[1] {
			last[1] = max(last[1], r[1])
		} else {
			merged = append(merged, r)
		}
	}
	return merged
}

// isCombining reports whether r is in the Combining Diacritical Marks block.
func isCombining(r rune) bool { return r >= 0x0300 && r <= 0x036F }

// foldAppend appends the folded form of s to dst: lower case, without
// diacritics. Invalid UTF-8 becomes U+FFFD, one per bad byte, which is also
// what JSON encoding does to the name before the frontend sees it.
func foldAppend(dst []byte, s string) []byte {
	for i := 0; i < len(s); {
		if c := s[i]; c < utf8.RuneSelf {
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			dst = append(dst, c)
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		dst = appendFolded(dst, r)
	}
	return dst
}

func appendFolded(dst []byte, r rune) []byte {
	switch {
	case r < utf8.RuneSelf:
		if 'A' <= r && r <= 'Z' {
			r += 'a' - 'A'
		}
		return append(dst, byte(r))
	case isCombining(r):
		return dst
	}
	r = unicode.ToLower(r)
	if rep, ok := foldTable[r]; ok {
		return append(dst, rep...)
	}
	return utf8.AppendRune(dst, r)
}

// foldMapped is foldAppend that also records, for every byte of the folded
// text, the UTF-16 range of the source character it came from. A combining
// mark produces no output but extends the range of the letter before it, so
// a highlight never splits a letter from its mark.
func foldMapped(s string) (folded []byte, starts, ends []int) {
	folded = make([]byte, 0, len(s))
	starts = make([]int, 0, len(s))
	ends = make([]int, 0, len(s))
	pos := 0        // UTF-16 offset of the next source rune
	groupFirst := 0 // first folded byte of the last letter, the one marks attach to
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		units := 1
		if r >= 0x10000 {
			units = 2
		}
		from, to := pos, pos+units
		pos = to

		if isCombining(r) {
			for k := groupFirst; k < len(folded); k++ {
				ends[k] = to
			}
			continue
		}
		groupFirst = len(folded)
		folded = appendFolded(folded, r)
		for len(starts) < len(folded) {
			starts = append(starts, from)
			ends = append(ends, to)
		}
	}
	return folded, starts, ends
}

// foldTable maps lower-case Latin letters with diacritics to their plain form.
// It covers Latin-1 Supplement and Latin Extended-A plus a few letters of
// Extended-B (ǎ ǐ ǒ ǔ ș ț). Not covered: Latin Extended Additional (Vietnamese
// and the like) in precomposed form, other Extended-B letters, Greek and
// Cyrillic. Those are only lower-cased; their decomposed forms still fold,
// because combining marks are dropped.
var foldTable = buildFoldTable()

func buildFoldTable() map[rune]string {
	groups := []struct{ plain, letters string }{
		{"a", "àáâãäåāăąǎ"},
		{"c", "çćĉċč"},
		{"d", "ďđð"},
		{"e", "èéêëēĕėęě"},
		{"g", "ĝğġģ"},
		{"h", "ĥħ"},
		{"i", "ìíîïĩīĭįıǐ"},
		{"j", "ĵ"},
		{"k", "ķĸ"},
		{"l", "ĺļľŀł"},
		{"n", "ñńņňŉŋ"},
		{"o", "òóôõöøōŏőǒ"},
		{"r", "ŕŗř"},
		{"s", "śŝşšș"},
		{"t", "ţťŧț"},
		{"u", "ùúûüũūŭůűųǔ"},
		{"w", "ŵ"},
		{"y", "ýÿŷ"},
		{"z", "źżž"},
		{"ss", "ß"},
		{"ae", "æ"},
		{"oe", "œ"},
		{"ij", "ĳ"},
		{"th", "þ"},
	}
	m := make(map[rune]string, 160)
	for _, g := range groups {
		for _, r := range g.letters {
			m[r] = g.plain
		}
	}
	return m
}
