// Package export turns a parsed note into one self-contained HTML document
// (DESIGN.md §8): inline CSS in the manuscript style of the reader (serif
// body, gold accents, hairline rules, the same callouts, tables, task boxes
// and lightly highlighted code), light and dark through
// prefers-color-scheme, print-friendly, dir="auto" on every text block.
//
// The output never loads anything: no scripts, no external stylesheets,
// fonts or images, and a Content-Security-Policy that forbids network
// fetches. All note text is escaped; raw HTML in the note is shown as
// text, except a handful of attribute-free inline formatting tags
// (<kbd>, <sub>, <sup>, …). Link destinations with schemes other than
// http, https and mailto are dropped. Wikilinks become relative links to
// the target's .html file, so a folder of exported notes browses as a site.
package export

import (
	"fmt"
	"html"
	"io"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/ZahakJ/folio/internal/md"
	"github.com/ZahakJ/folio/internal/render"
	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/theme"
)

// Options controls an export. All fields are optional.
type Options struct {
	// Path is the note's vault-relative path ("folder/Note.md"); links to
	// other notes are made relative to its folder.
	Path string
	// Title is the fallback title (normally the file name) for a note
	// without a frontmatter title or leading H1.
	Title string
	// Resolver resolves wikilinks and Markdown links to vault paths and
	// supplies the source of embedded notes. Without one, links point at
	// "<target>.html" next to the note and embeds become links.
	Resolver render.Resolver
	// Today judges overdue tasks (zero = now).
	Today time.Time
	// Backlinks, when positive, is shown in the title block.
	Backlinks int
	// Generator is written as <meta name="generator"> when non-empty.
	Generator string
}

// HTML returns the complete HTML document for doc.
func HTML(doc *md.Document, opt Options) string {
	var b strings.Builder
	_ = Write(&b, doc, opt)
	return b.String()
}

// Write writes the complete HTML document for doc to w.
func Write(w io.Writer, doc *md.Document, opt Options) error {
	if doc == nil {
		doc = md.Parse("")
	}
	e := &exporter{doc: doc, opt: opt, today: opt.Today}
	if e.today.IsZero() {
		e.today = time.Now()
	}
	e.page()
	_, err := io.WriteString(w, e.b.String())
	return err
}

type exporter struct {
	doc   *md.Document
	opt   Options
	today time.Time
	b     strings.Builder
	depth int // embed depth
}

func (e *exporter) w(s string)           { e.b.WriteString(s) }
func (e *exporter) esc(s string)         { e.b.WriteString(escape(s)) }
func (e *exporter) f(f string, a ...any) { fmt.Fprintf(&e.b, f, a...) }

// escape makes text safe in HTML text and quoted attributes; control
// characters other than tab and newline are dropped and invalid UTF-8 is
// replaced.
func escape(s string) string {
	s = strings.ToValidUTF8(s, "�")
	if strings.IndexFunc(s, func(r rune) bool { return r < 0x20 && r != '\t' && r != '\n' || r == 0x7f }) >= 0 {
		s = strings.Map(func(r rune) rune {
			if r < 0x20 && r != '\t' && r != '\n' || r == 0x7f {
				return -1
			}
			return r
		}, s)
	}
	return html.EscapeString(s)
}

// ---------------------------------------------------------------------------
// Page

func (e *exporter) page() {
	title, rest := e.title()
	e.w("<!doctype html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n")
	e.w("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	e.w("<meta name=\"color-scheme\" content=\"light dark\">\n")
	// Defence in depth: nothing may be fetched from the network.
	e.w("<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'; img-src 'self' data: file:\">\n")
	if e.opt.Generator != "" {
		e.w("<meta name=\"generator\" content=\"")
		e.esc(e.opt.Generator)
		e.w("\">\n")
	}
	e.w("<title>")
	e.esc(title)
	e.w("</title>\n<style>\n")
	e.w(stylesheet())
	e.w("</style>\n</head>\n<body>\n<article class=\"page\">\n")
	e.titleBlock(title)
	e.sections(rest)
	e.footnotes()
	e.w("</article>\n</body>\n</html>\n")
}

