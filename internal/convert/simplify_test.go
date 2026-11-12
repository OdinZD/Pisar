package convert

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Namespaces the objects are told apart by.
const (
	nsChartT   = "http://schemas.openxmlformats.org/drawingml/2006/chart"
	nsDiagramT = "http://schemas.openxmlformats.org/drawingml/2006/diagram"
)

// runProps puts text in a paragraph with the given run properties.
func runProps(rPr string) string {
	return `<w:p><w:r><w:rPr>` + rPr + `</w:rPr><w:t>text</w:t></w:r></w:p>`
}

// para is a paragraph with the given paragraph properties.
func para(pPr string) string {
	return `<w:p><w:pPr>` + pPr + `</w:pPr><w:r><w:t>text</w:t></w:r></w:p>`
}

// field is a complex field with the given instruction chunks.
func field(instr ...string) string {
	var b strings.Builder
	b.WriteString(`<w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r>`)
	for _, i := range instr {
		b.WriteString(`<w:r><w:instrText xml:space="preserve">` + i + `</w:instrText></w:r>`)
	}
	b.WriteString(`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>result</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`)
	return b.String()
}

// TestExamineSimplified: for every code a case that has it and cases that
// look like it but are not it (another parent, another value, another namespace).
func TestExamineSimplified(t *testing.T) {
	tests := []struct {
		name  string
		inner string
		want  []Simplification
	}{
		{name: "no formatting", inner: `<w:p><w:r><w:t>text</w:t></w:r></w:p>`},

		// alignment
		{name: "jc center in a paragraph", inner: para(`<w:jc w:val="center"/>`), want: []Simplification{SimplifiedAlignment}},
		{name: "jc right", inner: para(`<w:jc w:val="right"/>`), want: []Simplification{SimplifiedAlignment}},
		{name: "jc both", inner: para(`<w:jc w:val="both"/>`), want: []Simplification{SimplifiedAlignment}},
		{name: "jc end", inner: para(`<w:jc w:val="end"/>`), want: []Simplification{SimplifiedAlignment}},
		{name: "jc left is the default", inner: para(`<w:jc w:val="left"/>`)},
		{name: "jc start is the default", inner: para(`<w:jc w:val="start"/>`)},
		{name: "jc without a value", inner: para(`<w:jc/>`)},
		{name: "jc of a table is not the alignment of a paragraph", inner: `<w:tbl><w:tblPr><w:jc w:val="center"/></w:tblPr></w:tbl>`},
		{name: "jc of a row is not the alignment of a paragraph", inner: `<w:tbl><w:tr><w:trPr><w:jc w:val="right"/></w:trPr></w:tr></w:tbl>`},

		// text colour
		{name: "red text", inner: runProps(`<w:color w:val="FF0000"/>`), want: []Simplification{SimplifiedTextColor}},
		{name: "colour of a paragraph mark", inner: para(`<w:rPr><w:color w:val="0000FF"/></w:rPr>`), want: []Simplification{SimplifiedTextColor}},
		{name: "auto colour", inner: runProps(`<w:color w:val="auto"/>`)},
		{name: "auto colour, other case", inner: runProps(`<w:color w:val="AUTO"/>`)},
		{name: "black", inner: runProps(`<w:color w:val="000000"/>`)},
		{name: "colour without a value", inner: runProps(`<w:color/>`)},
		{name: "colour outside run properties", inner: `<w:p><w:color w:val="FF0000"/></w:p>`},
		{name: "a border colour is an attribute", inner: para(`<w:pBdr><w:top w:val="single" w:color="FF0000"/></w:pBdr>`)},

		// font
		{name: "font name", inner: runProps(`<w:rFonts w:ascii="Courier New"/>`), want: []Simplification{SimplifiedFont}},
		{name: "font size", inner: runProps(`<w:sz w:val="40"/>`), want: []Simplification{SimplifiedFont}},
		{name: "complex-script size is not looked at", inner: runProps(`<w:szCs w:val="40"/>`)},
		{name: "size outside run properties", inner: `<w:r><w:sz w:val="40"/></w:r>`},
		{name: "fonts outside run properties", inner: para(`<w:rFonts w:ascii="Courier New"/>`)},

		// line spacing
		{name: "spacing of a paragraph", inner: para(`<w:spacing w:line="480" w:lineRule="auto"/>`), want: []Simplification{SimplifiedSpacing}},
		{name: "spacing of characters is not line spacing", inner: runProps(`<w:spacing w:val="20"/>`)},

		// indentation
		{name: "indented paragraph", inner: para(`<w:ind w:left="1440"/>`), want: []Simplification{SimplifiedIndentation}},
		{name: "table indent is another element", inner: `<w:tbl><w:tblPr><w:tblInd w:w="720" w:type="dxa"/></w:tblPr></w:tbl>`},
		{name: "ind outside paragraph properties", inner: runProps(`<w:ind w:left="1440"/>`)},

		// page breaks
		{name: "page break in a run", inner: `<w:p><w:r><w:br w:type="page"/></w:r></w:p>`, want: []Simplification{SimplifiedPageBreaks}},
		{name: "soft line break", inner: `<w:p><w:r><w:br/></w:r></w:p>`},
		{name: "text wrapping break", inner: `<w:p><w:r><w:br w:type="textWrapping"/></w:r></w:p>`},
		{name: "pageBreakBefore", inner: para(`<w:pageBreakBefore/>`), want: []Simplification{SimplifiedPageBreaks}},
		{name: "pageBreakBefore on", inner: para(`<w:pageBreakBefore w:val="1"/>`), want: []Simplification{SimplifiedPageBreaks}},
		{name: "pageBreakBefore off with 0", inner: para(`<w:pageBreakBefore w:val="0"/>`)},
		{name: "pageBreakBefore off with false", inner: para(`<w:pageBreakBefore w:val="false"/>`)},
		{name: "section break in a paragraph", inner: para(`<w:sectPr><w:pgSz w:w="11906"/></w:sectPr>`), want: []Simplification{SimplifiedPageBreaks}},
		{name: "the section of the body is not a break", inner: `<w:p><w:r><w:t>text</w:t></w:r></w:p><w:sectPr><w:pgSz w:w="11906"/></w:sectPr>`},

		// tabs
		{name: "tab in a run", inner: `<w:p><w:r><w:t>a</w:t><w:tab/><w:t>b</w:t></w:r></w:p>`, want: []Simplification{SimplifiedTabs}},
		{name: "tab stop definition", inner: para(`<w:tabs><w:tab w:val="left" w:pos="720"/></w:tabs>`)},

		// fields
		{name: "PAGE field", inner: field(`PAGE`), want: []Simplification{SimplifiedFields}},
		{name: "TOC field", inner: field(` TOC \o "1-3" `), want: []Simplification{SimplifiedFields}},
		{name: "simple field", inner: `<w:p><w:fldSimple w:instr=" PAGE "><w:r><w:t>1</w:t></w:r></w:fldSimple></w:p>`, want: []Simplification{SimplifiedFields}},
		{name: "HYPERLINK field is a link", inner: field(` HYPERLINK "https://example.org" `)},
		{name: "HYPERLINK field, lower case", inner: field(`hyperlink "https://example.org"`)},
		{name: "HYPERLINK split over two instructions", inner: field(`HYPER`, `LINK "https://example.org"`)},
		{name: "simple HYPERLINK field is a link", inner: `<w:p><w:fldSimple w:instr=" HYPERLINK &quot;https://example.org&quot; "><w:r><w:t>x</w:t></w:r></w:fldSimple></w:p>`},
		{name: "keyword that only starts with HYPERLINK", inner: field(`HYPERLINKS x`), want: []Simplification{SimplifiedFields}},
		{name: "another field inside a link", inner: `<w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText> HYPERLINK "x" </w:instrText></w:r>` +
			`<w:r><w:fldChar w:fldCharType="separate"/></w:r>` + field(`PAGE`) + `<w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`, want: []Simplification{SimplifiedFields}},
		{name: "a link inside another field", inner: `<w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText> PAGE </w:instrText></w:r>` +
			`<w:r><w:fldChar w:fldCharType="separate"/></w:r>` + field(`HYPERLINK "x"`) + `<w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`, want: []Simplification{SimplifiedFields}},
		{name: "separate without begin", inner: `<w:p><w:r><w:fldChar w:fldCharType="separate"/></w:r></w:p>`, want: []Simplification{SimplifiedFields}},
		{name: "end without begin", inner: `<w:p><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`, want: []Simplification{SimplifiedFields}},
		{name: "instruction outside a field", inner: `<w:p><w:r><w:instrText>PAGE</w:instrText></w:r></w:p>`, want: []Simplification{SimplifiedFields}},
		{name: "fldChar of an unknown kind", inner: `<w:p><w:r><w:fldChar w:fldCharType="other"/></w:r></w:p>`, want: []Simplification{SimplifiedFields}},
		{name: "fields nested absurdly deep", inner: strings.Repeat(`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>HYPERLINK "x"</w:instrText></w:r>`, maxFieldDepth+1),
			want: []Simplification{SimplifiedFields}},

		// tables
		{name: "cell shading", inner: `<w:tbl><w:tr><w:tc><w:tcPr><w:shd w:val="clear" w:fill="FFFF00"/></w:tcPr></w:tc></w:tr></w:tbl>`, want: []Simplification{SimplifiedTableFormatting}},
		{name: "shading of a paragraph is not table formatting", inner: para(`<w:shd w:val="clear" w:fill="FFFF00"/>`)},
		{name: "cell borders", inner: `<w:tbl><w:tr><w:tc><w:tcPr><w:tcBorders><w:top w:val="single"/></w:tcBorders></w:tcPr></w:tc></w:tr></w:tbl>`, want: []Simplification{SimplifiedTableFormatting}},
		{name: "table borders", inner: `<w:tbl><w:tblPr><w:tblBorders><w:top w:val="single"/></w:tblBorders></w:tblPr></w:tbl>`, want: []Simplification{SimplifiedTableFormatting}},
		{name: "table without borders or shading", inner: `<w:tbl><w:tblPr><w:tblStyle w:val="Table"/><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tr><w:tc><w:tcPr><w:tcW w:w="100" w:type="dxa"/></w:tcPr></w:tc></w:tr></w:tbl>`},

		{name: "everything, in the order of the list", inner: field(`PAGE`) +
			`<w:tbl><w:tblPr><w:tblBorders/></w:tblPr></w:tbl>` + `<w:p><w:r><w:tab/></w:r></w:p>` +
			para(`<w:sectPr/>`) + para(`<w:ind w:left="1"/>`) + para(`<w:spacing w:after="0"/>`) +
			runProps(`<w:sz w:val="40"/>`) + runProps(`<w:color w:val="FF0000"/>`) + para(`<w:jc w:val="center"/>`),
			want: []Simplification{
				SimplifiedAlignment, SimplifiedTextColor, SimplifiedFont, SimplifiedSpacing, SimplifiedIndentation,
				SimplifiedPageBreaks, SimplifiedTabs, SimplifiedFields, SimplifiedTableFormatting,
			}},
		{name: "the same code twice is reported once", inner: para(`<w:jc w:val="center"/>`) + para(`<w:jc w:val="right"/>`),
			want: []Simplification{SimplifiedAlignment}},
		{name: "wrapped in other elements", inner: `<w:sdt><w:sdtContent><w:p><w:hyperlink><w:r><w:tab/></w:r></w:hyperlink></w:p></w:sdtContent></w:sdt>`,
			want: []Simplification{SimplifiedTabs}},
		{name: "markup-looking text", inner: `<w:p><w:r><w:t>&lt;w:jc w:val="center"/&gt;</w:t></w:r></w:p>`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := writeFile(t, mustZip(t, []entry{docWith(tc.inner)}))
			got, err := Examine(context.Background(), p, testLimits)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Simplified, tc.want) {
				t.Fatalf("simplified = %v, want %v", got.Simplified, tc.want)
			}
			if got.Losses != nil {
				t.Fatalf("losses = %v, want none", got.Losses)
			}
		})
	}
}

