package convert

import (
	"encoding/xml"
	"strings"
)

// Simplification is direct formatting that the round trip through Markdown
// flattens. Unlike a Loss it blocks nothing: nearly every Word document has
// some, so the application only warns before the first save.
type Simplification string

// The order of this list is the order of Report.Simplified.
const (
	SimplifiedAlignment       Simplification = "alignment"
	SimplifiedTextColor       Simplification = "text-color"
	SimplifiedFont            Simplification = "font"
	SimplifiedSpacing         Simplification = "spacing"
	SimplifiedIndentation     Simplification = "indentation"
	SimplifiedPageBreaks      Simplification = "page-breaks"
	SimplifiedTabs            Simplification = "tabs"
	SimplifiedFields          Simplification = "fields"
	SimplifiedTableFormatting Simplification = "table-formatting"
)

type simpSet uint16

const (
	simpAlignment simpSet = 1 << iota
	simpTextColor
	simpFont
	simpSpacing
	simpIndentation
	simpPageBreaks
	simpTabs
	simpFields
	simpTable

	simpAll = simpAlignment | simpTextColor | simpFont | simpSpacing | simpIndentation |
		simpPageBreaks | simpTabs | simpFields | simpTable
)

var simplifyOrder = [...]struct {
	bit  simpSet
	code Simplification
}{
	{simpAlignment, SimplifiedAlignment},
	{simpTextColor, SimplifiedTextColor},
	{simpFont, SimplifiedFont},
	{simpSpacing, SimplifiedSpacing},
	{simpIndentation, SimplifiedIndentation},
	{simpPageBreaks, SimplifiedPageBreaks},
	{simpTabs, SimplifiedTabs},
	{simpFields, SimplifiedFields},
	{simpTable, SimplifiedTableFormatting},
}

// maxFieldDepth bounds the nesting of fields that is tracked. Real documents
// nest a field in a field a few levels deep; a document that goes past this is
// not one where the answer matters, so it simply counts as having fields.
const maxFieldDepth = 64

// maxInstrBytes is how much of a field's instruction is kept: enough to read
// its keyword.
const maxInstrBytes = 64

// docScanner finds the formatting in word/document.xml. What an element means
// depends on its parent (w:jc in w:pPr is the alignment of a paragraph, in
// w:tblPr that of a table; w:spacing in w:pPr is the line spacing, in w:rPr the
// spacing of characters), so it keeps the path to the current element.
type docScanner struct {
	found simpSet
	// stack holds the local name of every open element that is in a
	// WordprocessingML namespace, and "" for any other.
	stack []string
	// fields is the stack of open complex fields (w:fldChar begin ... end).
	fields []fieldFrame
}

type fieldFrame struct {
	instr   []byte // the start of the instruction, from w:instrText
	decided bool   // the keyword has been judged (at w:fldChar separate or end)
}

func newDocScanner() *docScanner { return &docScanner{} }

func (s *docScanner) complete() bool { return s.found == simpAll }

func (s *docScanner) result() []Simplification {
	var out []Simplification
	for _, o := range simplifyOrder {
		if s.found&o.bit != 0 {
			out = append(out, o.code)
		}
	}
	return out
}

func isWordSpace(space string) bool { return space == nsTransitional || space == nsStrict }

// wordAttr returns the value of an attribute of the WordprocessingML namespace.
func wordAttr(t xml.StartElement, local string) (string, bool) {
	for _, a := range t.Attr {
		if a.Name.Local == local && isWordSpace(a.Name.Space) {
			return a.Value, true
		}
	}
	return "", false
}

// isOff is an on/off attribute that says "off". An absent value means "on".
func isOff(v string, present bool) bool {
	if !present {
		return false
	}
	switch strings.ToLower(v) {
	case "0", "false", "off":
		return true
	}
	return false
}

func (s *docScanner) start(t xml.StartElement) {
	parent := ""
	if n := len(s.stack); n > 0 {
		parent = s.stack[n-1]
	}
	local := ""
	if isWordSpace(t.Name.Space) {
		local = t.Name.Local
		s.element(parent, local, t)
	}
	s.stack = append(s.stack, local)
}