// title picks the title (frontmatter title, else a leading H1, else the
// fallback) and returns the blocks left to render.
func (e *exporter) title() (string, []md.Block) {
	blocks := e.doc.Blocks
	title := ""
	if fm := e.doc.Frontmatter; fm != nil {
		title = fm.Title
	}
	for i, b := range blocks {
		if skipped(b) {
			continue
		}
		if h, ok := b.(*md.Heading); ok && h.Level == 1 &&
			(title == "" || strings.EqualFold(strings.TrimSpace(h.Text), strings.TrimSpace(title))) {
			title = h.Text
			rest := append([]md.Block(nil), blocks[:i]...)
			blocks = append(rest, blocks[i+1:]...)
		}
		break
	}
	if title == "" {
		title = e.opt.Title
	}
	if title == "" {
		title = "Untitled"
	}
	return title, blocks
}

func skipped(b md.Block) bool {
	switch b.(type) {
	case *md.CommentBlock, *md.LinkRefDef, *md.FootnoteDef:
		return true
	}
	return false
}

var shownKeys = map[string]bool{"title": true, "tags": true, "tag": true, "date": true}

func (e *exporter) titleBlock(title string) {
	fm := e.doc.Frontmatter
	e.w("<header class=\"title-block\">\n<h1 class=\"title\" dir=\"auto\">")
	e.esc(title)
	e.w("</h1>\n")
	var chips []string
	if fm != nil {
		date := fm.Date
		if date == "" {
			date = fm.Created
		}
		if date != "" {
			d := strings.Trim(date, `"' `)
			shown := d
			if len(d) >= 10 {
				if t, err := time.Parse("2006-01-02", d[:10]); err == nil {
					shown = t.Format("2 January 2006")
				}
			}
			chips = append(chips, "<time datetime=\""+escape(d)+"\">"+escape(shown)+"</time>")
		}
		if len(fm.Tags) > 0 {
			var tags []string
			for _, t := range fm.Tags {
				tags = append(tags, tagHTML(t))
			}
			chips = append(chips, strings.Join(tags, " "))
		}
	}
	if e.opt.Backlinks > 0 {
		s := strconv.Itoa(e.opt.Backlinks) + " backlink"
		if e.opt.Backlinks != 1 {
			s += "s"
		}
		chips = append(chips, escape(s))
	}
	if len(chips) > 0 {
		e.w("<p class=\"chips\">")
		e.w(strings.Join(chips, " <span class=\"dot\">·</span> "))
		e.w("</p>\n")
	}
	e.w("</header>\n")
	if fm == nil {
		return
	}
	var fields []md.Field
	for _, f := range fm.Fields {
		k := strings.ToLower(f.Key)
		if shownKeys[k] || (k == "created" && fm.Date == "") {
			continue
		}
		fields = append(fields, f)
	}
	if len(fields) == 0 {
		return
	}
	e.f("<details class=\"properties\">\n<summary>properties · %d</summary>\n<dl>\n", len(fields))
	for _, f := range fields {
		val := f.Scalar()
		if (val == "" && len(f.Lines) > 0) || strings.HasPrefix(strings.TrimSpace(f.Value), "[") {
			val = strings.Join(f.List(), ", ")
		}
		e.w("<dt>")
		e.esc(f.Key)
		e.w("</dt><dd dir=\"auto\">")
		e.esc(val)
		e.w("</dd>\n")
	}
	e.w("</dl>\n</details>\n")
}

func tagHTML(name string) string {
	return "<span class=\"tag\"><span class=\"hash\">#</span>" + escape(name) + "</span>"
}

// ---------------------------------------------------------------------------
// Blocks

// sections renders top-level blocks; each heading opens a <section> so the
// page has a document outline.
func (e *exporter) sections(blocks []md.Block) {
	e.blocks(blocks, false)
}