func TestExamineSimplifiedNamespaces(t *testing.T) {
	strictDoc := func(inner string) entry {
		return entry{"word/document.xml", xmlDecl + `<w:document xmlns:w="` + nsStrict + `"><w:body>` + inner + `</w:body></w:document>`}
	}
	foreign := entry{"word/document.xml", xmlDecl + `<w:document xmlns:w="` + nsTransitional + `" xmlns:o="` + nsOther + `"><w:body>` +
		`<o:pPr><o:jc w:val="center"/></o:pPr><o:rPr><o:color w:val="FF0000"/><o:sz/></o:rPr><o:r><o:tab/></o:r>` +
		`<w:p xmlns:w="` + nsOther + `"><w:pPr><w:jc w:val="center"/></w:pPr></w:p></w:body></w:document>`}
	// The value is in a namespace that is not Word's: it is not the value of w:jc.
	foreignAttr := entry{"word/document.xml", xmlDecl + `<w:document xmlns:w="` + nsTransitional + `" xmlns:o="` + nsOther + `"><w:body>` +
		`<w:p><w:pPr><w:jc o:val="center"/></w:pPr></w:p></w:body></w:document>`}

	tests := []struct {
		name  string
		entry entry
		want  []Simplification
	}{
		{"strict namespace", strictDoc(`<w:p><w:pPr><w:jc w:val="end"/></w:pPr><w:r><w:tab/></w:r></w:p>`),
			[]Simplification{SimplifiedAlignment, SimplifiedTabs}},
		{"strict start is the default", strictDoc(`<w:p><w:pPr><w:jc w:val="start"/></w:pPr></w:p>`), nil},
		{"same local names in a foreign namespace", foreign, nil},
		{"attribute in a foreign namespace", foreignAttr, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := writeFile(t, mustZip(t, []entry{tc.entry}))
			got, err := Examine(context.Background(), p, testLimits)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Simplified, tc.want) {
				t.Fatalf("simplified = %v, want %v", got.Simplified, tc.want)
			}
		})
	}
}

