package export

import (
	"flag"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/folio/internal/md"
	"github.com/ZahakJ/folio/internal/render"
)

var writeHTML = flag.String("html", "", "write the export of examples/vault/Typography.md to this file")

type resolver map[string]string

func (r resolver) Resolve(q render.LinkQuery) (string, bool) {
	t := strings.TrimSuffix(q.Target, ".md")
	for p := range r {
		if strings.EqualFold(strings.TrimSuffix(p, ".md"), t) || strings.EqualFold(strings.TrimSuffix(p[strings.LastIndex(p, "/")+1:], ".md"), t) {
			return p, true
		}
	}
	if isImage(t) && !strings.Contains(t, "missing") {
		return "attachments/" + t[strings.LastIndex(t, "/")+1:], true
	}
	return "", false
}

func (r resolver) Embed(p string) (string, string, bool) {
	s, ok := r[p]
	return strings.TrimSuffix(p[strings.LastIndex(p, "/")+1:], ".md"), s, ok
}

var vault = resolver{
	"Astrolabe.md":      "# Astrolabe\n\nBrass and stars.\n\n## The rete\n\nPointers.\n",
	"Reading list.md":   "# Reading list\n",
	"sub/Deep Note.md":  "# Deep Note\n",
	"Target.md":         "# Target\n\nbody\n",
	"sub/inner/Leaf.md": "leaf",
}

func export(src string, opt Options) string {
	if opt.Today.IsZero() {
		opt.Today = time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	}
	return HTML(md.Parse(src), opt)
}