func (e *exporter) blocks(bs []md.Block, tight bool) {
	for _, b := range bs {
		e.block(b, tight)
	}
}

// idAttr returns an id attribute for an Obsidian block id.
func idAttr(id string) string {
	if id == "" {
		return ""
	}
	return " id=\"" + escape("^"+id) + "\""
}

func (e *exporter) block(b md.Block, tight bool) {
	switch b := b.(type) {
	case *md.Paragraph:
		if e.embedBlock(b) {
			return
		}
		if tight {
			e.inlines(b.Inlines)
			e.w("\n")
			return
		}
		e.w("<p dir=\"auto\"" + idAttr(b.ID) + ">")
		e.inlines(b.Inlines)
		e.w("</p>\n")
	case *md.Heading:
		lvl := min(6, max(1, b.Level))
		e.f("<h%d id=\"%s\" dir=\"auto\">", lvl, escape(b.Slug))
		e.inlines(b.Inlines)
		e.f("</h%d>\n", lvl)
	case *md.ThematicBreak:
		e.w("<div class=\"ornament\" role=\"separator\"><span>·</span><span class=\"star\">✦</span><span>·</span></div>\n")
	case *md.BlockQuote:
		e.w("<blockquote dir=\"" + blocksDir(b.Children) + "\"" + idAttr(b.ID) + ">\n")
		e.blocks(b.Children, false)
		e.w("</blockquote>\n")
	case *md.Callout:
		e.callout(b)
	case *md.List:
		e.list(b)
	case *md.CodeBlock:
		e.code(b)
	case *md.MathBlock:
		e.w("<div class=\"math display\"" + idAttr(b.ID) + ">")
		e.esc(strings.Trim(strings.Join(b.Lines, "\n"), "\n"))
		e.w("</div>\n")
	case *md.HTMLBlock:
		// Raw HTML is shown, never interpreted. Comments stay hidden.
		var lines []string
		for _, l := range stripComments(b.Lines) {
			if l != nil {
				lines = append(lines, *l)
			}
		}
		if len(lines) == 0 {
			return
		}
		e.w("<pre class=\"html\">")
		e.esc(strings.Join(lines, "\n"))
		e.w("</pre>\n")
	case *md.Table:
		e.table(b)
	}
}

// textDir is "rtl" when s's first strong character is right-to-left.
func textDir(s string) string {
	if text.BaseDirection(s) == text.RTL {
		return "rtl"
	}
	return "ltr"
}

// blocksDir is the direction of the first text in a container's blocks.
// dir="auto" cannot be used on containers: it ignores descendants that
// carry their own dir attribute, which every text block does.
func blocksDir(bs []md.Block) string {
	d := ""
	md.WalkBlocks(bs, func(b md.Block) bool {
		if d != "" {
			return false
		}
		var s string
		switch b := b.(type) {
		case *md.Paragraph:
			s = md.PlainText(b.Inlines)
		case *md.Heading:
			s = b.Text
		case *md.CodeBlock, *md.Table, *md.MathBlock:
			d = "ltr"
			return false
		default:
			return true
		}
		switch text.BaseDirection(s) {
		case text.RTL:
			d = "rtl"
		case text.LTR:
			d = "ltr"
		}
		return d == ""
	})
	if d == "" {
		return "ltr"
	}
	return d
}

func stripComments(lines []string) []*string {
	out := make([]*string, len(lines))
	in := false
	for i, l := range lines {
		var b strings.Builder
		had := in
		rest := l
		for rest != "" {
			if in {
				j := strings.Index(rest, "-->")
				if j < 0 {
					rest = ""
					break
				}
				rest, in = rest[j+3:], false
				continue
			}
			j := strings.Index(rest, "<!--")
			if j < 0 {
				b.WriteString(rest)
				break
			}
			had = true
			b.WriteString(rest[:j])
			rest, in = rest[j+4:], true
		}
		s := b.String()
		if had && strings.TrimSpace(s) == "" {
			continue
		}
		out[i] = &s
	}
	return out
}