// Only word/document.xml is looked at: what headers, footers and styles carry
// survives through the reference document.
func TestExamineReadsFormattingOnlyFromTheDocument(t *testing.T) {
	formatted := part("hdr", para(`<w:jc w:val="center"/>`)+runProps(`<w:color w:val="FF0000"/>`)+field(`PAGE`))
	p := writeFile(t, mustZip(t, []entry{plainDoc(),
		{"word/header1.xml", formatted},
		{"word/footer1.xml", formatted},
		{"word/styles.xml", part("styles", `<w:style><w:pPr><w:jc w:val="center"/></w:pPr></w:style>`)},
		{"word/footnotes.xml", formatted},
	}))
	got, err := Examine(context.Background(), p, testLimits)
	if err != nil {
		t.Fatal(err)
	}
	if got.Simplified != nil || got.Losses != nil {
		t.Fatalf("got %+v, want nothing", got)
	}
}

// word/document.xml is found by its name in any case and at any place in the archive.
func TestExamineDocumentAnywhereInTheArchive(t *testing.T) {
	p := writeFile(t, mustZip(t, []entry{
		{"word/header1.xml", part("hdr", `<w:p/>`)},
		{"Word/Document.XML", part("document", `<w:body>`+para(`<w:jc w:val="center"/>`)+`</w:body>`)},
	}))
	got, err := Examine(context.Background(), p, testLimits)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Simplification{SimplifiedAlignment}; !reflect.DeepEqual(got.Simplified, want) {
		t.Fatalf("simplified = %v, want %v", got.Simplified, want)
	}
}

