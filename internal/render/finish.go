package render

import (
	"strconv"
	"strings"
	"time"

	"github.com/ZahakJ/astrolabe-cli/internal/md"
	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
)

// finishLine prepares one wrapped line of a w-cell box for display: the
// bidi display transform when enabled, and right alignment for RTL blocks.
func (r *renderer) finishLine(ln []sp, dir text.Direction, w int) []sp {
	ln = r.bidiLine(ln, dir)
	if dir == text.RTL {
		if gap := w - spansW(ln); gap > 0 {
			ln = append([]sp{mk(spaces(gap), theme.Style{})}, ln...)
		}
	}
	return ln
}

// bidiLine applies Arabic shaping and bidi reordering (with mirroring) to a
// line when Options.Bidi is set and the line needs it; otherwise the line is
// returned in logical order, untouched.
func (r *renderer) bidiLine(ln []sp, dir text.Direction) []sp {
	if !r.opt.Bidi {
		return ln
	}
	need := dir == text.RTL
	if !need {
		for _, s := range ln {
			if text.HasRTL(s.Text) {
				need = true
				break
			}
		}
	}
	if !need {
		return ln
	}
	type cell struct {
		g string
		a attr
	}
	var cells []cell
	for _, s := range ln {
		for _, g := range text.Graphemes(s.Text) {
			cells = append(cells, cell{g.Text, s.Style})
		}
	}
	cl := make([]string, len(cells))
	for i, c := range cells {
		cl[i] = c.g
	}
	shaped := text.ShapeClusters(cl)
	bl := text.Reorder(shaped, dir)
	out := make([]sp, 0, len(ln))
	for _, l := range bl.VisualToLogical {
		g := shaped[l]
		if g == "" {
			continue
		}
		if bl.IsRTL(l) {
			g = text.Mirror(g)
		}
		a := cells[l].a
		if n := len(out); n > 0 && out[n-1].Style == a {
			out[n-1].Text += g
			continue
		}
		out = append(out, sp{Text: g, Style: a})
	}
	return out
}

// ---------------------------------------------------------------------------
// Title block