var calloutGlyphs = theme.UnicodeGlyphs

func (e *exporter) callout(c *md.Callout) {
	kind := theme.CalloutKind(c.Kind)
	open := ""
	tag := "div"
	if c.Fold != md.FoldNone {
		tag = "details"
		if c.Fold == md.FoldOpen {
			open = " open"
		}
	}
	dir := blocksDir(c.Children)
	if c.Title != nil {
		dir = textDir(md.PlainText(c.Title))
	}
	e.f("<aside class=\"callout callout-%s\" dir=\"%s\"%s>\n", kind, dir, idAttr(c.ID))
	if tag == "details" {
		e.w("<details" + open + ">\n<summary class=\"callout-title\">")
	} else {
		e.w("<p class=\"callout-title\">")
	}
	e.w("<span class=\"glyph\" aria-hidden=\"true\">")
	e.esc(calloutGlyphs.Callout(kind))
	e.w("</span> ")
	if c.Title != nil {
		e.inlines(c.Title)
	} else {
		k := c.Kind
		if k == "" {
			k = "note"
		}
		e.esc(strings.ToUpper(k[:1]) + k[1:])
	}
	if tag == "details" {
		e.w("</summary>\n")
	} else {
		e.w("</p>\n")
	}
	e.blocks(c.Children, false)
	if tag == "details" {
		e.w("</details>\n")
	}
	e.w("</aside>\n")
}

func (e *exporter) list(l *md.List) {
	tag := "ul"
	start := ""
	if l.Ordered {
		tag = "ol"
		if l.Start != 1 {
			start = " start=\"" + strconv.Itoa(l.Start) + "\""
		}
	}
	cls := ""
	for _, it := range l.Items {
		if it.Task != nil {
			cls = " class=\"tasks\""
			break
		}
	}
	e.f("<%s dir=\"%s\"%s%s>\n", tag, blocksDir(md.BlockChildren(l)), start, cls)
	for _, it := range l.Items {
		e.item(it, l.Tight)
	}
	e.f("</%s>\n", tag)
}

func (e *exporter) item(it *md.ListItem, tight bool) {
	if it.Task == nil {
		e.w("<li dir=\"auto\"" + idAttr(it.ID) + ">")
		e.blocks(it.Children, tight)
		e.w("</li>\n")
		return
	}
	state, box := "open", calloutGlyphs.TaskOpen
	switch it.Task.State {
	case 'x', 'X':
		state, box = "done", calloutGlyphs.TaskDone
	case '/':
		state, box = "doing", calloutGlyphs.TaskDoing
	case '-':
		state, box = "cancelled", calloutGlyphs.TaskCancelled
	case ' ':
	default:
		state, box = "other", "["+string(it.Task.State)+"]"
	}
	e.f("<li class=\"task %s\" dir=\"auto\"%s><span class=\"box\" aria-hidden=\"true\">%s</span> ", state, idAttr(it.ID), escape(box))
	children := it.Children
	if len(children) > 0 {
		if p, ok := children[0].(*md.Paragraph); ok {
			e.taskText(p, it.Task)
			children = children[1:]
		}
	}
	e.blocks(children, tight)
	e.w("</li>\n")
}