// chartRef and diagramRef are a w:drawing that refers to a chart and to a SmartArt
// diagram, with the namespace of the reference as the argument.
func chartRef(ns string) string {
	return `<w:p><w:r><w:drawing><a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:graphicData>` +
		`<c:chart xmlns:c="` + ns + `"/></a:graphicData></a:graphic></w:drawing></w:r></w:p>`
}

func diagramRef(ns string) string {
	return `<w:p><w:r><w:drawing><a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:graphicData>` +
		`<dgm:relIds xmlns:dgm="` + ns + `"/></a:graphicData></a:graphic></w:drawing></w:r></w:p>`
}

func TestExamineObjects(t *testing.T) {
	objectIn := func(ns string) entry {
		return entry{"word/document.xml", xmlDecl + `<w:document xmlns:w="` + ns + `"><w:body><w:p><w:r><w:object/></w:r></w:p></w:body></w:document>`}
	}
	tests := []struct {
		name    string
		entries []entry
		want    []Loss
	}{
		{"chart", []entry{docWith(chartRef(nsChartT))}, []Loss{LossObjects}},
		{"chart, strict namespace", []entry{docWith(chartRef(nsChartStrict))}, []Loss{LossObjects}},
		{"SmartArt", []entry{docWith(diagramRef(nsDiagramT))}, []Loss{LossObjects}},
		{"SmartArt, strict namespace", []entry{docWith(diagramRef(nsDiagramStrict))}, []Loss{LossObjects}},
		{"embedded object", []entry{docWith(`<w:p><w:r><w:object w:dxaOrig="1440"/></w:r></w:p>`)}, []Loss{LossObjects}},
		{"embedded object, strict namespace", []entry{objectIn(nsStrict)}, []Loss{LossObjects}},
		{"embedded object in a footer", []entry{plainDoc(), {"word/footer1.xml", part("ftr", `<w:p><w:r><w:object/></w:r></w:p>`)}}, []Loss{LossObjects}},
		{"chart in a header", []entry{plainDoc(), {"word/header1.xml", part("hdr", chartRef(nsChartT))}}, []Loss{LossObjects}},

		{"w:object in a foreign namespace", []entry{objectIn(nsOther)}, nil},
		{"chart element in another namespace", []entry{docWith(chartRef(nsOther))}, nil},
		{"chart namespace with another element", []entry{docWith(`<w:p><w:r><c:relIds xmlns:c="` + nsChartT + `"/><c:other xmlns:c="` + nsChartT + `"/></w:r></w:p>`)}, nil},
		{"diagram namespace with another element", []entry{docWith(`<w:p><w:r><d:chart xmlns:d="` + nsDiagramT + `"/></w:r></w:p>`)}, nil},
		{"the data part of a chart is not read", []entry{plainDoc(),
			{"word/charts/chart1.xml", `<c:chartSpace xmlns:c="` + nsChartT + `"><c:chart/></c:chartSpace>`}}, nil},
		{"w:chart in the Word namespace is not a chart", []entry{docWith(`<w:chart/><w:relIds/>`)}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := writeFile(t, mustZip(t, tc.entries))
			for name, get := range map[string]func() ([]Loss, error){
				"Inspect": func() ([]Loss, error) { return Inspect(context.Background(), p, testLimits) },
				"Examine": func() ([]Loss, error) {
					r, err := Examine(context.Background(), p, testLimits)
					return r.Losses, err
				},
			} {
				got, err := get()
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Errorf("%s: losses = %v, want %v", name, got, tc.want)
				}
			}
		})
	}
}

