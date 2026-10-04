package render

import (
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ZahakJ/folio/internal/md"
	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/theme"
)

// inlines appends the spans for nodes, styled over a, to out.
func (r *renderer) inlines(nodes []md.Inline, a attr, out []sp) []sp {
	afterComment := false
	for _, n := range nodes {
		start := len(out)
		out = r.inline(n, a, out)
		if _, ok := n.(*md.Comment); ok {
			afterComment = true
			continue
		}
		// A hidden comment between two spaces would leave a double
		// space: drop the second.
		if afterComment && start < len(out) && start > 0 &&
			strings.HasSuffix(out[start-1].Text, " ") && strings.HasPrefix(out[start].Text, " ") {
			out[start].Text = out[start].Text[1:]
		}
		afterComment = false
	}
	return out
}

// lineOf returns the source line of a node's first byte.
func (r *renderer) lineOf(n md.Inline) int32 {
	l, _ := r.doc.LineCol(n.SourceSpan().Start)
	return int32(l)
}

func (r *renderer) inline(n md.Inline, a attr, out []sp) []sp {
	switch n := n.(type) {
	case *md.Text:
		a.line = r.lineOf(n)
		return append(out, sp{Text: cleanText(n.Value), Style: a})
	case *md.SoftBreak:
		a.line = r.lineOf(n)
		return append(out, sp{Text: " ", Style: a})
	case *md.HardBreak:
		a.line = r.lineOf(n)
		return append(out, sp{Text: "\n", Style: a})
	case *md.Emphasis:
		a.st.Attrs |= theme.Italic
		return r.inlines(n.Children, a, out)
	case *md.Strong:
		a.st.Attrs |= theme.Bold
		return r.inlines(n.Children, a, out)
	case *md.Strikethrough:
		a.st = r.faint().Over(theme.Style{BG: a.st.BG, Attrs: a.st.Attrs}).With(theme.Strike)
		return r.inlines(n.Children, a, out)
	case *md.Highlight:
		a.st = r.t.Highlight.Over(a.st)
		return r.inlines(n.Children, a, out)
	case *md.Code:
		a.line = r.lineOf(n)
		v := cleanText(n.Value)
		if r.raised {
			st := theme.Style{FG: r.t.Text, BG: r.t.Raised, Attrs: a.st.Attrs &^ (theme.Italic | theme.Strike)}
			if a.st.Attrs&theme.Strike != 0 {
				st.Attrs |= theme.Strike
			}
			// The padding cells are non-breaking so the ground never
			// dangles alone at a line edge.
			return append(out, sp{Text: nbsp + v + nbsp, Style: attr{st: st, hit: a.hit, line: a.line}})
		}
		tick := attr{st: theme.Style{FG: r.t.Accent, Attrs: a.st.Attrs & theme.Bold}, hit: a.hit, line: a.line}
		if r.t.Accent.IsDefault() {
			tick.st = r.faint()
		}
		body := attr{st: theme.Style{FG: a.st.FG, Attrs: a.st.Attrs}, hit: a.hit, line: a.line}
		return append(out, sp{Text: "`", Style: tick}, sp{Text: v, Style: body}, sp{Text: "`", Style: tick})
	case *md.Math:
		a.line = r.lineOf(n)
		delim := "$"
		if n.Display {
			delim = "$$"
		}
		ms := attr{st: theme.Style{FG: r.t.Math, BG: a.st.BG}, hit: a.hit, line: a.line}
		if r.t.Math.IsDefault() {
			ms.st.Attrs |= theme.Italic
		}
		fd := attr{st: r.faint(), hit: a.hit, line: a.line}
		return append(out, sp{Text: delim, Style: fd}, sp{Text: cleanText(n.Value), Style: ms}, sp{Text: delim, Style: fd})
	case *md.WikiLink:
		return r.wikiLink(n, a, out)
	case *md.Link:
		return r.mdLink(n, a, out)
	case *md.Image:
		return r.image(n, a, out)
	case *md.Tag:
		a.line = r.lineOf(n)
		hash := attr{st: theme.Style{FG: r.t.Accent, BG: a.st.BG, Attrs: a.st.Attrs & theme.Bold}, hit: a.hit, line: a.line}
		name := attr{st: theme.Style{FG: r.t.Muted, BG: a.st.BG, Attrs: a.st.Attrs & (theme.Bold | theme.Italic)}, hit: a.hit, line: a.line}
		return append(out, sp{Text: "#", Style: hash}, sp{Text: cleanText(n.Name), Style: name})
	case *md.FootnoteRef:
		a.line = r.lineOf(n)
		if n.Index <= 0 {
			return append(out, sp{Text: "[^" + cleanText(n.Label) + "]", Style: attr{st: r.faint(), line: a.line}})
		}
		hit := r.addHit(pendingHit{kind: HitFootnote, footnote: n.Index})
		st := theme.Style{FG: r.t.Accent, BG: a.st.BG}
		if r.t.Accent.IsDefault() {
			st.Attrs |= theme.Bold
		}
		return append(out, sp{Text: "[" + strconv.Itoa(n.Index) + "]", Style: attr{st: st, hit: hit, line: a.line}})
	case *md.RawHTML:
		a.line = r.lineOf(n)
		return append(out, sp{Text: cleanText(n.Raw), Style: attr{st: r.faint(), hit: a.hit, line: a.line}})
	case *md.Comment:
		return out
	}
	return out
}