// taskText writes a task's first paragraph with due and priority markers
// lifted into chips, as the reader shows them.
func (e *exporter) taskText(p *md.Paragraph, t *md.Task) {
	var sub exporter
	sub.doc, sub.opt, sub.today, sub.depth = e.doc, e.opt, e.today, e.depth
	sub.inlines(p.Inlines)
	body := sub.b.String()
	due := ""
	if m := dueRe.FindStringSubmatchIndex(body); m != nil {
		due = body[m[2]:m[3]]
		body = body[:m[0]] + body[m[1]:]
	}
	prio := ""
	if m := prioRe.FindStringSubmatchIndex(body); m != nil {
		prio = map[string]string{"🔺": "!!!", "⏫": "!!", "🔼": "!", "🔽": "↓", "⏬": "↓↓"}[body[m[2]:m[3]]]
		body = body[:m[0]] + body[m[1]:]
	}
	e.w("<span class=\"text\">")
	e.w(strings.TrimRight(body, " "))
	e.w("</span>")
	if prio != "" {
		e.w(" <span class=\"priority\">" + escape(prio) + "</span>")
	}
	if due != "" {
		cls := "due"
		word := "due"
		shown := due
		if d, err := time.Parse("2006-01-02", due); err == nil {
			today := time.Date(e.today.Year(), e.today.Month(), e.today.Day(), 0, 0, 0, 0, time.UTC)
			shown = d.Format("2 Jan 2006")
			switch int(d.Sub(today).Hours() / 24) {
			case 0:
				shown = "today"
			case 1:
				shown = "tomorrow"
			case -1:
				shown = "yesterday"
			}
			open := t.State != 'x' && t.State != 'X' && t.State != '-'
			if open && d.Before(today) {
				cls, word = "due overdue", "overdue"
			}
		}
		e.f(" <time class=\"%s\" datetime=\"%s\">%s %s</time>", cls, escape(due), word, escape(shown))
	}
}

func (e *exporter) code(c *md.CodeBlock) {
	lang := c.Lang
	e.w("<figure class=\"code\"" + idAttr(c.ID) + ">")
	if lang != "" {
		e.w("<figcaption>")
		e.esc(lang)
		e.w("</figcaption>")
	}
	lines := make([]string, len(c.Lines))
	for i, l := range c.Lines {
		lines[i] = text.ExpandTabs(l, 4)
	}
	e.w("<pre><code>")
	for i, toks := range render.Highlight(lang, lines) {
		if i > 0 {
			e.w("\n")
		}
		for _, tk := range toks {
			if tk.Class == render.TokPlain {
				e.esc(tk.Text)
				continue
			}
			e.w("<span class=\"tk-" + tk.Class.String() + "\">")
			e.esc(tk.Text)
			e.w("</span>")
		}
	}
	e.w("</code></pre></figure>\n")
}

func alignClass(a md.Align) string {
	switch a {
	case md.AlignLeft:
		return " class=\"l\""
	case md.AlignCenter:
		return " class=\"c\""
	case md.AlignRight:
		return " class=\"r\""
	}
	return ""
}

func (e *exporter) table(t *md.Table) {
	if t.Header == nil {
		return
	}
	e.w("<div class=\"table\"" + idAttr(t.ID) + "><table>\n<thead><tr>")
	for i, c := range t.Header.Cells {
		e.w("<th dir=\"auto\"" + alignClass(t.Align[min(i, len(t.Align)-1)]) + ">")
		e.inlines(c.Inlines)
		e.w("</th>")
	}
	e.w("</tr></thead>\n<tbody>\n")
	for _, r := range t.Rows {
		e.w("<tr>")
		for i, c := range r.Cells {
			e.w("<td dir=\"auto\"" + alignClass(t.Align[min(i, len(t.Align)-1)]) + ">")
			e.inlines(c.Inlines)
			e.w("</td>")
		}
		e.w("</tr>\n")
	}
	e.w("</tbody>\n</table></div>\n")
}

func (e *exporter) footnotes() {
	if len(e.doc.Footnotes) == 0 {
		return
	}
	e.w("<section class=\"footnotes\" role=\"doc-endnotes\">\n<ol>\n")
	for _, d := range e.doc.Footnotes {
		e.f("<li id=\"fn-%d\" value=\"%d\" dir=\"auto\">", d.Index, d.Index)
		e.blocks(d.Children, true)
		e.f(" <a class=\"back\" href=\"#fnref-%d\" aria-label=\"back to the text\">↩</a></li>\n", d.Index)
	}
	e.w("</ol>\n</section>\n")
}

