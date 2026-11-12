package preview

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const realTimeout = 30 * time.Second

// requirePandoc returns the path of pandoc, or skips: the tests that run the
// real tool are the ones that prove the guarantees.
func requirePandoc(tb testing.TB) string {
	tb.Helper()
	p, err := exec.LookPath("pandoc")
	if err != nil {
		tb.Skip("pandoc is not installed")
	}
	return p
}

func newRenderer(t testing.TB, mod func(*Options)) (*Renderer, Options) {
	t.Helper()
	opts := Options{
		PandocBin: "pandoc",
		Dir:       filepath.Join(t.TempDir(), "preview"),
		Timeout:   realTimeout,
		MaxSource: 1 << 20,
		MaxHTML:   16 << 20,
	}
	if mod != nil {
		mod(&opts)
	}
	r, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r, opts
}

func realRenderer(t testing.TB) *Renderer {
	t.Helper()
	bin := requirePandoc(t)
	r, _ := newRenderer(t, func(o *Options) { o.PandocBin = bin })
	return r
}

func pngBytes(t testing.TB) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	img.Set(1, 1, color.RGBA{B: 200, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeFile(t testing.TB, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// --- the safety check ---------------------------------------------------------

var (
	tagRe   = regexp.MustCompile(`<[^<>]*>`)
	tagName = regexp.MustCompile(`^</?([a-z][a-z0-9:-]*)`)
	attrRe  = regexp.MustCompile(`([^\s"'<>/=]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+)))?`)
	// styleOK is what pandoc itself writes: the alignment of a table cell and the
	// width of a column or a picture.
	styleOK  = regexp.MustCompile(`^(text-align:\s*(left|right|center);?|width:\s*[0-9.]+%;?)$`)
	dataImgs = regexp.MustCompile(`src="data:image/[a-z+.-]+;base64,([A-Za-z0-9+/=]*)"`)
)

// bannedTags never occur in the page, whatever their attributes.
var bannedTags = map[string]bool{
	"script": true, "iframe": true, "object": true, "embed": true, "link": true,
	"meta": true, "style": true, "form": true, "base": true, "video": true,
	"audio": true, "source": true, "track": true, "svg": true, "frame": true,
	"frameset": true, "applet": true, "input": true, "button": true,
	"textarea": true, "select": true, "template": true,
}

// addressAttrs name an address or a file. Only a link inside the page (#...)
// and an embedded picture (data:image/...) may be there.
var addressAttrs = map[string]bool{
	"href": true, "src": true, "srcset": true, "xlink:href": true, "action": true,
	"formaction": true, "poster": true, "data": true, "background": true,
	"ping": true, "cite": true, "longdesc": true, "usemap": true, "manifest": true,
}

// unsafeIn reports what in html could run, load from the network or point out
// of the page. It reads the tags, not the text: a "<" in the text is written
// by pandoc as &lt;, so what looks like a tag in html is one, and text that
// merely mentions onerror= or javascript: is not a finding.
func unsafeIn(html []byte) []string {
	var found []string
	s := string(html)
	for _, tag := range tagRe.FindAllString(s, -1) {
		low := strings.ToLower(tag)
		m := tagName.FindStringSubmatch(low)
		if m == nil {
			found = append(found, "not a plain tag: "+tag)
			continue
		}
		if bannedTags[m[1]] {
			found = append(found, "banned tag: "+tag)
		}
		for _, a := range attrRe.FindAllStringSubmatch(strings.TrimPrefix(strings.TrimPrefix(low, "</"), "<")[len(m[1]):], -1) {
			name := a[1]
			val := a[2] + a[3] + a[4]
			switch {
			case strings.HasPrefix(name, "on"):
				found = append(found, "event attribute: "+tag)
			case name == "style":
				if !styleOK.MatchString(strings.TrimSpace(val)) {
					found = append(found, "style: "+tag)
				}
			case name == "href" && !strings.HasPrefix(val, "#"):
				found = append(found, "href leaves the page: "+tag)
			case name == "src" && !strings.HasPrefix(val, "data:image/"):
				found = append(found, "src is not an embedded picture: "+tag)
			case addressAttrs[name] && name != "href" && name != "src":
				found = append(found, "address attribute: "+tag)
			}
			if strings.Contains(val, "javascript:") || strings.Contains(val, "url(") {
				found = append(found, "script or css address in a value: "+tag)
			}
		}
	}
	// Whatever is left once the tags are gone must have no "<" in it.
	if strings.Contains(tagRe.ReplaceAllString(s, ""), "<") {
		found = append(found, `a "<" outside a tag`)
	}
	return found
}

// literalBanned is the list from the design. The first group is looked for
// anywhere in the output; the second only inside tags, because a document may
// quote them as text ("javascript:alert(1)" typed as a link's text, a CSS rule
// shown in a code block) and then they are not an attack.
var (
	literalBanned = []string{
		"<script", "<iframe", "<object", "<embed", "<link", "<meta", "<style", "<form", "<base",
		`href="http`, `src="http`, `src="//`, `src="/`,
	}
	literalBannedInTags = []string{"javascript:", "srcset", "url("}
)

func literalHits(html []byte) []string {
	low := strings.ToLower(string(html))
	tags := strings.Join(tagRe.FindAllString(low, -1), "\n")
	var hits []string
	for _, b := range literalBanned {
		if strings.Contains(low, b) {
			hits = append(hits, b)
		}
	}
	for _, b := range literalBannedInTags {
		if strings.Contains(tags, b) {
			hits = append(hits, b)
		}
	}
	return hits
}

// requireSafe fails when html has anything unsafeIn finds.
func requireSafe(t testing.TB, html []byte) {
	t.Helper()
	if found := unsafeIn(html); len(found) > 0 {
		t.Fatalf("unsafe output:\n%s\n--- output:\n%s", strings.Join(found, "\n"), html)
	}
}

func TestUnsafeInFindsWhatItShould(t *testing.T) {
	tests := []struct {
		name string
		html string
		want bool
	}{
		{"plain", `<p>hi <a href="#fn1" class="footnote-ref" role="doc-noteref">1</a></p>`, false},
		{"text that mentions attacks", `<p>&lt;script&gt; onerror=1 javascript:x url(y)</p>`, false},
		{"embedded picture", `<img src="data:image/png;base64,AAAA" alt="x" />`, false},
		{"table styles", `<th style="text-align: left;">a</th><col style="width: 33%" />`, false},
		{"script", `<p>a</p><SCRIPT>1</SCRIPT>`, true},
		{"onerror", `<img src="data:image/png;base64,AA" onerror="x">`, true},
		{"ON in upper case", `<p ONCLICK=x>a</p>`, true},
		{"remote src", `<img src="http://h/x.png">`, true},
		{"relative src", `<img src="nope.png">`, true},
		{"remote href", `<a href="https://h">x</a>`, true},
		{"javascript href", `<a href="javascript:alert(1)">x</a>`, true},
		{"css address", `<span style="background:url(http://h/x)">x</span>`, true},
		{"srcset", `<img srcset="a 1x" src="data:image/png;base64,AA">`, true},
		{"comment", `<!-- x --><p>a</p>`, true},
		{"unbalanced", `<p>a < b</p>`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(unsafeIn([]byte(tc.html))) > 0; got != tc.want {
				t.Fatalf("unsafeIn(%q) = %v, want %v", tc.html, unsafeIn([]byte(tc.html)), tc.want)
			}
		})
	}
}

// --- a document folder for the hostile inputs ------------------------------------

// site is a document folder with one picture, a picture next to it (outside the
// folder) and a secret file the output must never contain.
type site struct {
	docDir string // the resourceDir
	outer  string // its parent
	secret string // absolute path of a file outside docDir
	png    []byte
}

const secretMarker = "SECRET-OUTSIDE-THE-FOLDER"

func newSite(t testing.TB) site {
	t.Helper()
	root := t.TempDir()
	s := site{docDir: filepath.Join(root, "doc"), outer: root, png: pngBytes(t)}
	writeFile(t, filepath.Join(s.docDir, "a.png"), s.png)
	// Files of types pandoc would turn into <video>, <audio> and <embed>.
	for _, name := range []string{"a.mp4", "a.pdf", "a.mp3"} {
		writeFile(t, filepath.Join(s.docDir, name), s.png)
	}
	// Valid picture bytes with a marker in them, so a leak is recognisable in
	// the output both as base64 and as a file that was read.
	secret := append(append([]byte{}, s.png...), secretMarker...)
	writeFile(t, filepath.Join(root, "tajna.png"), secret)
	writeFile(t, filepath.Join(root, "secret.txt"), []byte(secretMarker))
	writeFile(t, filepath.Join(root, "secret.tex"), []byte(secretMarker))
	s.secret = filepath.Join(root, "tajna.png")
	return s
}

// requireNoLeak fails when the output carries anything of the files outside the
// folder.
func (s site) requireNoLeak(t testing.TB, html []byte) {
	t.Helper()
	secret, err := os.ReadFile(s.secret)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(html, []byte(secretMarker)) || bytes.Contains(html, []byte(base64.StdEncoding.EncodeToString(secret))) {
		t.Fatalf("a file outside the document folder is in the output:\n%s", html)
	}
	for _, img := range dataImgs.FindAllStringSubmatch(string(html), -1) {
		got, err := base64.StdEncoding.DecodeString(img[1])
		if err != nil {
			t.Fatalf("embedded picture is not base64: %v", err)
		}
		if !bytes.Equal(got, s.png) {
			t.Fatalf("an embedded picture is not the one in the folder:\n%s", html)
		}
	}
}

// hostileInputs are documents that try to run something, load something or show
// a file from elsewhere. base is where a remote address points; abs is the path
// of a file outside the folder.
func hostileInputs(base string, s site) []struct{ name, md string } {
	enc := "%2F" + strings.TrimPrefix(s.secret, "/")
	return []struct{ name, md string }{
		{"raw script", "<script>alert(1)</script>\n"},
		{"raw script inline", "text <script>alert(1)</script> text\n"},
		{"img onerror", `<img src=x onerror=alert(1)>` + "\n"},
		{"img onerror inline", `before <img src="x" onerror="alert(1)"> after` + "\n"},
		{"javascript link", "[x](javascript:alert(1))\n"},
		{"javascript link in caps", "[x](JaVaScRiPt:alert(1))\n"},
		{"javascript autolink", "<javascript:alert(1)>\n"},
		{"https link", "[x](" + base + "/page)\n"},
		{"remote image", "![x](" + base + "/x.png)\n"},
		{"remote image inline", "a ![x](" + base + "/x.png) b\n"},
		{"scheme-relative image", "![x](//127.0.0.1/x.png)\n"},
		{"absolute image", "![x](/etc/hosts)\n"},
		{"absolute image existing", "![x](" + s.secret + ")\n"},
		{"home image", "![x](~/x.png)\n"},
		{"file image", "![x](file://" + s.secret + ")\n"},
		{"parent image", "![x](../tajna.png)\n"},
		{"parent image deeper", "![x](a/../../tajna.png)\n"},
		{"parent image backslash", "![x](..\\tajna.png)\n"},
		{"parent image encoded", "![x](%2e%2e/tajna.png)\n"},
		{"parent image encoded slash", "![x](..%2ftajna.png)\n"},
		{"absolute image encoded", "![x](" + enc + ")\n"},
		{"data image", "![x](data:image/png;base64," + base64.StdEncoding.EncodeToString(s.png) + ")\n"},
		{"data html", "![x](data:text/html;base64,PHNjcmlwdD4xPC9zY3JpcHQ+)\n"},
		{"video", "![x](a.mp4)\n"},
		{"pdf", "![x](a.pdf)\n"},
		{"audio", "![x](a.mp3)\n"},
		{"autolink", "<" + base + "/x>\n"},
		{"email autolink", "<a@b.c>\n"},
		{"reference link", "[x][r]\n\n[r]: " + base + "/x\n"},
		{"reference image", "![x][r]\n\n[r]: " + base + "/x.png\n"},
		{"raw comment", "<!-- <script>alert(1)</script> -->\n"},
		{"html style block", "<style>body{background:url(" + base + "/x)}</style>\n"},
		{"html style inline", "a <style>p{}</style> b\n"},
		{"html link tag", `<link rel="stylesheet" href="` + base + `/x.css">` + "\n"},
		{"html meta refresh", `<meta http-equiv="refresh" content="0;url=` + base + `">` + "\n"},
		{"html base", `<base href="` + base + `/">` + "\n"},
		{"html form", `<form action="` + base + `"><input name=a></form>` + "\n"},
		{"html iframe", `<iframe src="` + base + `"></iframe>` + "\n"},
		{"html object", `<object data="` + base + `/x"></object>` + "\n"},
		{"html embed", `<embed src="` + base + `/x">` + "\n"},
		{"html svg", `<svg onload="alert(1)"><script>1</script></svg>` + "\n"},
		{"html anchor", `<a href="javascript:alert(1)" onclick="x">y</a>` + "\n"},
		{"html div", `<div onclick="x" style="background:url(` + base + `/x)">d</div>` + "\n"},
		{"html span", `<span onmouseover="x">s</span>` + "\n"},
		{"html table", `<table onclick="x"><tr><td style="background:url(` + base + `/x)">1</td></tr></table>` + "\n"},
		{"span attributes", `[tekst]{onclick="x" style="background:url(` + base + `/x)" .c #i}` + "\n"},
		{"image attributes", `![x](a.png){onerror=alert(1) style="background:url(` + base + `/x)" width=10}` + "\n"},
		{"image attributes missing file", `![x](nope.png){onerror=alert(1) srcset="` + base + `/x 1x"}` + "\n"},
		{"width injection", `![x](a.png){width="10px;background:url(` + base + `/x)"}` + "\n"},
		{"heading attributes", "# Naslov {onclick=1 style=\"a\" .z #q}\n"},
		{"code block attributes", "```{.py onclick=3 #q include=\"" + s.outer + "/secret.txt\"}\ncode\n```\n"},
		{"code span attributes", "`code`{onclick=2}\n"},
		{"div attributes", "::: {onclick=1 style=\"a\" .d}\ntxt\n:::\n"},
		{"table attributes", "| a | b |\n|:--|--:|\n| 1 | 2 |\n\n: cap {onclick=1 .x}\n"},
		{"code include", "```{include=\"" + s.outer + "/secret.txt\"}\n```\n"},
		{"code include class", "~~~ {.c include=\"" + s.outer + "/secret.txt\"}\n~~~\n"},
		{"tex input", "\\input{" + s.outer + "/secret.tex}\n"},
		{"tex include", "\\include{" + s.outer + "/secret.tex}\n"},
		{"tex href", "\\href{javascript:alert(1)}{x}\n"},
		{"tex block", "\\begin{html}<script>1</script>\\end{html}\n"},
		{"math input", "$\\input{" + s.outer + "/secret.tex}$\n"},
		{"math href", "$\\href{javascript:alert(1)}{x}$\n"},
		{"math text script", "$\\text{<script>alert(1)</script>}$\n"},
		{"display math", "$$\\includegraphics{" + s.secret + "}$$\n"},
		{"raw attribute html", "```{=html}\n<script>alert(1)</script>\n```\n"},
		{"raw attribute inline", "`<script>alert(1)</script>`{=html}\n"},
		{"raw attribute tex", "```{=latex}\n\\input{" + s.outer + "/secret.tex}\n```\n"},
		{"yaml metadata", "---\ntitle: <script>x</script>\nheader-includes: <script>1</script>\ninclude-before: <script>2</script>\n---\n\nbody\n"},
		{"footnote with link", "a[^1]\n\n[^1]: <script>x</script> [l](" + base + "/x)\n"},
		{"inline note", "a^[<img src=x onerror=1>]\n"},
		{"linked image", "[![x](a.png)](" + base + "/x)\n"},
		{"image title", `![x](a.png "<script>alert(1)</script>")` + "\n"},
		{"image alt", "![<script>alert(1)</script>](nope.png)\n"},
		{"link title", `[x](a "<script>alert(1)</script>")` + "\n"},
		{"definition list", "term\n:   <b onclick=1>def</b>\n"},
		{"line block", "| <script>1</script>\n| second\n"},
		{"citation", "[@key]\n"},
		{"html script block", "<script>alert(1)</script>\n"},
		{"html script in a line", "a <script>alert(1)</script> b\n"},
		{"html img with http and onerror", `a <img src="http://127.0.0.1:9/y.png" onerror="alert(1)"> b` + "\n"},
		{"html span and div with attributes", `<span onclick=1 style="background:url(http://127.0.0.1:9/x)">tekst</span> i <div onclick=1>d</div>` + "\n"},
		{"html div with markdown", "<div onclick=1 style=\"x\">\n\nd *em*\n\n</div>\n"},
		{"html javascript anchor", `<a href="javascript:alert(1)">x</a>` + "\n"},
		{"html all dangerous tags", "<iframe src=http://127.0.0.1:9></iframe>\n\n<object data=x></object>\n\n<embed src=x>\n\n<link rel=stylesheet href=x>\n\n<meta http-equiv=refresh content=\"0;url=http://127.0.0.1:9\">\n\n<style>p{background:url(http://127.0.0.1:9)}</style>\n\n<form action=x><input name=a></form>\n\n<base href=http://127.0.0.1:9>\n"},
		{"html svg onload", "<svg onload=alert(1)><script>1</script></svg>\n"},
		{"html math with href", `<math><mi href="javascript:1" xlink:href="http://127.0.0.1:9">x</mi></math>` + "\n"},
		{"html details ontoggle", "<details ontoggle=alert(1) open><summary>s</summary>body</details>\n"},
		{"html unclosed and misnested", "<div><span>unclosed\n\n</div></span><b><i>x</b></i>\n"},
		{"html entities", "&lt;script&gt;alert(1)&lt;/script&gt; &#60;script&#62; &amp;lt;img src=x onerror=1&amp;gt;\n"},
		{"html markdown inside a block", "<div>\n*md* in block\n</div>\n\n<div markdown=1>*md*</div>\n"},
		{"html comment tricks", "a\n\n<!-- -->\n\nb\n\n<!-- x --> <!-- --><script>1</script> -->\n\n<![CDATA[<script>1</script>]]>\n\n<!-- a -- b --!><script>1</script>\n"},
		{"html comment between lists", "- a\n\n<!-- -->\n\n- b\n"},
		{"html comment with a tag in it", "<!-- <img src=x onerror=1> -->\n\ntext <!-- <script> --> more\n"},
		{"math tricks", "$\\color{red}x$ $\\htmlClass{a}{b}$ $\\hbox{<img src=x onerror=1>}$ $\\text{\"><script>}$ \\(\\input{x}\\) $\\href{http://x}{y}$\n"},
		{"span classes", "[x]{.smallcaps} [y]{.underline} [z]{.mark} [v]{style=\"color:red\" .smallcaps}\n"},
		{"definition and line block attributes", "term\n:   def {onclick=1}\n\n| l1\n| l2 [x]{onclick=1}\n"},
		{"grid table attributes", "+---+---+\n| a | b |\n+===+===+\n| 1 {onclick=1} | 2 |\n+---+---+\n\n: cap {onclick=1}\n"},
		{"pipe table cell span", "| a | b |\n|---|---|\n| [x]{onclick=1} | 2 |\n"},
		{"fenced div raw", "::: {=html}\n<script>1</script>\n:::\n\n::: {.a .b onclick=1 data-x=2}\ntext\n:::\n"},
		{"all attribute kinds", "# x {#a .b key=val onclick=1}\n\n![x](a.png){#i .c key=v}\n\n[l]{key=val lang=hr dir=rtl}\n"},
		{"autolink attributes", "<https://x>{onclick=1}\n\n[a](b){onclick=1}\n"},
		{"numbered code", "``` {.sourceCode .numberLines startFrom=3 onclick=1}\nx\n```\n"},
		{"task list", "- [ ] task\n- [x] done\n"},
		{"tex macro", "\\newcommand{\\x}{<script>}\n$\\x$\n"},
		{"empty", ""},
	}
}

// --- Render: safety --------------------------------------------------------------

// countingServer is a local HTTP server that counts what reaches it. A
// document's remote address points here, and none must arrive.
func countingServer(t *testing.T) (url string, hits *atomic.Int64) {
	t.Helper()
	hits = new(atomic.Int64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.NotFound(w, nil)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, hits
}

// noProxy keeps pandoc from sending a request anywhere but where it points. The
// variables are removed, not emptied: pandoc takes an empty proxy for a broken one.
func noProxy(t *testing.T) {
	t.Helper()
	for _, k := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(k, "") // registers the restore of the old value
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRenderSafety(t *testing.T) {
	noProxy(t)
	r := realRenderer(t)
	base, hits := countingServer(t)
	s := newSite(t)

	for _, tc := range hostileInputs(base, s) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			html, err := r.Render(context.Background(), []byte(tc.md), s.docDir)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			requireSafe(t, html)
			if hit := literalHits(html); len(hit) > 0 {
				t.Fatalf("output contains %q:\n%s", hit, html)
			}
			s.requireNoLeak(t, html)
		})
	}
	// Cleanup runs once the parallel subtests are done.
	t.Cleanup(func() {
		if n := hits.Load(); n != 0 {
			t.Errorf("pandoc made %d request(s) to the local server, want 0", n)
		}
	})
}

// TestRenderDoesNotFetch has a control: pandoc without the filter does fetch
// the address, so a count of zero with the filter means something.
func TestRenderDoesNotFetch(t *testing.T) {
	noProxy(t)
	bin := requirePandoc(t)
	base, hits := countingServer(t)
	md := "![x](" + base + "/x.png)\n"

	t.Run("control: plain pandoc fetches", func(t *testing.T) {
		cmd := exec.Command(bin, "-f", "markdown", "-t", "html5", "--embed-resources")
		cmd.Stdin = strings.NewReader(md)
		_ = cmd.Run() // it may fail on the 404; the request is what is counted
		if hits.Load() == 0 {
			t.Fatal("the control run made no request, so a count of zero proves nothing")
		}
	})

	hits.Store(0)
	s := newSite(t)
	r := realRenderer(t)
	for _, tc := range []struct{ name, md, dir string }{
		{"image", md, s.docDir},
		{"image without a folder", md, ""},
		{"link", "[x](" + base + "/x)\n", s.docDir},
		{"autolink", "<" + base + "/x>\n", s.docDir},
		{"raw img", `<img src="` + base + `/x.png">` + "\n", s.docDir},
		{"raw link", `<link rel="stylesheet" href="` + base + `/x.css">` + "\n", s.docDir},
		{"reference", "![x][r]\n\n[r]: " + base + "/x.png\n", s.docDir},
	} {
		t.Run(tc.name, func(t *testing.T) {
			html, err := r.Render(context.Background(), []byte(tc.md), tc.dir)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			requireSafe(t, html)
		})
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("Render made %d request(s) to the local server, want 0", n)
	}
}

// --- Render: output --------------------------------------------------------------

func TestRenderMarkup(t *testing.T) {
	r := realRenderer(t)
	tests := []struct {
		name   string
		md     string
		want   []string
		absent []string
	}{
		{"headings", "# Jedan\n\n## Dva\n", []string{"<h1", ">Jedan</h1>", "<h2", ">Dva</h2>"}, nil},
		{"emphasis", "**podebljano** i *kurziv*\n", []string{"<strong>podebljano</strong>", "<em>kurziv</em>"}, nil},
		{"list", "- a\n- b\n\n1. c\n2. d\n", []string{"<ul>", "<li>a</li>", `<ol type="1">`, "<li>c</li>"}, nil},
		{"table", "| a | b |\n|---|---|\n| 1 | 2 |\n", []string{"<table>", "<th>a</th>", "<td>2</td>"}, nil},
		{"footnote", "tekst[^1]\n\n[^1]: bilješka\n", []string{`<a href="#fn1"`, `role="doc-endnotes"`, "bilješka"}, nil},
		{"formula", "$x^2 + y^2 = z^2$\n", []string{"<math", "<msup>"}, []string{"$x^2"}},
		{"display formula", "$$\\frac{a}{b}$$\n", []string{"<math", `display="block"`, "<mfrac>"}, nil},
		{"code block", "```go\nfunc main() { x := \"<a>\" }\n```\n", []string{`<pre class="go"><code>`, "&lt;a&gt;"}, []string{"<span"}},
		{"inline code", "`a < b`\n", []string{"<code>a &lt; b</code>"}, nil},
		{"diacritics", "Čapljina, šuma, žaba, đak, ćup: ČĆŽŠĐ čćžšđ\n", []string{"Čapljina, šuma, žaba, đak, ćup: ČĆŽŠĐ čćžšđ"}, nil},
		{"typographic quotes", "„hrvatski“ i “english” i ‘single’ – pauza … točke\n", []string{"„hrvatski“ i “english” i ‘single’ – pauza … točke"}, nil},
		{"straight quotes stay", "\"ravni\" i 'ravni' -- dvije ... tri\n", []string{`&quot;ravni&quot; i &#39;ravni&#39;`, "-- dvije ... tri"}, []string{"“", "”", "–", "…"}},
		{"comment between lists is gone", "- a\n\n<!-- -->\n\n- b\n", []string{"<li>a</li>", "<li>b</li>"}, []string{"&lt;!--", "<!--", "-->"}},
		{"comment inside a paragraph is gone", "pred <!-- skriveno --> poslije\n", []string{"pred", "poslije"}, []string{"skriveno", "&lt;!--"}},
		{"html script leaves no trace", "<script>alert(1)</script>\n\nviše <script>alert(2)</script> teksta\n", []string{"teksta"}, []string{"alert", "script"}},
		{"html span and div keep their text", `<span onclick=1>tekst</span>` + "\n\n" + `<div onclick=1>blok</div>` + "\n", []string{"tekst", "blok"}, []string{"onclick"}},
		{"html anchor keeps its text, not its address", `<a href="javascript:alert(1)">poveznica</a>` + "\n", []string{"poveznica"}, []string{"href", "javascript"}},
		{"entities stay text", "&lt;script&gt;\n", []string{"&lt;script&gt;"}, []string{"<script"}},
		{"task list is text, not a control", "- [ ] jedan\n- [x] dva\n", []string{`<span class="task-box">☐</span> jedan`, `<span class="task-box">☒</span> dva`}, []string{"<input"}},
		{"blockquote", "> citat\n", []string{"<blockquote>", "citat"}, nil},
		{"horizontal rule", "a\n\n---\n\nb\n", []string{"<hr"}, nil},
		{"empty", "", nil, nil},
		{"only spaces", "  \n\n", nil, nil},
		{"hard line break", "a\\\nb\n", []string{"<br"}, nil},
		{"link becomes text", "[tekst](https://example.com/x)\n", []string{`<span class="link">tekst</span>`}, []string{"<a "}},
		{"escaped text", "a & b < c > d\n", []string{"a &amp; b &lt; c &gt; d"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			html, err := r.Render(context.Background(), []byte(tc.md), "")
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			requireSafe(t, html)
			for _, w := range tc.want {
				if !strings.Contains(string(html), w) {
					t.Errorf("output lacks %q:\n%s", w, html)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(string(html), a) {
					t.Errorf("output has %q:\n%s", a, html)
				}
			}
		})
	}
}

func TestRenderIsBodyOnly(t *testing.T) {
	r := realRenderer(t)
	html, err := r.Render(context.Background(), []byte("# T\n\ntext\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"<html", "<head", "<body", "<!DOCTYPE", "<title"} {
		if strings.Contains(strings.ToLower(string(html)), strings.ToLower(w)) {
			t.Errorf("output is a whole document (%s):\n%s", w, html)
		}
	}
}

func TestRenderAttributes(t *testing.T) {
	r := realRenderer(t)
	s := newSite(t)
	tests := []struct {
		name   string
		md     string
		want   []string
		absent []string
	}{
		{"class survives on a span", `[t]{.c onclick="x" #i}` + "\n", []string{`<span class="c">t</span>`}, []string{"onclick", `id=`}},
		{"class survives on a heading", "# H {.z #q onclick=1}\n", []string{`<h1 class="z">H</h1>`}, []string{"onclick", `id=`}},
		{"class survives on code", "```{.py #q onclick=3}\ncode\n```\n", []string{`<pre class="py"><code>code</code></pre>`}, []string{"onclick", `id=`}},
		{"div", "::: {.d onclick=1 style=\"a\"}\ntxt\n:::\n", []string{`<div class="d">`}, []string{"onclick", "style"}},
		{"table", "| a |\n|---|\n| 1 |\n\n: c {.x onclick=1}\n", []string{`<table class="x">`}, []string{"onclick"}},
		{"image keeps its size", "![x](a.png){width=10 height=20 onerror=1 .k}\n", []string{`width="10"`, `height="20"`, `class="k"`}, []string{"onerror"}},
		{"image drops a hostile size", `![x](a.png){width="1;background:url(x)" height="2)"}` + "\n", []string{"data:image/png"}, []string{"background", "url("}},
		{"alignment is pandoc's own", "| a | b |\n|:--|--:|\n| 1 | 2 |\n", []string{`style="text-align: left;"`, `style="text-align: right;"`}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			html, err := r.Render(context.Background(), []byte(tc.md), s.docDir)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			requireSafe(t, html)
			for _, w := range tc.want {
				if !strings.Contains(string(html), w) {
					t.Errorf("output lacks %q:\n%s", w, html)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(string(html), a) {
					t.Errorf("output has %q:\n%s", a, html)
				}
			}
		})
	}
}

func TestRenderNoFileIsRead(t *testing.T) {
	r := realRenderer(t)
	s := newSite(t)
	for _, name := range []string{"code include", "code include class", "tex input", "tex include", "math input", "display math", "absolute image existing", "raw attribute tex"} {
		var md string
		for _, in := range hostileInputs("http://127.0.0.1:1", s) {
			if in.name == name {
				md = in.md
			}
		}
		if md == "" {
			t.Fatalf("no input %q", name)
		}
		t.Run(name, func(t *testing.T) {
			html, err := r.Render(context.Background(), []byte(md), s.docDir)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if bytes.Contains(html, []byte(secretMarker)) {
				t.Fatalf("a file was read into the output:\n%s", html)
			}
			s.requireNoLeak(t, html)
		})
	}
}

// --- Render: pictures ------------------------------------------------------------

// embedded returns the bytes of every picture embedded in html.
func embedded(t *testing.T, html []byte) [][]byte {
	t.Helper()
	var out [][]byte
	for _, m := range dataImgs.FindAllSubmatch(html, -1) {
		b, err := base64.StdEncoding.DecodeString(string(m[1]))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

func TestRenderImages(t *testing.T) {
	pandocBin := requirePandoc(t)
	newR := func(t *testing.T) (*Renderer, Options) {
		return newRenderer(t, func(o *Options) { o.PandocBin = pandocBin })
	}

	t.Run("a local picture is embedded byte for byte", func(t *testing.T) {
		r, _ := newR(t)
		s := newSite(t)
		html, err := r.Render(context.Background(), []byte("![opis](a.png)\n"), s.docDir)
		if err != nil {
			t.Fatal(err)
		}
		requireSafe(t, html)
		if !strings.Contains(string(html), `src="data:image/png;base64,`) {
			t.Fatalf("not embedded:\n%s", html)
		}
		got := embedded(t, html)
		if len(got) != 1 || !bytes.Equal(got[0], s.png) {
			t.Fatalf("embedded bytes differ from the file")
		}
	})

	t.Run("a picture in a subfolder, a name with spaces and diacritics", func(t *testing.T) {
		r, _ := newR(t)
		s := newSite(t)
		writeFile(t, filepath.Join(s.docDir, "slike", "moja šuma.png"), s.png)
		html, err := r.Render(context.Background(), []byte("![a](<slike/moja šuma.png>)\n\n![b](slike/moja%20šuma.png)\n"), s.docDir)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(embedded(t, html)); n != 2 {
			t.Fatalf("embedded %d pictures, want 2:\n%s", n, html)
		}
	})

	t.Run("a folder with spaces and diacritics", func(t *testing.T) {
		r, _ := newR(t)
		s := newSite(t)
		dir := filepath.Join(s.outer, "Moje bilješke ć")
		writeFile(t, filepath.Join(dir, "s.png"), s.png)
		html, err := r.Render(context.Background(), []byte("![a](s.png)\n"), dir)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(embedded(t, html)); n != 1 {
			t.Fatalf("embedded %d pictures, want 1:\n%s", n, html)
		}
	})

	t.Run("image types", func(t *testing.T) {
		r, _ := newR(t)
		s := newSite(t)
		for _, name := range []string{"a.jpg", "a.JPEG", "a.gif", "a.webp", "a.svg", "A.PNG"} {
			writeFile(t, filepath.Join(s.docDir, name), s.png)
		}
		html, err := r.Render(context.Background(), []byte("![](a.jpg) ![](a.JPEG) ![](a.gif) ![](a.webp) ![](a.svg) ![](A.PNG)\n"), s.docDir)
		if err != nil {
			t.Fatal(err)
		}
		requireSafe(t, html)
		if n := len(embedded(t, html)); n != 6 {
			t.Fatalf("embedded %d pictures, want 6:\n%s", n, html)
		}
	})

	t.Run("what is not a picture is not embedded", func(t *testing.T) {
		r, _ := newR(t)
		s := newSite(t)
		for _, name := range []string{"a.mp4", "a.pdf", "a.mp3", "a.txt", "noext", "a.png.exe"} {
			writeFile(t, filepath.Join(s.docDir, name), s.png)
		}
		if err := os.Mkdir(filepath.Join(s.docDir, "dir.png"), 0o700); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(s.docDir, "empty.png"), nil)
		md := "![](a.mp4) ![](a.pdf) ![](a.mp3) ![](a.txt) ![](noext) ![](a.png.exe) ![](dir.png) ![](empty.png)\n"
		html, err := r.Render(context.Background(), []byte(md), s.docDir)
		if err != nil {
			t.Fatal(err)
		}
		requireSafe(t, html)
		if strings.Contains(string(html), "data:") || strings.Count(string(html), `class="missing-image"`) != 8 {
			t.Fatalf("something was embedded or left:\n%s", html)
		}
	})

	// An SVG can carry scripts and outside addresses. Pandoc puts it into an
	// <img> as a data: URI, and a browser runs nothing and loads nothing for an
	// image in that form. Were a future pandoc to paste the <svg> element into the
	// page instead, this fails.
	t.Run("a hostile svg is an img with a data URI, never an svg element", func(t *testing.T) {
		r, _ := newR(t)
		s := newSite(t)
		evil := `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" onload="alert(1)">` +
			`<script>alert(2)</script>` +
			`<a xlink:href="https://127.0.0.1:9/x"><text>t</text></a>` +
			`<image href="https://127.0.0.1:9/i.png"/>` +
			`<foreignObject><iframe src="https://127.0.0.1:9/f"></iframe></foreignObject></svg>`
		writeFile(t, filepath.Join(s.docDir, "evil.svg"), []byte(evil))
		html, err := r.Render(context.Background(), []byte("![x](evil.svg)\n"), s.docDir)
		if err != nil {
			t.Fatal(err)
		}
		low := strings.ToLower(string(html))
		for _, tag := range []string{"<svg", "<script", "<foreignobject", "<iframe", "<image", "<a "} {
			if strings.Contains(low, tag) {
				t.Errorf("the page has a %s tag:\n%s", tag, html)
			}
		}
		if !strings.Contains(string(html), `<img src="data:image/svg+xml;base64,`) {
			t.Fatalf("the picture is not an <img> with a data URI:\n%s", html)
		}
		if got := embedded(t, html); len(got) != 1 || string(got[0]) != evil {
			t.Fatalf("the embedded bytes are not the file")
		}
	})

	t.Run("a missing picture does not break the conversion", func(t *testing.T) {
		r, _ := newR(t)
		s := newSite(t)
		html, err := r.Render(context.Background(), []byte("before ![opis](nema.png) after\n\n![druga](a.png)\n"), s.docDir)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		requireSafe(t, html) // among other things: no src="nema.png" is left
		if !strings.Contains(string(html), `<span class="missing-image">opis</span>`) || len(embedded(t, html)) != 1 {
			t.Fatalf("unexpected output:\n%s", html)
		}
	})

	t.Run("a picture alone in a paragraph that is missing shows its description once", func(t *testing.T) {
		r, _ := newR(t)
		html, err := r.Render(context.Background(), []byte("![opis](https://example.com/x.png)\n"), t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(html), "opis") != 1 || strings.Contains(string(html), "<figure") {
			t.Fatalf("unexpected output:\n%s", html)
		}
	})

	t.Run("no folder: nothing is embedded", func(t *testing.T) {
		r, opts := newR(t)
		s := newSite(t)
		// pandoc looks in its working folder as well as in --resource-path, so a
		// picture there must not get in. The filter does not let it.
		writeFile(t, filepath.Join(opts.Dir, "rel.png"), s.png)
		writeFile(t, filepath.Join(opts.Dir, "a.png"), s.png)
		html, err := r.Render(context.Background(), []byte("![r](rel.png) ![a](a.png)\n"), "")
		if err != nil {
			t.Fatal(err)
		}
		requireSafe(t, html)
		if strings.Contains(string(html), "data:") || strings.Count(string(html), `class="missing-image"`) != 2 {
			t.Fatalf("a picture was embedded:\n%s", html)
		}
	})

	t.Run("a picture of the working folder does not get in when the document has a folder", func(t *testing.T) {
		r, opts := newR(t)
		s := newSite(t)
		writeFile(t, filepath.Join(opts.Dir, "rel.png"), s.png)
		html, err := r.Render(context.Background(), []byte("![r](rel.png)\n"), s.docDir)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(html), "data:") || strings.Contains(string(html), `src="rel.png"`) {
			t.Fatalf("the working folder was searched:\n%s", html)
		}
	})

	t.Run("an address of the document's own is not enough", func(t *testing.T) {
		r, _ := newR(t)
		s := newSite(t)
		// docDir/../tajna.png exists; every spelling of "go up" must fail.
		for _, p := range []string{"../tajna.png", "./../tajna.png", "a/../../tajna.png", "%2e%2e/tajna.png", "%2E%2E/tajna.png", "..%2Ftajna.png", ".%2e/tajna.png", "..\\tajna.png", "%2e%2e%5ctajna.png"} {
			html, err := r.Render(context.Background(), []byte("![x]("+p+")\n"), s.docDir)
			if err != nil {
				t.Fatalf("%s: %v", p, err)
			}
			if strings.Contains(string(html), "data:") || strings.Contains(string(html), "tajna") {
				t.Errorf("%s: got out of the folder:\n%s", p, html)
			}
		}
	})

	t.Run("a folder with a colon gets no pictures", func(t *testing.T) {
		r, _ := newR(t)
		s := newSite(t)
		// pandoc would split the folder at the colon and search "<outer>/Notes",
		// a different folder; a picture there must not be reachable.
		sibling := filepath.Join(s.outer, "Notes")
		writeFile(t, filepath.Join(sibling, "x.png"), s.png)
		dir := filepath.Join(s.outer, "Notes: 2024")
		writeFile(t, filepath.Join(dir, "own.png"), s.png)
		html, err := r.Render(context.Background(), []byte("![a](x.png) ![b](own.png)\n"), dir)
		if err != nil {
			t.Fatal(err)
		}
		requireSafe(t, html)
		if strings.Contains(string(html), "data:") {
			t.Fatalf("a picture was embedded:\n%s", html)
		}
	})

	// Not a wish, a measurement: pandoc and the filter both open the file the
	// way the system does, so a symbolic link in the folder is followed even when
	// it points outside it. The folder is the user's own, and so is the link, but
	// a folder that came from somewhere else (an unpacked archive) may hold one.
	// There is no way to see this from Lua or from the Markdown; it is a known
	// limit of the package, recorded here so that a change of behaviour shows.
	t.Run("a symbolic link out of the folder is followed (known limit)", func(t *testing.T) {
		r, _ := newR(t)
		s := newSite(t)
		if err := os.Symlink(s.secret, filepath.Join(s.docDir, "link.png")); err != nil {
			t.Fatal(err)
		}
		html, err := r.Render(context.Background(), []byte("![x](link.png)\n"), s.docDir)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(embedded(t, html)); n != 1 {
			t.Fatalf("expected the link to be followed (embedded 1), got %d:\n%s", n, html)
		}
	})
}

func TestRenderRelativeResourceDir(t *testing.T) {
	r, _ := newRenderer(t, nil)
	for _, dir := range []string{"relative", "./x", ".."} {
		if _, err := r.Render(context.Background(), []byte("x"), dir); err == nil {
			t.Errorf("resourceDir %q: want an error", dir)
		}
	}
}

// --- Render: warnings, failures, limits -------------------------------------------

func TestRenderWarningIsNotAnError(t *testing.T) {
	r := realRenderer(t)
	// pandoc prints "Could not convert TeX math" and still exits with 0.
	html, err := r.Render(context.Background(), []byte("$\\nosuchcommand{x}$\n"), "")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(html), "nosuchcommand") {
		t.Fatalf("the formula is gone:\n%s", html)
	}
}

// recorder is a Runner that notes what it was asked and answers with fixed output.
type recorder struct {
	mu     sync.Mutex
	calls  []recordedCall
	stdout []byte
	stderr []byte
	err    error
}

type recordedCall struct {
	dir, name string
	stdin     []byte
	maxStdout int
	args      []string
}

func (r *recorder) run(_ context.Context, dir, name string, stdin []byte, maxStdout int, args ...string) ([]byte, []byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedCall{dir, name, bytes.Clone(stdin), maxStdout, append([]string(nil), args...)})
	return r.stdout, r.stderr, r.err
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func fakeRenderer(t *testing.T, rec *recorder, mod func(*Options)) (*Renderer, Options) {
	t.Helper()
	return newRenderer(t, func(o *Options) {
		o.PandocBin = "/opt/test/pandoc"
		o.Run = rec.run
		if mod != nil {
			mod(o)
		}
	})
}

func TestRenderCommand(t *testing.T) {
	const reader = "markdown-smart-raw_tex-raw_attribute"
	base := func(filter string) []string {
		return []string{
			"-f", reader, "-t", "html5",
			"--syntax-highlighting=none", "--math-method=mathml", "--wrap=none",
			"--embed-resources", "--lua-filter=" + filter,
		}
	}
	res := filepath.Join(t.TempDir(), "Moj folder")
	tests := []struct {
		name string
		dir  string
		want func(filter string) []string
	}{
		{"with a folder", res, func(f string) []string {
			return append(base(f), "--metadata=pisar-images=on", "--metadata=pisar-resource-dir="+res, "--resource-path="+res)
		}},
		{"unclean folder is cleaned", res + "/x/..", func(f string) []string {
			return append(base(f), "--metadata=pisar-images=on", "--metadata=pisar-resource-dir="+res, "--resource-path="+res)
		}},
		{"without a folder: no resource path", "", func(f string) []string {
			return append(base(f), "--metadata=pisar-images=off")
		}},
		{"folder with a colon: no pictures", "/a/Notes: 2024", func(f string) []string {
			return append(base(f), "--metadata=pisar-images=off")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{stdout: []byte("<p>x</p>")}
			r, opts := fakeRenderer(t, rec, func(o *Options) { o.MaxHTML = 1234 })
			md := []byte("# čćž $x$ -dash\n")
			got, err := r.Render(context.Background(), md, tc.dir)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "<p>x</p>" {
				t.Fatalf("output %q is not what the runner returned, untouched", got)
			}
			if rec.count() != 1 {
				t.Fatalf("%d runs, want 1", rec.count())
			}
			c := rec.calls[0]
			filter := filepath.Join(opts.Dir, filterName)
			if c.name != "/opt/test/pandoc" || c.dir != opts.Dir || !bytes.Equal(c.stdin, md) || c.maxStdout != 1234 {
				t.Errorf("call = %+v", c)
			}
			want := tc.want(filter)
			if fmt.Sprint(c.args) != fmt.Sprint(want) {
				t.Errorf("args:\n got  %q\n want %q", c.args, want)
			}
		})
	}
}

func TestRenderSourceLimit(t *testing.T) {
	rec := &recorder{stdout: []byte("ok")}
	r, _ := fakeRenderer(t, rec, func(o *Options) { o.MaxSource = 10 })
	tests := []struct {
		name string
		size int
		ok   bool
	}{
		{"empty", 0, true},
		{"at the limit", 10, true},
		{"one over", 11, false},
		{"far over", 1 << 20, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := rec.count()
			_, err := r.Render(context.Background(), bytes.Repeat([]byte("a"), tc.size), "")
			switch {
			case tc.ok && err != nil:
				t.Fatalf("Render: %v", err)
			case !tc.ok && !errors.Is(err, ErrTooLarge):
				t.Fatalf("err = %v, want ErrTooLarge", err)
			}
			if ran := rec.count() - before; ran != map[bool]int{true: 1, false: 0}[tc.ok] {
				t.Fatalf("pandoc ran %d time(s)", ran)
			}
		})
	}
}

func TestRenderHTMLLimitWithFakeRunner(t *testing.T) {
	tests := []struct {
		name string
		size int
		ok   bool
	}{
		{"at the limit", 100, true},
		{"one over", 101, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// This runner does not keep the bound it was given; Render must.
			rec := &recorder{stdout: bytes.Repeat([]byte("a"), tc.size)}
			r, _ := fakeRenderer(t, rec, func(o *Options) { o.MaxHTML = 100 })
			_, err := r.Render(context.Background(), []byte("x"), "")
			if tc.ok != (err == nil) || (!tc.ok && !errors.Is(err, ErrTooLarge)) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	t.Run("a runner that reports it", func(t *testing.T) {
		rec := &recorder{err: ErrTooLarge}
		r, _ := fakeRenderer(t, rec, nil)
		if _, err := r.Render(context.Background(), []byte("x"), ""); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestRenderHTMLLimitWithPandoc(t *testing.T) {
	bin := requirePandoc(t)
	r, _ := newRenderer(t, func(o *Options) { o.PandocBin = bin; o.MaxHTML = 50 })
	_, err := r.Render(context.Background(), []byte("# A heading that is long enough for the page to pass fifty bytes\n"), "")
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

// script writes an executable shell script that stands in for pandoc.
func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-pandoc")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestRenderStopsAtTheLimit: a pandoc that never stops writing is killed as
// soon as the limit is crossed. A runner that collected everything first would
// run until the timeout, and the process would still be there.
func TestRenderStopsAtTheLimit(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	bin := script(t, "echo $$ > '"+pidFile+"'\nexec yes aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	r, _ := newRenderer(t, func(o *Options) { o.PandocBin = bin; o.MaxHTML = 1024; o.Timeout = 20 * time.Second })

	start := time.Now()
	_, err := r.Render(context.Background(), []byte("x"), "")
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("took %v: the output was read to the timeout", d)
	}
	raw, rerr := os.ReadFile(pidFile)
	if rerr != nil {
		t.Fatal(rerr)
	}
	var pid int
	if _, err := fmt.Sscanf(string(raw), "%d", &pid); err != nil {
		t.Fatalf("pid file %q: %v", raw, err)
	}
	// Render waited for the process, so it is gone, not just asked to stop.
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("process %d is still there: %v", pid, err)
	}
}

func TestRenderTimeout(t *testing.T) {
	t.Run("real process", func(t *testing.T) {
		bin := script(t, "exec sleep 30")
		r, _ := newRenderer(t, func(o *Options) { o.PandocBin = bin; o.Timeout = 200 * time.Millisecond })
		start := time.Now()
		_, err := r.Render(context.Background(), []byte("x"), "")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want DeadlineExceeded", err)
		}
		var te *ToolError
		if errors.As(err, &te) {
			t.Fatalf("a timeout is not the tool's failure: %v", err)
		}
		if d := time.Since(start); d > 10*time.Second {
			t.Fatalf("took %v", d)
		}
	})
	t.Run("a child that keeps the pipes open", func(t *testing.T) {
		// sh is killed, sleep is not, and it holds stdout. WaitDelay ends the wait.
		bin := script(t, "sleep 3\n")
		r, _ := newRenderer(t, func(o *Options) { o.PandocBin = bin; o.Timeout = 200 * time.Millisecond })
		start := time.Now()
		_, err := r.Render(context.Background(), []byte("x"), "")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want DeadlineExceeded", err)
		}
		if d := time.Since(start); d > 10*time.Second {
			t.Fatalf("took %v", d)
		}
	})
	t.Run("the deadline reaches the runner", func(t *testing.T) {
		var deadline time.Time
		var ok bool
		r, _ := newRenderer(t, func(o *Options) {
			o.Timeout = time.Minute
			o.Run = func(ctx context.Context, _, _ string, _ []byte, _ int, _ ...string) ([]byte, []byte, error) {
				deadline, ok = ctx.Deadline()
				return nil, nil, nil
			}
		})
		if _, err := r.Render(context.Background(), []byte("x"), ""); err != nil {
			t.Fatal(err)
		}
		if !ok || time.Until(deadline) > time.Minute || time.Until(deadline) < 30*time.Second {
			t.Fatalf("deadline %v (set %v)", deadline, ok)
		}
	})
}

func TestRenderCancel(t *testing.T) {
	t.Run("already cancelled: nothing runs", func(t *testing.T) {
		rec := &recorder{}
		r, _ := fakeRenderer(t, rec, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := r.Render(ctx, []byte("x"), ""); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want Canceled", err)
		}
		if rec.count() != 0 {
			t.Fatal("pandoc ran")
		}
	})
	t.Run("cancelled while running", func(t *testing.T) {
		started := make(chan struct{})
		r, _ := newRenderer(t, func(o *Options) {
			o.Run = func(ctx context.Context, _, _ string, _ []byte, _ int, _ ...string) ([]byte, []byte, error) {
				close(started)
				<-ctx.Done()
				return nil, nil, ctx.Err()
			}
		})
		ctx, cancel := context.WithCancel(context.Background())
		errc := make(chan error, 1)
		go func() {
			_, err := r.Render(ctx, []byte("x"), "")
			errc <- err
		}()
		<-started
		cancel()
		select {
		case err := <-errc:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want Canceled", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Render did not return after the cancel")
		}
	})
	t.Run("real process", func(t *testing.T) {
		bin := script(t, "exec sleep 30")
		r, _ := newRenderer(t, func(o *Options) { o.PandocBin = bin })
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_, err := r.Render(ctx, []byte("x"), "")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestRenderToolFailure(t *testing.T) {
	t.Run("exit code", func(t *testing.T) {
		bin := script(t, "echo 'bad things happened' >&2\nexit 3")
		r, _ := newRenderer(t, func(o *Options) { o.PandocBin = bin })
		_, err := r.Render(context.Background(), []byte("x"), "")
		var te *ToolError
		if !errors.As(err, &te) {
			t.Fatalf("err = %v, want *ToolError", err)
		}
		var ee *exec.ExitError
		if te.Tool != "pandoc" || te.Output != "bad things happened" || !errors.As(err, &ee) || ee.ExitCode() != 3 {
			t.Fatalf("ToolError = %+v (%v)", te, err)
		}
		if !strings.Contains(err.Error(), "bad things happened") {
			t.Fatalf("message %q lacks the tool's output", err)
		}
	})
	t.Run("long stderr is bounded", func(t *testing.T) {
		bin := script(t, "head -c 100000 /dev/zero | tr '\\0' 'x' >&2\nexit 1")
		r, _ := newRenderer(t, func(o *Options) { o.PandocBin = bin })
		_, err := r.Render(context.Background(), []byte("x"), "")
		var te *ToolError
		if !errors.As(err, &te) || len(te.Output) > maxToolOutput+len("…") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a failing runner is a ToolError", func(t *testing.T) {
		boom := errors.New("boom")
		rec := &recorder{err: boom, stderr: []byte("  complaint \n")}
		r, _ := fakeRenderer(t, rec, nil)
		_, err := r.Render(context.Background(), []byte("x"), "")
		var te *ToolError
		if !errors.As(err, &te) || !errors.Is(err, boom) || te.Output != "complaint" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("warnings with exit 0 are not an error", func(t *testing.T) {
		rec := &recorder{stdout: []byte("<p>x</p>"), stderr: []byte("[WARNING] Could not fetch resource secret-name.png\n")}
		r, _ := fakeRenderer(t, rec, nil)
		got, err := r.Render(context.Background(), []byte("x"), "")
		if err != nil || string(got) != "<p>x</p>" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
}

func TestRenderToolMissing(t *testing.T) {
	for _, tc := range []struct{ name, bin string }{
		{"absolute path", filepath.Join(t.TempDir(), "no-such-pandoc")},
		{"name not in PATH", "pisar-no-such-pandoc-4f7a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newRenderer(t, func(o *Options) { o.PandocBin = tc.bin })
			_, err := r.Render(context.Background(), []byte("x"), "")
			if !errors.Is(err, ErrToolMissing) {
				t.Fatalf("err = %v, want ErrToolMissing", err)
			}
			var te *ToolError
			if errors.As(err, &te) {
				t.Fatalf("a missing tool is not a ToolError: %v", err)
			}
		})
	}
}

// --- the filter file -------------------------------------------------------------

func TestFilterFile(t *testing.T) {
	filterPath := func(o Options) string { return filepath.Join(o.Dir, filterName) }
	stat := func(t *testing.T, p string) fs.FileInfo {
		t.Helper()
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		return info
	}
	render := func(t *testing.T, r *Renderer) {
		t.Helper()
		if _, err := r.Render(context.Background(), []byte("x"), ""); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("New touches nothing", func(t *testing.T) {
		rec := &recorder{}
		_, opts := fakeRenderer(t, rec, nil)
		if _, err := os.Lstat(opts.Dir); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("folder exists after New: %v", err)
		}
	})

	t.Run("created with the embedded text and private modes", func(t *testing.T) {
		rec := &recorder{}
		r, opts := fakeRenderer(t, rec, nil)
		render(t, r)
		got, err := os.ReadFile(filterPath(opts))
		if err != nil || !bytes.Equal(got, filterLua) || len(filterLua) == 0 {
			t.Fatalf("filter file: %v", err)
		}
		if m := stat(t, filterPath(opts)).Mode().Perm(); m != 0o600 {
			t.Errorf("file mode %v, want 0600", m)
		}
		if m := stat(t, opts.Dir).Mode().Perm(); m != 0o700 {
			t.Errorf("folder mode %v, want 0700", m)
		}
		entries, err := os.ReadDir(opts.Dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("folder holds %v (%v), want only the filter", entries, err)
		}
	})

	t.Run("written once", func(t *testing.T) {
		rec := &recorder{}
		r, opts := fakeRenderer(t, rec, nil)
		render(t, r)
		first := stat(t, filterPath(opts))
		time.Sleep(20 * time.Millisecond)
		for range 3 {
			render(t, r)
		}
		second := stat(t, filterPath(opts))
		if !os.SameFile(first, second) || !first.ModTime().Equal(second.ModTime()) {
			t.Fatal("the filter was written again")
		}
	})

	t.Run("a second Renderer reuses the file", func(t *testing.T) {
		rec := &recorder{}
		r, opts := fakeRenderer(t, rec, nil)
		render(t, r)
		first := stat(t, filterPath(opts))
		r2, err := New(opts)
		if err != nil {
			t.Fatal(err)
		}
		render(t, r2)
		if !os.SameFile(first, stat(t, filterPath(opts))) {
			t.Fatal("the filter was replaced")
		}
	})

	t.Run("written again when it is gone", func(t *testing.T) {
		rec := &recorder{}
		r, opts := fakeRenderer(t, rec, nil)
		render(t, r)
		if err := os.Remove(filterPath(opts)); err != nil {
			t.Fatal(err)
		}
		render(t, r)
		if got, err := os.ReadFile(filterPath(opts)); err != nil || !bytes.Equal(got, filterLua) {
			t.Fatalf("filter file: %v", err)
		}
	})

	t.Run("written again when the folder is gone", func(t *testing.T) {
		rec := &recorder{}
		r, opts := fakeRenderer(t, rec, nil)
		render(t, r)
		if err := os.RemoveAll(opts.Dir); err != nil {
			t.Fatal(err)
		}
		render(t, r)
		if got, err := os.ReadFile(filterPath(opts)); err != nil || !bytes.Equal(got, filterLua) {
			t.Fatalf("filter file: %v", err)
		}
	})

	t.Run("restored when it was changed", func(t *testing.T) {
		rec := &recorder{}
		r, opts := fakeRenderer(t, rec, nil)
		render(t, r)
		for _, changed := range [][]byte{nil, []byte("function Link() end"), append(bytes.Clone(filterLua[:len(filterLua)-1]), 'X')} {
			writeFile(t, filterPath(opts), changed)
			render(t, r)
			if got, err := os.ReadFile(filterPath(opts)); err != nil || !bytes.Equal(got, filterLua) {
				t.Fatalf("filter file after a change to %q: %v", changed, err)
			}
		}
	})

	t.Run("a link in its place is replaced, never written through", func(t *testing.T) {
		rec := &recorder{}
		r, opts := fakeRenderer(t, rec, nil)
		target := filepath.Join(t.TempDir(), "target.lua")
		writeFile(t, target, []byte("untouched"))
		if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filterPath(opts)); err != nil {
			t.Fatal(err)
		}
		render(t, r)
		if !stat(t, filterPath(opts)).Mode().IsRegular() {
			t.Fatal("the link is still there")
		}
		if got, err := os.ReadFile(target); err != nil || string(got) != "untouched" {
			t.Fatalf("the link's target was written: %q, %v", got, err)
		}
	})

	t.Run("fails when the folder cannot be made", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "file")
		writeFile(t, blocker, []byte("x"))
		r, err := New(Options{PandocBin: "pandoc", Dir: filepath.Join(blocker, "sub"), Timeout: time.Second, MaxSource: 1, MaxHTML: 1, Run: (&recorder{}).run})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Render(context.Background(), []byte("x"), ""); err == nil {
			t.Fatal("want an error")
		}
	})

	t.Run("concurrent renders", func(t *testing.T) {
		rec := &recorder{}
		r, opts := fakeRenderer(t, rec, nil)
		var wg sync.WaitGroup
		for range 16 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := r.Render(context.Background(), []byte("x"), ""); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		if got, err := os.ReadFile(filterPath(opts)); err != nil || !bytes.Equal(got, filterLua) {
			t.Fatalf("filter file: %v", err)
		}
		if entries, err := os.ReadDir(opts.Dir); err != nil || len(entries) != 1 {
			t.Fatalf("folder holds %v (%v), want only the filter", entries, err)
		}
	})
}

func TestNew(t *testing.T) {
	ok := Options{PandocBin: "pandoc", Dir: t.TempDir(), Timeout: time.Second, MaxSource: 1, MaxHTML: 1}
	tests := []struct {
		name string
		mod  func(*Options)
		fail bool
	}{
		{"complete", func(*Options) {}, false},
		{"no pandoc", func(o *Options) { o.PandocBin = "" }, true},
		{"no folder", func(o *Options) { o.Dir = "" }, true},
		{"relative folder", func(o *Options) { o.Dir = "preview" }, true},
		{"zero timeout", func(o *Options) { o.Timeout = 0 }, true},
		{"negative timeout", func(o *Options) { o.Timeout = -time.Second }, true},
		{"zero source limit", func(o *Options) { o.MaxSource = 0 }, true},
		{"negative source limit", func(o *Options) { o.MaxSource = -1 }, true},
		{"zero page limit", func(o *Options) { o.MaxHTML = 0 }, true},
		{"negative page limit", func(o *Options) { o.MaxHTML = -1 }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := ok
			tc.mod(&opts)
			r, err := New(opts)
			if tc.fail != (err != nil) || (tc.fail && r != nil) || (!tc.fail && r == nil) {
				t.Fatalf("New = %v, %v", r, err)
			}
		})
	}
}

func TestToolErrorMessage(t *testing.T) {
	err := &ToolError{Tool: "pandoc", Output: "oops", Err: errors.New("exit status 1")}
	if got := err.Error(); got != "preview: pandoc failed: exit status 1: oops" {
		t.Errorf("message %q", got)
	}
	err.Output = ""
	if got := err.Error(); got != "preview: pandoc failed: exit status 1" {
		t.Errorf("message %q", got)
	}
}

func TestBoundOutput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"trimmed", "  a\n", "a"},
		{"invalid UTF-8", "a\xffb", "a�b"},
		{"long", strings.Repeat("a", maxToolOutput+10), strings.Repeat("a", maxToolOutput) + "…"},
		{"cut inside a rune", strings.Repeat("a", maxToolOutput-1) + "čč", strings.Repeat("a", maxToolOutput-1) + "…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := boundOutput([]byte(tc.in)); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// --- fuzzing ---------------------------------------------------------------------

// FuzzRender: whatever the text, the page has nothing in it that runs, loads or
// shows a file from outside the folder. Run it longer with
//
//	go test -run '^$' -fuzz FuzzRender -fuzztime 20s ./internal/preview
func FuzzRender(f *testing.F) {
	bin := requirePandoc(f)
	r, _ := newRenderer(f, func(o *Options) { o.PandocBin = bin })
	s := newSite(f)
	for _, in := range hostileInputs("http://127.0.0.1:9", s) {
		f.Add(in.md)
	}
	for _, md := range []string{
		"# Naslov\n\ntekst s **podebljanim** i `kodom`\n",
		"| a | b |\n|---|---|\n| 1 | 2 |\n",
		"[^1]: x\n\ntext[^1]\n",
		"![x](a.png)\n",
		"[x]{.a onclick=1}\n![](a.png){onerror=1}\n",
	} {
		f.Add(md)
	}
	f.Fuzz(func(t *testing.T, md string) {
		html, err := r.Render(context.Background(), []byte(md), s.docDir)
		var te *ToolError
		if errors.As(err, &te) || errors.Is(err, ErrTooLarge) {
			t.Skip() // no page, so nothing to check
		}
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		requireSafe(t, html)
		s.requireNoLeak(t, html)
	})
}