func TestDocumentShape(t *testing.T) {
	out := export("# Hello\n\nworld\n", Options{})
	for _, want := range []string{"<!doctype html>", "<meta charset=\"utf-8\">", "<title>Hello</title>",
		"prefers-color-scheme: dark", "@media print", "<h1 class=\"title\" dir=\"auto\">Hello</h1>",
		"<p dir=\"auto\">world</p>", "Content-Security-Policy"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Count(out, ">Hello</h1>") != 1 {
		t.Error("leading H1 printed twice")
	}
}

// TestSelfContained checks nothing is loaded: no scripts, no external
// stylesheets, fonts or images, even when the note asks for them.
func TestSelfContained(t *testing.T) {
	b, err := os.ReadFile("../../examples/vault/Typography.md")
	if err != nil {
		t.Skip(err)
	}
	src := string(b) + "\n![remote](https://example.org/x.png)\n\n<script src=\"https://evil.example/x.js\"></script>\n\n<link rel=stylesheet href=https://evil.example/s.css>\n"
	out := export(src, Options{Resolver: vault})
	low := strings.ToLower(out)
	for _, bad := range []string{"<script", "<link", "@import", "url(", "src=\"http", "src=\"//", "<iframe", "<object", "<embed"} {
		if strings.Contains(low, bad) {
			t.Errorf("output contains %q", bad)
		}
	}
	if !strings.Contains(out, `href="https://example.org/x.png"`) {
		t.Error("external image should become a link")
	}
}

// TestInjection feeds hostile note content through every construct and
// checks no markup escapes from its context.
func TestInjection(t *testing.T) {
	payload := `<img src=x onerror=alert(1)>"'&`
	src := strings.Join([]string{
		"---",
		"title: \"" + payload + "\"",
		"tags: [\"" + payload + "\"]",
		"evil: " + payload,
		"---",
		"",
		"Text " + payload + " and *" + payload + "*",
		"",
		"`" + payload + "` $" + payload + "$",
		"",
		"[[" + payload + "]] [[Target|" + payload + "]]",
		"",
		"[x](javascript:alert(1)) [y](JAVASCRIPT:alert(1)) [z](data:text/html,<b>) [ok](https://example.org/\"onmouseover=\"alert(1))",
		"",
		"![a\" onerror=\"alert(1)](pic.png)",
		"",
		"<div onclick=\"alert(1)\">raw block</div>",
		"",
		"inline <span onclick=x>raw</span> <kbd>K</kbd> <b class=x>b</b>",
		"",
		"> [!note] " + payload,
		"> body",
		"",
		"```" + "go\"><script>",
		payload,
		"```",
		"",
		"| " + payload + " | b |",
		"|---|---|",
		"| c | d |",
		"",
		"- [ ] " + payload + " due:2026-01-01",
		"",
		"#tag<script>",
		"",
		"note[^1]",
		"",
		"[^1]: " + payload,
	}, "\n")
	out := export(src, Options{Resolver: vault})
	body := out[strings.Index(out, "<body>"):]
	if strings.Contains(body, "<img src=x") || strings.Contains(body, " onerror=\"") {
		t.Errorf("unescaped payload in output")
	}
	for _, bad := range []string{"<script", "javascript:", "JAVASCRIPT:", "data:text", "<div onclick", "<span onclick", "<b class", "\"onmouseover"} {
		if strings.Contains(body, bad) {
			t.Errorf("output contains %q", bad)
		}
	}
	// Attribute values must not contain raw quotes or angle brackets.
	attr := regexp.MustCompile(`="([^"]*)"`)
	for _, m := range attr.FindAllStringSubmatch(body, -1) {
		if strings.ContainsAny(m[1], "<>") {
			t.Errorf("attribute value with markup: %q", m[1])
		}
	}
	if !strings.Contains(body, "<kbd>K</kbd>") {
		t.Error("whitelisted inline tag dropped")
	}
	if !strings.Contains(body, "&lt;div onclick=&#34;alert(1)&#34;&gt;raw block&lt;/div&gt;") {
		t.Error("raw HTML block should be shown escaped")
	}
	// Control characters never reach the page.
	if out := export("bell\x07 esc\x1b[31m nul\x00\n", Options{}); strings.ContainsAny(out, "\x07\x1b\x00") {
		t.Error("control characters in output")
	}
}

func TestLinks(t *testing.T) {
	src := "[[Astrolabe]] [[Astrolabe#The rete|rete]] [[Deep Note]] [[Ghost]] [[#Section]] [[#Nope]] " +
		"[list](Reading%20list.md) [[Astrolabe#^blk]] ![[star.png]]\n\n## Section\n"
	out := export(src, Options{Path: "sub/inner/Leaf.md", Resolver: vault})
	for _, want := range []string{
		`href="../../Astrolabe.html"`,
		`href="../../Astrolabe.html#the-rete">rete</a>`,
		`href="../Deep%20Note.html"`,
		`<span class="link broken" title="no such note">Ghost</span>`,
		`href="#section"`,
		`href="../../Reading%20list.html"`,
		`href="../../Astrolabe.html#%5Eblk"`,
		`<img src="../../attachments/star.png"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(out, `href="#nope"`) {
		t.Error("broken same-note anchor became a link")
	}
	// Without a resolver links point next to the note.
	if out := export("[[Some Note]]\n", Options{}); !strings.Contains(out, `href="Some%20Note.html"`) {
		t.Error("unresolved export link")
	}
	ext := export("<https://example.org> [a](mailto:x@example.org)\n", Options{})
	if !strings.Contains(ext, `<a class="link external" href="https://example.org" rel="noopener noreferrer">https://example.org</a>`) ||
		!strings.Contains(ext, `href="mailto:x@example.org"`) {
		t.Errorf("external links:\n%s", ext[strings.Index(ext, "<body>"):])
	}
}

func TestBlocks(t *testing.T) {
	src := "> [!warning]- Careful\n> body\n\n" +
		"- [x] done\n- [ ] late due:2026-10-01\n- [/] doing 📅 2027-01-01\n\n" +
		"| a | b |\n|:-|-:|\n| 1 | 2 |\n\n" +
		"```go\nfunc f() {} // c\n```\n\n---\n\n$$\nx^2\n$$\n\n%% hidden %%\n\n<!-- hidden too -->\n\n==hi== ~~no~~ #tag\n\nfoot[^a]\n\n[^a]: the note\n\n![[Astrolabe#The rete]]\n"
	out := export(src, Options{Resolver: vault})
	for _, want := range []string{
		`<aside class="callout callout-warning" dir="ltr">`, `<details>`, `<summary class="callout-title">`,
		`<li class="task done"`, `☑`, `<time class="due overdue" datetime="2026-10-01">overdue 1 Oct 2026</time>`,
		`<time class="due" datetime="2027-01-01">due 1 Jan 2027</time>`,
		`<th dir="auto" class="l">a</th>`, `<td dir="auto" class="r">2</td>`,
		`<figcaption>go</figcaption>`, `<span class="tk-keyword">func</span>`, `<span class="tk-comment">// c</span>`,
		`class="ornament"`, `<div class="math display">x^2</div>`,
		`<mark>hi</mark>`, `<del>no</del>`, `<span class="tag"><span class="hash">#</span>tag</span>`,
		`<sup class="fnref"><a id="fnref-1" href="#fn-1">[1]</a></sup>`, `<li id="fn-1" value="1" dir="auto">the note`,
		`<blockquote class="embed"`, `Astrolabe › The rete`, `Pointers.`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, bad := range []string{"hidden too", "% hidden", "Brass and stars"} {
		if strings.Contains(out[strings.Index(out, "<body>"):], bad) {
			t.Errorf("output contains %q", bad)
		}
	}
}

func TestTitleBlock(t *testing.T) {
	src := "---\ntitle: My Title\ndate: 2026-03-14\ntags: [a, b]\nstatus: draft\n---\n\n# My Title\n\nbody\n"
	out := export(src, Options{Backlinks: 2})
	for _, want := range []string{`<title>My Title</title>`, `<time datetime="2026-03-14">14 March 2026</time>`,
		`<span class="hash">#</span>a</span>`, `2 backlinks`, `<summary>properties · 1</summary>`, `<dt>status</dt><dd dir="auto">draft</dd>`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Count(out, "My Title") != 2 { // <title> and the h1
		t.Errorf("title appears %d times", strings.Count(out, "My Title"))
	}
}

func TestDirection(t *testing.T) {
	out := export("- عنصر\n  - فرعي\n\n> اقتباس\n\n- English\n", Options{})
	for _, want := range []string{`<ul dir="rtl">`, `<blockquote dir="rtl">`, `<ul dir="ltr">`, `<li dir="auto">عنصر`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s", want)
		}
	}
}

func TestRelPath(t *testing.T) {
	cases := [][3]string{
		{".", "a.html", "a.html"},
		{"x", "x/a.html", "a.html"},
		{"x/y", "a.html", "../../a.html"},
		{"x/y", "x/z/a.html", "../z/a.html"},
		{"", "b/c.html", "b/c.html"},
	}
	for _, c := range cases {
		if got := relPath(c[0], c[1]); got != c[2] {
			t.Errorf("relPath(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

// TestWriteShowcase writes the showcase export for viewing in a browser.
func TestWriteShowcase(t *testing.T) {
	if *writeHTML == "" {
		t.Skip("use -html FILE")
	}
	b, err := os.ReadFile("../../examples/vault/Typography.md")
	if err != nil {
		t.Fatal(err)
	}
	out := export(string(b), Options{Path: "Typography.md", Resolver: vault, Backlinks: 3})
	if err := os.WriteFile(*writeHTML, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}