// ---------------------------------------------------------------------------
// Inlines

func (e *exporter) inlines(nodes []md.Inline) {
	for _, n := range nodes {
		e.inline(n)
	}
}

// inlineTags are the attribute-free formatting tags passed through.
var inlineTags = map[string]bool{"kbd": true, "sub": true, "sup": true, "mark": true, "u": true,
	"ins": true, "del": true, "s": true, "small": true, "b": true, "i": true, "em": true, "strong": true,
	"abbr": true, "cite": true, "q": true, "var": true, "samp": true, "code": true}

func (e *exporter) inline(n md.Inline) {
	switch n := n.(type) {
	case *md.Text:
		e.esc(n.Value)
	case *md.SoftBreak:
		e.w("\n")
	case *md.HardBreak:
		e.w("<br>\n")
	case *md.Emphasis:
		e.w("<em>")
		e.inlines(n.Children)
		e.w("</em>")
	case *md.Strong:
		e.w("<strong>")
		e.inlines(n.Children)
		e.w("</strong>")
	case *md.Strikethrough:
		e.w("<del>")
		e.inlines(n.Children)
		e.w("</del>")
	case *md.Highlight:
		e.w("<mark>")
		e.inlines(n.Children)
		e.w("</mark>")
	case *md.Code:
		e.w("<code>")
		e.esc(n.Value)
		e.w("</code>")
	case *md.Math:
		cls := "math"
		if n.Display {
			cls += " display-inline"
		}
		e.w("<span class=\"" + cls + "\">")
		e.esc(n.Value)
		e.w("</span>")
	case *md.WikiLink:
		e.wikiLink(n)
	case *md.Link:
		e.link(n)
	case *md.Image:
		e.image(n)
	case *md.Tag:
		e.w(tagHTML(n.Name))
	case *md.FootnoteRef:
		if n.Index <= 0 {
			e.esc("[^" + n.Label + "]")
			return
		}
		e.f("<sup class=\"fnref\"><a id=\"fnref-%d\" href=\"#fn-%d\">[%d]</a></sup>", n.Index, n.Index, n.Index)
	case *md.RawHTML:
		raw := strings.TrimSpace(n.Raw)
		name := strings.ToLower(strings.Trim(raw, "</>"))
		if (raw == "<"+name+">" || raw == "</"+name+">") && inlineTags[name] {
			e.w(strings.ToLower(raw))
			return
		}
		e.w("<span class=\"raw\">")
		e.esc(n.Raw)
		e.w("</span>")
	case *md.Comment:
	}
}

// safeURL reports whether an external destination may become an href.
func safeURL(dest string) bool {
	u, err := url.Parse(strings.TrimSpace(dest))
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		return true
	}
	return false
}

// noteHref returns the relative href for a vault path (a note's .md
// becomes .html; other files keep their name), plus a fragment.
func (e *exporter) noteHref(p, heading, block string) string {
	target := p
	if ext := strings.ToLower(path.Ext(target)); ext == ".md" || ext == ".markdown" {
		target = strings.TrimSuffix(target, path.Ext(target)) + ".html"
	} else if ext == "" {
		target += ".html"
	}
	rel := relPath(path.Dir(e.opt.Path), target)
	href := (&url.URL{Path: rel}).EscapedPath()
	if strings.Contains(strings.SplitN(rel, "/", 2)[0], ":") {
		href = "./" + href // keep "a:b.html" from reading as a scheme
	}
	return href + fragment(heading, block)
}

func fragment(heading, block string) string {
	switch {
	case block != "":
		return "#" + url.PathEscape("^"+block)
	case heading != "":
		h := heading
		if i := strings.LastIndexByte(h, '#'); i >= 0 {
			h = h[i+1:]
		}
		return "#" + url.PathEscape(md.Slugify(h))
	}
	return ""
}