// titleBlock renders the title, the chip line, the hairline and the
// properties fold; it returns the blocks left to render (a leading H1 used
// as the title is consumed).
func (r *renderer) titleBlock(blocks []md.Block) ([]row, []md.Block) {
	fm := r.doc.Frontmatter
	title := ""
	var titleInl []md.Inline
	src0, src1 := -1, -1
	if fm != nil {
		title = fm.Title
		src0, src1 = fm.StartLine, fm.EndLine
		if f, ok := fm.Get("title"); ok {
			src0, src1 = f.Line, f.Line
		}
	}
	// The first non-skipped block: a leading H1.
	var lead *md.Heading
	leadIdx := -1
	for i, b := range blocks {
		if skipped(b) {
			continue
		}
		if h, ok := b.(*md.Heading); ok && h.Level == 1 {
			lead, leadIdx = h, i
		}
		break
	}
	if lead != nil && (title == "" || strings.EqualFold(strings.TrimSpace(lead.Text), strings.TrimSpace(title))) {
		title, titleInl = lead.Text, lead.Inlines
		src0, src1 = lead.StartLine, lead.EndLine
		rest := make([]md.Block, 0, len(blocks)-1)
		rest = append(rest, blocks[:leadIdx]...)
		blocks = append(rest, blocks[leadIdx+1:]...)
	}
	if title == "" && fm != nil {
		title = r.opt.Title
	}
	if title == "" && fm == nil {
		return nil, blocks
	}
	w := r.measure
	var rows []row
	if title != "" {
		a := attr{st: theme.Style{FG: r.t.TitleInk(), Attrs: theme.Bold}, line: int32(src0)}
		var spans []sp
		if titleInl != nil {
			spans = r.inlines(titleInl, a, nil)
		} else {
			spans = []sp{{Text: cleanText(title), Style: a}}
		}
		tr := r.wrapRows(spans, w, w, src0, src1, KindTitle, "")
		for i := range tr {
			tr[i].level = 1
		}
		rows = append(rows, tr...)
	}
	// Chip line: date · #tag #tag · N backlinks. Each chip (the date, one
	// tag, the count) is a unit that never breaks across lines — a tag
	// like #navigation/astrolabe must not wrap at its slash — laid out
	// greedily with the separator that precedes it.
	type chip struct{ sep, body []sp }
	var chips []chip
	dot := []sp{mk(nbsp+r.g.Dot+" ", r.faint())}
	group := func() []sp {
		if len(chips) == 0 {
			return nil
		}
		return dot
	}
	if fm != nil {
		date := fm.Date
		if date == "" {
			date = fm.Created
		}
		if date != "" {
			chips = append(chips, chip{group(), []sp{mk(prettyDate(cleanText(date)), r.muted())}})
		}
		for i, tg := range fm.Tags {
			sep := []sp{mk(" ", r.muted())}
			if i == 0 {
				sep = group()
			}
			name := cleanText(tg)
			if r.opt.Bidi && text.HasRTL(name) {
				// Each tag is shaped and ordered on its own, like a bidi
				// isolate (see the chip line below).
				name = text.Visual(name, text.BaseDirection(name))
			}
			chips = append(chips, chip{sep, []sp{mk("#", r.accent()), mk(name, r.muted())}})
		}
	}
	if r.opt.Backlinks > 0 {
		chips = append(chips, chip{group(), []sp{mk(plural(r.opt.Backlinks, "backlink"), r.muted())}})
	}
	fmSrc0, fmSrc1 := -1, -1
	if fm != nil {
		fmSrc0, fmSrc1 = fm.StartLine, fm.EndLine
	}
	if len(chips) > 0 {
		// The chip line is metadata in the page's left-to-right frame, so
		// it is not run through the paragraph bidi pass: an Arabic first
		// tag would otherwise flip the whole line and drag the date, the
		// other tags and the backlink count into right-to-left order. Tags
		// were put in visual order one by one above. Under a right-to-left
		// title the line is right-aligned to stay with it.
		rtlTitle := len(rows) > 0 && rows[0].rtl
		emit := func(ln []sp) {
			if gap := w - spansW(ln); rtlTitle && gap > 0 {
				ln = append([]sp{mk(spaces(gap), theme.Style{})}, ln...)
			}
			rows = append(rows, row{spans: ln, src0: fmSrc0, src1: fmSrc1, kind: KindTitle})
		}
		var ln []sp
		for _, c := range chips {
			body := text.TruncateSpans(c.body, w, r.g.Ellipsis)
			if lw := spansW(ln); lw > 0 && lw+spansW(c.sep)+spansW(body) > w {
				emit(ln)
				ln = nil
			}
			if len(ln) > 0 {
				ln = append(ln, c.sep...)
			}
			ln = append(ln, body...)
		}
		emit(ln)
	}
	rule := mk(strings.Repeat(r.g.Rule, w/max(1, text.Width(r.g.Rule))), r.hair())
	rows = append(rows, row{spans: []sp{rule}, src0: src1, src1: src1, kind: KindTitle})
	if pr := r.properties(w); len(pr) > 0 {
		rows = append(rows, pr...)
	}
	return rows, blocks
}

// shownKeys are the frontmatter keys the title block already shows.
var shownKeys = map[string]bool{"title": true, "tags": true, "tag": true, "date": true}