// linkStyle is the ink of a link with the given status, over the
// surrounding attributes (bold/italic carry through).
func (r *renderer) linkStyle(st LinkStatus, a attr) theme.Style {
	if st == LinkBroken {
		s := theme.Style{FG: r.t.Danger, BG: a.st.BG, Attrs: a.st.Attrs&(theme.Bold|theme.Italic) | theme.CurlyUnderline}
		return s
	}
	s := r.t.LinkStyle()
	s.BG = a.st.BG
	s.Attrs |= a.st.Attrs & (theme.Bold | theme.Italic | theme.Strike)
	return s
}

// registerLink adds a link to the page and returns its hit index.
func (r *renderer) registerLink(l Link, n md.Inline) int32 {
	off := n.SourceSpan().Start
	l.SrcLine, l.SrcCol = r.doc.LineCol(off)
	l.Offset = off
	l.Line = -1
	r.links = append(r.links, l)
	return r.addHit(pendingHit{kind: HitLink, link: len(r.links) - 1})
}

// resolve returns the status and resolved path of an internal link.
func (r *renderer) resolve(q LinkQuery) (LinkStatus, string) {
	if q.Target == "" {
		frag := q.Heading
		if q.Block != "" {
			frag = "^" + q.Block
		}
		if frag == "" {
			return LinkResolved, ""
		}
		if _, ok := md.ResolveAnchor(r.doc, frag); ok {
			return LinkResolved, ""
		}
		return LinkBroken, ""
	}
	if r.opt.Resolver == nil {
		return LinkResolved, ""
	}
	p, ok := r.opt.Resolver.Resolve(q)
	if !ok {
		return LinkBroken, ""
	}
	return LinkResolved, p
}

func (r *renderer) wikiLink(n *md.WikiLink, a attr, out []sp) []sp {
	a.line = r.lineOf(n)
	q := LinkQuery{Target: n.Target, Heading: n.Heading, Block: n.Block, Wiki: true, Embed: n.Embed}
	status, p := r.resolve(q)
	label := cleanText(r.wikiDisplay(n))
	if n.Embed {
		return r.embedInline(n, status, p, a, out)
	}
	hit := r.registerLink(Link{Kind: LinkWiki, Status: status, Text: label, Target: n.Target,
		Heading: n.Heading, Block: n.Block, Path: p}, n)
	return append(out, sp{Text: label, Style: attr{st: r.linkStyle(status, a), hit: hit, line: a.line}})
}

// wikiDisplay is the text shown for a wikilink: its label, else the
// target with the heading or block after a breadcrumb separator.
func (r *renderer) wikiDisplay(n *md.WikiLink) string {
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
	return n.Target + " " + r.g.Crumb + " " + frag
}

// isImageName reports file names shown as image placeholders.
func isImageName(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp", ".avif", ".tif", ".tiff", ".ico", ".heic":
		return true
	}
	return false
}

// isNoteName reports targets that name a note (no extension or .md).
func isNoteName(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	return ext == "" || ext == ".md" || ext == ".markdown" || len(ext) > 6 || strings.ContainsAny(ext, " ")
}

// embedInline renders an embed in running text as a placeholder:
// "▣ label  file" for files, "▣ Note" for notes.
func (r *renderer) embedInline(n *md.WikiLink, status LinkStatus, p string, a attr, out []sp) []sp {
	label := cleanText(n.Display())
	hit := r.registerLink(Link{Kind: LinkEmbed, Status: status, Text: label, Target: n.Target,
		Heading: n.Heading, Block: n.Block, Path: p}, n)
	gst := theme.Style{FG: r.t.Accent, BG: a.st.BG}
	if status == LinkBroken {
		gst.FG = r.t.Danger
	}
	out = append(out, sp{Text: r.g.Image + nbsp, Style: attr{st: gst, hit: hit, line: a.line}})
	if !isNoteName(n.Target) && !n.HasLabel {
		// A file: its name is the label, shown muted.
		st := theme.Style{FG: r.t.Muted, BG: a.st.BG}
		if status == LinkBroken {
			st = r.linkStyle(status, a)
		}
		return append(out, sp{Text: path.Base(label), Style: attr{st: st, hit: hit, line: a.line}})
	}
	st := r.linkStyle(status, a)
	out = append(out, sp{Text: label, Style: attr{st: st, hit: hit, line: a.line}})
	if !isNoteName(n.Target) {
		out = append(out, sp{Text: nbsp + nbsp + path.Base(cleanText(n.Target)), Style: attr{st: theme.Style{FG: r.t.Muted, BG: a.st.BG}, line: a.line}})
	}
	return out
}