// relPath returns target relative to directory dir (both slash paths
// relative to the vault root).
func relPath(dir, target string) string {
	if dir == "." || dir == "" {
		return target
	}
	ds := strings.Split(dir, "/")
	ts := strings.Split(target, "/")
	i := 0
	for i < len(ds) && i < len(ts)-1 && ds[i] == ts[i] {
		i++
	}
	var parts []string
	for range ds[i:] {
		parts = append(parts, "..")
	}
	parts = append(parts, ts[i:]...)
	return strings.Join(parts, "/")
}

// resolve resolves an internal link to an href; ok false marks it broken.
func (e *exporter) resolve(q render.LinkQuery) (string, bool) {
	if q.Target == "" {
		frag := q.Heading
		if q.Block != "" {
			frag = "^" + q.Block
		}
		if frag == "" {
			return "#", true
		}
		if _, ok := md.ResolveAnchor(e.doc, frag); !ok {
			return "", false
		}
		return fragment(q.Heading, q.Block), true
	}
	if e.opt.Resolver == nil {
		return e.noteHref(q.Target, q.Heading, q.Block), true
	}
	p, ok := e.opt.Resolver.Resolve(q)
	if !ok {
		return "", false
	}
	return e.noteHref(p, q.Heading, q.Block), true
}

func wikiLabel(n *md.WikiLink) string {
	if n.HasLabel && n.Label != "" {
		return n.Label
	}
	frag := n.Heading
	if n.Block != "" {
		frag = "^" + n.Block
	}
	switch {
	case frag == "":
		return n.Target
	case n.Target == "":
		return frag
	}
	return n.Target + " › " + frag
}

func (e *exporter) wikiLink(n *md.WikiLink) {
	q := render.LinkQuery{Target: n.Target, Heading: n.Heading, Block: n.Block, Wiki: true, Embed: n.Embed}
	href, ok := e.resolve(q)
	label := wikiLabel(n)
	if n.Embed && isImage(n.Target) && ok {
		e.w("<img src=\"" + escape(href) + "\" alt=\"" + escape(label) + "\">")
		return
	}
	if !ok {
		e.w("<span class=\"link broken\" title=\"no such note\">")
		e.esc(label)
		e.w("</span>")
		return
	}
	cls := "link internal"
	if n.Embed {
		cls += " embed"
	}
	e.w("<a class=\"" + cls + "\" href=\"" + escape(href) + "\">")
	if n.Embed {
		e.w("<span class=\"glyph\">▣</span> ")
	}
	e.esc(label)
	e.w("</a>")
}

func isImage(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp", ".avif":
		return true
	}
	return false
}

func (e *exporter) link(n *md.Link) {
	if n.External {
		if !safeURL(n.Dest) {
			e.inlines(n.Children)
			return
		}
		title := ""
		if n.Title != "" {
			title = " title=\"" + escape(n.Title) + "\""
		}
		e.w("<a class=\"link external\" href=\"" + escape(n.Dest) + "\"" + title + " rel=\"noopener noreferrer\">")
		if len(n.Children) == 0 || n.Kind == md.LinkBare || n.Kind == md.LinkAutolink {
			e.esc(strings.TrimPrefix(n.Dest, "mailto:"))
		} else {
			e.inlines(n.Children)
		}
		e.w("</a>")
		return
	}
	q := render.LinkQuery{Target: n.Path, Heading: n.Fragment}
	if strings.HasPrefix(n.Fragment, "^") {
		q.Heading, q.Block = "", n.Fragment[1:]
	}
	href, ok := e.resolve(q)
	if !ok {
		e.w("<span class=\"link broken\">")
		e.inlines(n.Children)
		e.w("</span>")
		return
	}
	e.w("<a class=\"link internal\" href=\"" + escape(href) + "\">")
	if len(n.Children) == 0 {
		e.esc(n.Dest)
	} else {
		e.inlines(n.Children)
	}
	e.w("</a>")
}