// properties renders the remaining frontmatter keys behind a fold.
func (r *renderer) properties(w int) []row {
	fm := r.doc.Frontmatter
	if fm == nil {
		return nil
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
		return nil
	}
	const id = "properties"
	folded := r.foldState(id, true)
	r.registerFold(id, folded, fm.StartLine, KindProperties, -1)
	glyph := r.g.FoldOpen
	if folded {
		glyph = r.g.FoldClosed
	}
	hit := r.addHit(pendingHit{kind: HitFold, fold: id})
	head := row{
		spans: []sp{
			{Text: "properties", Style: attr{st: r.faint(), hit: hit, line: -1}},
			mk(" "+r.g.Dot+" ", r.faint()),
			mk(itoa(len(fields)), r.faint()),
		},
		gutter: []sp{{Text: glyph, Style: attr{st: r.faint(), hit: hit, line: -1}}},
		src0:   fm.StartLine, src1: fm.EndLine, kind: KindProperties, fold: id, folded: folded,
	}
	rows := []row{head}
	if folded {
		return rows
	}
	kw := 0
	for _, f := range fields {
		kw = max(kw, text.Width(cleanText(f.Key)))
	}
	kw = min(kw, max(6, w/3))
	for _, f := range fields {
		val := f.Scalar()
		if (val == "" && len(f.Lines) > 0) || strings.HasPrefix(strings.TrimSpace(f.Value), "[") {
			val = strings.Join(f.List(), ", ")
		}
		key := text.Truncate(cleanText(f.Key), kw, r.g.Ellipsis)
		spans := []sp{{Text: strings.ReplaceAll(text.PadRight(key, kw), " ", nbsp), Style: attr{st: r.faint(), line: int32(f.Line)}},
			mk(nbsp+nbsp, theme.Style{}),
			{Text: cleanText(val), Style: attr{st: r.muted(), line: int32(f.Line)}}}
		rows = append(rows, r.wrapRows(spans, w, w, f.Line, f.Line+len(f.Lines), KindProperties, "")...)
	}
	return rows
}

func itoa(n int) string { return strconv.Itoa(n) }

// prettyDate turns "2026-10-04" (optionally with a time) into "4 Oct 2026";
// anything else is shown as written.
func prettyDate(s string) string {
	s = strings.Trim(s, `"' `)
	if len(s) >= 10 {
		if d, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return d.Format("2 Jan 2006")
		}
	}
	return s
}

// ---------------------------------------------------------------------------
// Footnotes

// footnotes renders the definitions at the end of the page under a short
// hairline, numbered in accent with a hanging indent.
func (r *renderer) footnotes(w int) []row {
	defs := r.doc.Footnotes
	if len(defs) == 0 {
		return nil
	}
	rows := []row{{spans: []sp{mk(strings.Repeat(r.g.Rule, min(w, 12)), r.hair())}, src0: -1, src1: -1, kind: KindFootnote}}
	numW := len(itoa(len(defs))) + 1
	for _, d := range defs {
		num := itoa(d.Index) + "."
		hit := r.addHit(pendingHit{kind: HitFootnote, footnote: d.Index})
		st := r.accent()
		marker := []sp{{Text: text.PadLeft(num, numW), Style: attr{st: st, hit: hit, line: int32(d.StartLine)}}, mk(" ", theme.Style{})}
		mw := spansW(marker)
		a := r.body()
		a.st.FG = r.t.Muted
		body := r.blocks(d.Children, w-mw, ctx{base: &a}, true)
		if len(body) == 0 {
			body = []row{{src0: d.StartLine, src1: d.EndLine}}
		}
		for i := range body {
			body[i].kind = KindFootnote
			if body[i].src0 < 0 {
				body[i].src0, body[i].src1 = d.StartLine, d.EndLine
			}
		}
		body[0].blockID = "fn:" + itoa(d.Index)
		rows = append(rows, prefix(body, marker, []sp{mk(spaces(mw), theme.Style{})}, false, w)...)
	}
	return rows
}

// ---------------------------------------------------------------------------
// Embeds