func (r *renderer) mdLink(n *md.Link, a attr, out []sp) []sp {
	a.line = r.lineOf(n)
	var l Link
	l.Kind = LinkMarkdown
	if n.Kind == md.LinkBare || n.Kind == md.LinkAutolink {
		l.Kind = LinkURL
	}
	if n.External {
		l.Status = LinkExternal
		l.URL = n.Dest
	} else {
		target := n.Path
		q := LinkQuery{Target: target, Heading: n.Fragment}
		if strings.HasPrefix(n.Fragment, "^") {
			q.Heading, q.Block = "", n.Fragment[1:]
		}
		l.Status, l.Path = r.resolve(q)
		l.Target, l.Heading, l.Block = target, q.Heading, q.Block
	}
	label := cleanText(md.PlainText(n.Children))
	if label == "" {
		label = cleanText(n.Dest)
	}
	l.Text = label
	hit := r.registerLink(l, n)
	st := r.linkStyle(l.Status, a)
	if l.Status == LinkExternal {
		st.Link = n.Dest
	}
	la := attr{st: st, hit: hit, line: a.line}
	if l.Kind == LinkURL || len(n.Children) == 0 {
		shown := label
		if l.Kind == LinkURL {
			shown = r.shortURL(n.Dest, label)
		}
		out = append(out, sp{Text: shown, Style: la})
	} else {
		start := len(out)
		out = r.inlines(n.Children, la, out)
		// Children keep their emphasis but take the link ink and hit.
		for i := start; i < len(out); i++ {
			s := &out[i].Style
			s.hit = hit
			s.st.FG = st.FG
			if !st.BG.IsDefault() && s.st.BG.IsDefault() {
				s.st.BG = st.BG
			}
			s.st.Attrs |= st.Attrs
			s.st.Link = st.Link
		}
	}
	if l.Status == LinkExternal {
		ext := r.faint()
		ext.BG = a.st.BG
		ext.Link = n.Dest
		out = append(out, sp{Text: nbsp + r.g.External, Style: attr{st: ext, hit: hit, line: a.line}})
	}
	return out
}

// shortURL shortens a bare URL longer than half the measure to its host
// plus an ellipsis (DESIGN.md §5).
func (r *renderer) shortURL(dest, label string) string {
	if Width(label) <= r.measure/2 {
		return label
	}
	u, err := url.Parse(dest)
	if err != nil || u.Host == "" {
		return label
	}
	host := strings.TrimPrefix(u.Host, "www.")
	return host + r.g.Ellipsis
}

func (r *renderer) image(n *md.Image, a attr, out []sp) []sp {
	a.line = r.lineOf(n)
	l := Link{Kind: LinkImage}
	if n.External {
		l.Status, l.URL = LinkExternal, n.Src
	} else {
		l.Status, l.Path = r.resolve(LinkQuery{Target: n.Path})
		l.Target = n.Path
	}
	alt := cleanText(md.PlainText(n.Alt))
	name := n.Src
	if n.Path != "" {
		name = n.Path
	}
	name = cleanText(path.Base(name))
	l.Text = alt
	if alt == "" {
		l.Text = name
	}
	hit := r.registerLink(l, n)
	gst := theme.Style{FG: r.t.Accent, BG: a.st.BG}
	if l.Status == LinkBroken {
		gst.FG = r.t.Danger
	}
	out = append(out, sp{Text: r.g.Image + nbsp, Style: attr{st: gst, hit: hit, line: a.line}})
	if alt != "" {
		st := theme.Style{FG: a.st.FG, BG: a.st.BG, Attrs: a.st.Attrs | theme.Italic}
		if l.Status == LinkBroken {
			st = r.linkStyle(l.Status, a)
		}
		out = append(out, sp{Text: alt, Style: attr{st: st, hit: hit, line: a.line}},
			sp{Text: nbsp + nbsp, Style: attr{st: theme.Style{BG: a.st.BG}, line: a.line}})
	}
	ns := theme.Style{FG: r.t.Muted, BG: a.st.BG}
	if r.t.Muted.IsDefault() {
		ns = r.faint()
	}
	if l.Status == LinkExternal {
		ns.Link = n.Src
	}
	return append(out, sp{Text: name, Style: attr{st: ns, hit: hit, line: a.line}})
}

// cleanText makes note text safe for a terminal: invalid UTF-8 becomes
// U+FFFD, tabs become spaces, other C0/C1 controls (ESC above all) and the
// explicit bidi controls are dropped. The fast path returns s unchanged.
func cleanText(s string) string {
	clean := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f || c >= 0x80 {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		ru, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case ru == utf8.RuneError && size == 1:
			b.WriteRune('�')
		case ru == '\t':
			b.WriteByte(' ')
		case ru == '\n':
			b.WriteByte(' ')
		case ru < 0x20 || ru == 0x7f || (ru >= 0x80 && ru < 0xa0):
		case ru >= 0x202a && ru <= 0x202e, ru >= 0x2066 && ru <= 0x2069:
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// cleanCode is cleanText for verbatim lines: tabs are expanded to 4-cell
// stops instead of becoming single spaces.
func cleanCode(s string) string {
	if strings.IndexByte(s, '\t') >= 0 {
		s = text.ExpandTabs(s, 4)
	}
	return cleanText(s)
}