func (e *exporter) image(n *md.Image) {
	alt := md.PlainText(n.Alt)
	if n.External {
		// No external requests: an external image becomes a link.
		if !safeURL(n.Src) {
			e.esc(alt)
			return
		}
		e.w("<a class=\"link external image\" href=\"" + escape(n.Src) + "\" rel=\"noopener noreferrer\"><span class=\"glyph\">▣</span> ")
		if alt == "" {
			alt = path.Base(n.Src)
		}
		e.esc(alt)
		e.w("</a>")
		return
	}
	src := n.Path
	if e.opt.Resolver != nil {
		if p, ok := e.opt.Resolver.Resolve(render.LinkQuery{Target: n.Path}); ok {
			src = p
		}
		src = relPath(path.Dir(e.opt.Path), src)
	}
	title := ""
	if n.Title != "" {
		title = " title=\"" + escape(n.Title) + "\""
	}
	e.w("<img src=\"" + escape((&url.URL{Path: src}).EscapedPath()) + "\" alt=\"" + escape(alt) + "\"" + title + ">")
}

// embedBlock renders a paragraph holding only a note embed as a quoted
// excerpt of the target (its section, when a heading is named).
func (e *exporter) embedBlock(p *md.Paragraph) bool {
	var link *md.WikiLink
	for _, n := range p.Inlines {
		switch n := n.(type) {
		case *md.WikiLink:
			if link != nil || !n.Embed {
				return false
			}
			link = n
		case *md.Text:
			if strings.TrimSpace(n.Value) != "" {
				return false
			}
		case *md.SoftBreak, *md.Comment:
		default:
			return false
		}
	}
	if link == nil || isImage(link.Target) || link.Target == "" || e.opt.Resolver == nil || e.depth > 0 {
		return false
	}
	q := render.LinkQuery{Target: link.Target, Heading: link.Heading, Block: link.Block, Wiki: true, Embed: true}
	pth, ok := e.opt.Resolver.Resolve(q)
	if !ok {
		return false
	}
	title, src, ok := e.opt.Resolver.Embed(pth)
	if !ok {
		return false
	}
	sub := md.Parse(src)
	blocks := section(sub, link.Heading, link.Block)
	if title == "" {
		title = link.Target
	}
	if link.Heading != "" {
		title += " › " + link.Heading
	}
	if len(blocks) > 0 && link.Heading == "" && link.Block == "" {
		if h, ok := blocks[0].(*md.Heading); ok && h.Level == 1 {
			blocks = blocks[1:]
		}
	}
	e.w("<blockquote class=\"embed\" dir=\"auto\">\n<p class=\"embed-title\"><a class=\"link internal\" href=\"")
	e.esc(e.noteHref(pth, link.Heading, link.Block))
	e.w("\"><span class=\"glyph\">▣</span> ")
	e.esc(title)
	e.w("</a></p>\n")
	child := &exporter{doc: sub, opt: e.opt, today: e.today, depth: e.depth + 1}
	child.opt.Path = pth
	child.blocks(blocks, false)
	e.w(child.b.String())
	e.w("</blockquote>\n")
	return true
}

func section(doc *md.Document, heading, block string) []md.Block {
	if block != "" {
		var found md.Block
		md.WalkBlocks(doc.Blocks, func(b md.Block) bool {
			if found == nil && b.BlockBase().ID == block {
				found = b
			}
			return found == nil
		})
		if found == nil {
			return nil
		}
		return []md.Block{found}
	}
	if heading == "" {
		return doc.Blocks
	}
	line, ok := md.ResolveAnchor(doc, heading)
	if !ok {
		return nil
	}
	for i, b := range doc.Blocks {
		h, isH := b.(*md.Heading)
		if !isH || h.StartLine != line {
			continue
		}
		end := len(doc.Blocks)
		for j := i + 1; j < len(doc.Blocks); j++ {
			if h2, ok := doc.Blocks[j].(*md.Heading); ok && h2.Level <= h.Level {
				end = j
				break
			}
		}
		return doc.Blocks[i+1 : end]
	}
	return nil
}