// embedParagraph renders a paragraph made only of one ![[Note]] embed as a
// preview: a title line, then the first EmbedLines lines of the target
// inside a quote bar.
func (r *renderer) embedParagraph(p *md.Paragraph, w int, c ctx) ([]row, bool) {
	var link *md.WikiLink
	for _, n := range p.Inlines {
		switch n := n.(type) {
		case *md.WikiLink:
			if link != nil || !n.Embed {
				return nil, false
			}
			link = n
		case *md.Text:
			if strings.TrimSpace(n.Value) != "" {
				return nil, false
			}
		case *md.SoftBreak, *md.Comment:
		default:
			return nil, false
		}
	}
	if link == nil || !isNoteName(link.Target) || r.depth > 0 || r.opt.Resolver == nil || link.Target == "" {
		return nil, false
	}
	q := LinkQuery{Target: link.Target, Heading: link.Heading, Block: link.Block, Wiki: true, Embed: true}
	status, p2 := r.resolve(q)
	if status != LinkResolved {
		return nil, false
	}
	title, src, ok := r.opt.Resolver.Embed(p2)
	if !ok {
		return nil, false
	}
	sub := md.Parse(src)
	blocks := sectionBlocks(sub, link.Heading, link.Block)
	if title == "" {
		title = link.Target
	}
	// The preview's own title is already on the embed's title line.
	for i, b := range blocks {
		if skipped(b) {
			continue
		}
		if h, ok := b.(*md.Heading); ok && h.Level == 1 && link.Heading == "" && link.Block == "" {
			blocks = append(append([]md.Block(nil), blocks[:i]...), blocks[i+1:]...)
		}
		break
	}
	shown := title
	if link.Heading != "" {
		shown += " " + r.g.Crumb + " " + link.Heading
	}
	if link.HasLabel && link.Label != "" {
		shown = link.Label
	}
	line := int32(p.StartLine)
	hit := r.registerLink(Link{Kind: LinkEmbed, Status: status, Text: cleanText(shown), Target: link.Target,
		Heading: link.Heading, Block: link.Block, Path: p2}, link)
	bar := mk(r.g.Bar+" ", r.accent())
	bw := spansW([]sp{bar})
	head := []sp{
		{Text: r.g.Image + nbsp, Style: attr{st: r.accent(), hit: hit, line: line}},
		{Text: cleanText(shown), Style: attr{st: theme.Style{FG: r.t.Heading, Attrs: theme.Bold}, hit: hit, line: line}},
	}
	rows := r.wrapRows(head, w, w, p.StartLine, p.EndLine, KindEmbed, p.ID)

	// The preview is rendered by a child renderer whose links are inert
	// (they are relative to the embedded note, not this one).
	opt := r.opt
	opt.NoTitleBlock = true
	opt.Folds = nil
	opt.Resolver = nil
	child := newRenderer(sub, opt, r.depth+1)
	child.measure = w - bw
	var body []row
	if len(blocks) > 0 {
		body = child.blocks(blocks, w-bw, ctx{}, false)
	}
	for len(body) > 0 && body[0].blank {
		body = body[1:]
	}
	if len(body) > EmbedLines {
		body = body[:EmbedLines]
		for len(body) > 0 && body[len(body)-1].blank {
			body = body[:len(body)-1]
		}
		body = append(body, row{spans: []sp{mk(r.g.Ellipsis, r.faint())}})
	}
	for i := range body {
		for j := range body[i].spans {
			body[i].spans[j].Style.hit = 0
		}
		body[i].gutter = nil
		body[i].fold = ""
		body[i].code = nil
		body[i].src0, body[i].src1 = p.StartLine, p.EndLine
		body[i].kind = KindEmbed
		body[i].out = 0
	}
	// A right-to-left preview carries its bar on the right, like a quote.
	rtl := blocksDirection(blocks) == text.RTL
	body = prefix(body, []sp{bar}, []sp{bar}, rtl, w)
	return append(rows, body...), true
}