func (s *docScanner) end() {
	if n := len(s.stack); n > 0 {
		s.stack = s.stack[:n-1]
	}
}

func (s *docScanner) element(parent, local string, t xml.StartElement) {
	switch local {
	case "jc":
		// Left is what a paragraph has without any formatting ("start" is its strict spelling).
		if v, ok := wordAttr(t, "val"); parent == "pPr" && ok && v != "left" && v != "start" {
			s.found |= simpAlignment
		}
	case "color":
		// Black is what text has without any formatting.
		if v, ok := wordAttr(t, "val"); parent == "rPr" && ok && !strings.EqualFold(v, "auto") && !strings.EqualFold(v, "000000") {
			s.found |= simpTextColor
		}
	case "rFonts", "sz":
		if parent == "rPr" {
			s.found |= simpFont
		}
	case "spacing":
		if parent == "pPr" {
			s.found |= simpSpacing
		}
	case "ind":
		if parent == "pPr" {
			s.found |= simpIndentation
		}
	case "br":
		if v, _ := wordAttr(t, "type"); v == "page" {
			s.found |= simpPageBreaks
		}
	case "pageBreakBefore":
		if v, ok := wordAttr(t, "val"); parent == "pPr" && !isOff(v, ok) {
			s.found |= simpPageBreaks
		}
	case "sectPr":
		// A section break in a paragraph; the sectPr that ends the body is not one.
		if parent == "pPr" {
			s.found |= simpPageBreaks
		}
	case "tab":
		// The character in a run; in w:tabs it is the definition of a tab stop.
		if parent == "r" {
			s.found |= simpTabs
		}
	case "fldSimple":
		if v, _ := wordAttr(t, "instr"); !isHyperlinkField([]byte(v)) {
			s.found |= simpFields
		}
	case "fldChar":
		v, _ := wordAttr(t, "fldCharType")
		s.fldChar(v)
	case "instrText":
		if len(s.fields) == 0 {
			s.found |= simpFields // an instruction outside any field: not Word's own writing, not understood
		}
	case "shd":
		if parent == "tcPr" {
			s.found |= simpTable
		}
	case "tcBorders":
		if parent == "tcPr" {
			s.found |= simpTable
		}
	case "tblBorders":
		if parent == "tblPr" {
			s.found |= simpTable
		}
	}
}

// fldChar follows a complex field. A link written as a HYPERLINK field is not
// a simplification: pandoc reads it as a link and writes a link back. Every
// other field comes back as its last result, as plain text.
func (s *docScanner) fldChar(kind string) {
	if s.found&simpFields != 0 {
		return // nothing more to learn, and no reason to keep frames
	}
	switch kind {
	case "begin":
		if len(s.fields) >= maxFieldDepth {
			s.found |= simpFields
			s.fields = nil
			return
		}
		s.fields = append(s.fields, fieldFrame{})
	case "separate", "end":
		n := len(s.fields)
		if n == 0 {
			s.found |= simpFields
			return
		}
		f := &s.fields[n-1]
		if !f.decided {
			f.decided = true
			if !isHyperlinkField(f.instr) {
				s.found |= simpFields
			}
		}
		if kind == "end" {
			s.fields = s.fields[:n-1]
		}
	default:
		s.found |= simpFields
	}
}

// text collects the instruction of the innermost open field.
func (s *docScanner) text(b xml.CharData) {
	n := len(s.stack)
	if n == 0 || s.stack[n-1] != "instrText" || len(s.fields) == 0 {
		return
	}
	f := &s.fields[len(s.fields)-1]
	if f.decided {
		return
	}
	if room := maxInstrBytes - len(f.instr); room > 0 {
		f.instr = append(f.instr, b[:min(room, len(b))]...)
	}
}

// isHyperlinkField tells whether the field instruction starts with the keyword HYPERLINK.
func isHyperlinkField(instr []byte) bool {
	fields := strings.Fields(string(instr))
	return len(fields) > 0 && strings.EqualFold(fields[0], "HYPERLINK")
}