// Inspect stops reading as soon as the four losses are found; Examine cannot,
// the formatting is in the part it is reading.
func TestExamineKeepsReadingAfterTheLosses(t *testing.T) {
	losses := `<w:comment/><w:ins/><w:txbxContent/><w:object/>`
	t.Run("the rest of the document is read", func(t *testing.T) {
		p := writeFile(t, mustZip(t, []entry{{"word/document.xml",
			xmlDecl + `<w:document xmlns:w="` + nsTransitional + `"><w:body>` + losses +
				para(`<w:jc w:val="center"/>`) + `</w:body></w:document>`}}))
		got, err := Examine(context.Background(), p, testLimits)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Losses) != 4 || !reflect.DeepEqual(got.Simplified, []Simplification{SimplifiedAlignment}) {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("Inspect stops, Examine reads on and meets the error", func(t *testing.T) {
		p := writeFile(t, mustZip(t, []entry{{"word/document.xml",
			xmlDecl + `<w:document xmlns:w="` + nsTransitional + `"><w:body>` + losses + `<w:oops></w:document>`}}))
		if got, err := Inspect(context.Background(), p, testLimits); err != nil || len(got) != 4 {
			t.Fatalf("Inspect = %v, %v", got, err)
		}
		got, err := Examine(context.Background(), p, testLimits)
		if !errors.Is(err, ErrNotDocx) || got.Losses != nil || got.Simplified != nil {
			t.Fatalf("Examine = %+v, %v; want ErrNotDocx and nothing else", got, err)
		}
	})
	t.Run("all losses and all formatting: the rest is not read", func(t *testing.T) {
		everything := field(`PAGE`) + `<w:tbl><w:tblPr><w:tblBorders/></w:tblPr></w:tbl><w:p><w:r><w:tab/></w:r></w:p>` +
			para(`<w:sectPr/>`) + para(`<w:ind w:left="1"/>`) + para(`<w:spacing w:after="0"/>`) +
			runProps(`<w:sz w:val="40"/>`) + runProps(`<w:color w:val="FF0000"/>`) + para(`<w:jc w:val="center"/>`)
		p := writeFile(t, mustZip(t, []entry{{"word/document.xml",
			xmlDecl + `<w:document xmlns:w="` + nsTransitional + `"><w:body>` + losses + everything + `<w:oops></w:document>`},
			{"word/header1.xml", "this is not XML"}}))
		got, err := Examine(context.Background(), p, testLimits)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Losses) != 4 || len(got.Simplified) != 9 {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("the document is read first, wherever it is in the archive", func(t *testing.T) {
		// The header is not XML, but everything is found in the document before it is reached.
		p := writeFile(t, mustZip(t, []entry{
			{"word/header1.xml", "this is not XML"},
			docWith(losses),
		}))
		got, err := Inspect(context.Background(), p, testLimits)
		if err != nil || len(got) != 4 {
			t.Fatalf("Inspect = %v, %v", got, err)
		}
	})
}

func TestExamineErrors(t *testing.T) {
	t.Run("not a document", func(t *testing.T) {
		got, err := Examine(context.Background(), writeFile(t, []byte("not a zip")), testLimits)
		if !errors.Is(err, ErrNotDocx) || got.Losses != nil || got.Simplified != nil {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("zero limits", func(t *testing.T) {
		got, err := Examine(context.Background(), writeFile(t, mustZip(t, []entry{plainDoc()})), InspectLimits{})
		if !errors.Is(err, ErrTooLarge) || got.Losses != nil || got.Simplified != nil {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := Examine(ctx, writeFile(t, mustZip(t, []entry{plainDoc()})), testLimits)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	})
}

// What Examine says about every sample document. The ones pandoc wrote itself
// (basic, complex-table, header-image) carry no direct formatting: a rule that
// fired on them would be too wide, because every document saved by Pisar would
// be warned about its own output.
func TestExamineFixtures(t *testing.T) {
	tests := []struct {
		file       string
		losses     []Loss
		simplified []Simplification
	}{
		{file: "basic.docx"},
		{file: "complex-table.docx"},
		{file: "header-image.docx"},
		{file: "comments.docx", losses: []Loss{LossComments}},
		{file: "tracked-changes.docx", losses: []Loss{LossTrackedChanges}},
		{file: "textbox.docx", losses: []Loss{LossTextBoxes}},
		{file: "chart.docx", losses: []Loss{LossObjects}},
		{file: "formatting.docx", simplified: []Simplification{
			SimplifiedAlignment, SimplifiedTextColor, SimplifiedFont, SimplifiedSpacing, SimplifiedIndentation,
			SimplifiedPageBreaks, SimplifiedTabs, SimplifiedFields, SimplifiedTableFormatting,
		}},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			got, err := Examine(context.Background(), filepath.Join(fixtureDir, tc.file), realLimits)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Losses, tc.losses) {
				t.Errorf("losses = %v, want %v", got.Losses, tc.losses)
			}
			if !reflect.DeepEqual(got.Simplified, tc.simplified) {
				t.Errorf("simplified = %v, want %v", got.Simplified, tc.simplified)
			}
		})
	}
	t.Run("legacy.doc is not a .docx", func(t *testing.T) {
		if _, err := Examine(context.Background(), fixturePath("legacy"), realLimits); !errors.Is(err, ErrNotDocx) {
			t.Fatalf("err = %v, want ErrNotDocx", err)
		}
	})
}

func FuzzExamine(f *testing.F) {
	f.Add([]byte(nil))
	f.Add(mustZip(f, []entry{plainDoc()}))
	f.Add(mustZip(f, []entry{docWith(para(`<w:jc w:val="center"/>`) + field(`PAGE`) + field(`HYPERLINK "x"`) + `<w:object/>`)}))
	f.Add(mustZip(f, []entry{docWith(strings.Repeat(`<w:r><w:fldChar w:fldCharType="begin"/></w:r>`, 70))}))
	p := filepath.Join(f.TempDir(), "doc.docx")
	f.Fuzz(func(t *testing.T, data []byte) {
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		lim := InspectLimits{MaxEntries: 200, MaxPartBytes: 1 << 20}
		got, err := Examine(context.Background(), p, lim)
		if err != nil {
			if !errors.Is(err, ErrNotDocx) && !errors.Is(err, ErrTooLarge) {
				t.Fatalf("unexpected error kind: %v", err)
			}
			if got.Losses != nil || got.Simplified != nil {
				t.Fatalf("result %+v alongside an error", got)
			}
			return
		}
		// The losses are those of Inspect, in the same order.
		want, ierr := Inspect(context.Background(), p, lim)
		if ierr != nil || !reflect.DeepEqual(got.Losses, want) {
			t.Fatalf("Examine losses %v, Inspect %v (err %v)", got.Losses, want, ierr)
		}
		// Fixed order, no duplicates.
		rank := map[Simplification]int{}
		for i, o := range simplifyOrder {
			rank[o.code] = i
		}
		last := -1
		for _, s := range got.Simplified {
			r, ok := rank[s]
			if !ok || r <= last {
				t.Fatalf("bad result %v", got.Simplified)
			}
			last = r
		}
	})
}