// sectionBlocks selects the blocks of doc under heading (up to the next
// heading of the same or higher rank), or the block with id block, or all
// top-level blocks.
func sectionBlocks(doc *md.Document, heading, block string) []md.Block {
	if block != "" {
		var found md.Block
		md.WalkBlocks(doc.Blocks, func(b md.Block) bool {
			if found == nil && b.BlockBase().ID == block {
				found = b
			}
			return found == nil
		})
		if found != nil {
			return []md.Block{found}
		}
		return nil
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

// ---------------------------------------------------------------------------
// Page assembly

// finish places rows on the page (margins, gutter marks, ground), resolves
// hit regions and builds the page indexes.
func (r *renderer) finish(rows []row) *Page {
	// Trim trailing blank rows.
	for len(rows) > 0 && rows[len(rows)-1].blank {
		rows = rows[:len(rows)-1]
	}
	p := &Page{Width: r.pane, Measure: r.measure, Left: r.left, Footnotes: map[int]int{}}
	p.CursorX = max(0, r.left-3)
	p.ChevronX = max(0, r.left-2)
	ground := theme.Style{}
	if r.ground {
		ground.BG = r.t.Ground
	}
	regions := make([][]Region, len(r.links))
	p.Lines = make([]Line, len(rows))
	for i, rw := range rows {
		spans := make([]Span, 0, len(rw.spans)+len(rw.gutter)+4)
		x0 := r.left - rw.out
		// Left margin, with a gutter mark in the chevron column.
		if len(rw.gutter) > 0 && !rw.gutterRight && x0 >= 2 {
			spans = append(spans, Span{Text: spaces(p.ChevronX), Style: ground})
			gw := spansW(rw.gutter)
			spans = appendSp(spans, rw.gutter, ground)
			spans = append(spans, Span{Text: spaces(max(0, x0-p.ChevronX-gw)), Style: ground})
		} else if x0 > 0 {
			spans = append(spans, Span{Text: spaces(x0), Style: ground})
		}
		spans = appendSp(spans, rw.spans, ground)
		if len(rw.gutter) > 0 && rw.gutterRight {
			cur := spansWidth(spans)
			if gap := r.left + r.measure + 1 - cur; gap > 0 {
				spans = append(spans, Span{Text: spaces(gap), Style: ground})
			}
			spans = appendSp(spans, rw.gutter, ground)
		}
		// Hit regions, from the spans' positions.
		var hits []Hit
		x := 0
		hx := func(list []sp, start int) int {
			for _, s := range list {
				w := Width(s.Text)
				if s.Style.hit > 0 && w > 0 {
					h := r.hits[s.Style.hit-1]
					if n := len(hits); n > 0 && hits[n-1].X1 == start && sameHit(hits[n-1], h) {
						hits[n-1].X1 = start + w
					} else {
						hits = append(hits, toHit(h, start, start+w))
					}
				}
				start += w
			}
			return start
		}
		if len(rw.gutter) > 0 && !rw.gutterRight && x0 >= 2 {
			hx(rw.gutter, p.ChevronX)
		}
		x = hx(rw.spans, x0)
		if len(rw.gutter) > 0 && rw.gutterRight {
			hx(rw.gutter, max(x, r.left+r.measure+1))
		}
		width := spansWidth(spans)
		if width > r.pane {
			spans = sliceSpans(spans, 0, r.pane)
			width = spansWidth(spans)
		}
		if r.ground && width < r.pane {
			spans = append(spans, Span{Text: spaces(r.pane - width), Style: ground})
			width = r.pane
		}
		spans = mergeSpans(spans)
		ln := Line{Spans: spans, Width: width, SrcStart: rw.src0, SrcEnd: rw.src1, Kind: rw.kind,
			BlockID: rw.blockID, Level: rw.level, Fold: rw.fold, Folded: rw.folded, RTL: rw.rtl}
		if rw.blank {
			ln.Kind = KindBlank
			ln.SrcStart, ln.SrcEnd = -1, -1
		}
		if ln.SrcEnd < ln.SrcStart {
			ln.SrcEnd = ln.SrcStart
		}
		for _, h := range hits {
			if h.X1 > r.pane {
				h.X1 = r.pane
			}
			if h.X0 >= h.X1 {
				continue
			}
			ln.Hits = append(ln.Hits, h)
			if h.Kind == HitLink {
				regions[h.Link] = append(regions[h.Link], Region{Line: i, X0: h.X0, X1: h.X1})
			}
		}
		if rw.code != nil {
			cv := &CodeView{X: x0 + rw.code.x, View: rw.code.view, Width: rw.code.width,
				Ellipsis: r.g.Ellipsis}
			es := r.faint()
			if r.raised {
				es.BG = r.t.Raised
			}
			cv.EllipsisStyle = es
			cv.Content = make([]Span, len(rw.code.content))
			for k, s := range rw.code.content {
				cv.Content[k] = Span{Text: s.Text, Style: s.Style.st}
			}
			ln.Code = cv
		}
		if rw.fold != "" {
			if fi, ok := r.foldIdx[rw.fold]; ok && r.folds[fi].Line < 0 {
				r.folds[fi].Line = i
			}
		}
		if strings.HasPrefix(rw.blockID, "fn:") {
			n := 0
			for _, c := range rw.blockID[3:] {
				n = n*10 + int(c-'0')
			}
			if _, ok := p.Footnotes[n]; !ok {
				p.Footnotes[n] = i
			}
		}
		p.Lines[i] = ln
	}
	// Links: keep those shown, in document order, and renumber hits.
	remap := make([]int, len(r.links))
	for i, l := range r.links {
		remap[i] = -1
		if len(regions[i]) == 0 {
			continue
		}
		l.Regions = regions[i]
		l.Line = regions[i][0].Line
		remap[i] = len(p.Links)
		p.Links = append(p.Links, l)
	}
	for i := range p.Lines {
		for j := range p.Lines[i].Hits {
			if h := &p.Lines[i].Hits[j]; h.Kind == HitLink {
				h.Link = remap[h.Link]
			}
		}
	}
	p.Folds = r.folds
	r.buildSourceMap(p)
	for _, o := range md.Outline(r.doc) {
		e := OutlineEntry{Level: o.Level, Text: o.Text, Slug: o.Slug, SrcLine: o.Line}
		e.Line = p.LineForSource(o.Line)
		if e.Line < len(p.Lines) {
			l := p.Lines[e.Line]
			e.Visible = l.SrcStart <= o.Line && o.Line <= l.SrcEnd
		}
		if _, ok := r.foldIdx["h:"+o.Slug]; ok {
			e.Fold = "h:" + o.Slug
		}
		p.Outline = append(p.Outline, e)
	}
	return p
}

func sameHit(a Hit, b pendingHit) bool {
	switch b.kind {
	case HitLink:
		return a.Kind == HitLink && a.Link == b.link
	case HitTask:
		return a.Kind == HitTask && a.Task.Offset == b.task.Offset
	case HitFold:
		return a.Kind == HitFold && a.Fold == b.fold
	case HitFootnote:
		return a.Kind == HitFootnote && a.Footnote == b.footnote
	}
	return false
}

func toHit(h pendingHit, x0, x1 int) Hit {
	return Hit{X0: x0, X1: x1, Kind: h.kind, Link: h.link, Task: h.task, Fold: h.fold, Footnote: h.footnote}
}

// blanks backs spaces: substrings of one long run of spaces, so padding
// needs no allocation.
var blanks = strings.Repeat(" ", 512)

// spaces returns n spaces.
func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	if n <= len(blanks) {
		return blanks[:n]
	}
	return strings.Repeat(" ", n)
}

// appendSp converts internal spans to page spans: the ground fills unset
// backgrounds and the layout's non-breaking spaces become plain spaces.
func appendSp(dst []Span, src []sp, ground theme.Style) []Span {
	for _, s := range src {
		st := s.Style.st
		if st.BG.IsDefault() {
			st.BG = ground.BG
		}
		t := s.Text
		if strings.Contains(t, nbsp) {
			t = strings.ReplaceAll(t, nbsp, " ")
		}
		dst = append(dst, Span{Text: t, Style: st})
	}
	return dst
}

// buildSourceMap fills Page.srcFirst: every source line maps to the first
// rendered line showing it; lines without one map to the fold hiding them,
// else to the next rendered block (else the last line).
func (r *renderer) buildSourceMap(p *Page) {
	n := r.doc.LineCount()
	if n == 0 {
		n = 1
	}
	first := make([]int, n)
	for i := range first {
		first[i] = -1
	}
	for i, l := range p.Lines {
		if l.SrcStart < 0 {
			continue
		}
		for s := max(0, l.SrcStart); s <= l.SrcEnd && s < n; s++ {
			if first[s] < 0 {
				first[s] = i
			}
		}
	}
	// Folded ranges map to their heading's line (the last rendered line
	// starting before the range).
	for _, h := range r.hidden {
		at := -1
		for i, l := range p.Lines {
			if l.SrcStart >= 0 && l.SrcStart < h[0] && l.Fold != "" {
				at = i
			}
		}
		if at < 0 {
			continue
		}
		for s := h[0]; s <= h[1] && s < n; s++ {
			if first[s] < 0 {
				first[s] = at
			}
		}
	}
	// Remaining gaps take the next mapped line.
	next := len(p.Lines) - 1
	if next < 0 {
		next = 0
	}
	for s := n - 1; s >= 0; s-- {
		if first[s] >= 0 {
			next = first[s]
		} else {
			first[s] = next
		}
	}
	p.srcFirst = first
}
