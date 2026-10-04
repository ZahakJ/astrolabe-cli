package render

import (
	"strconv"
	"strings"
	"time"

	"github.com/ZahakJ/folio/internal/md"
	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/theme"
)

// attr is the internal per-span payload: a style, an optional hit (1-based
// index into renderer.hits, 0 for none) and the source line the text came
// from (-1 unknown).
type attr struct {
	st   theme.Style
	hit  int32
	line int32
}

// sp is an internal styled span.
type sp = text.Span[attr]

// nbsp joins padding and marks to their neighbours so the wrapper never
// separates them; it is turned back into a plain space on output.
const nbsp = " "

// pendingHit is a hit region before its columns are known.
type pendingHit struct {
	kind     HitKind
	link     int
	task     TaskRef
	fold     string
	footnote int
}

// row is one line under construction: content laid out at column 0 of the
// block's own box.
type row struct {
	spans      []sp
	src0, src1 int
	kind       Kind
	level      int
	blockID    string
	fold       string
	folded     bool
	rtl        bool
	blank      bool
	// code viewport: x is relative to the row start.
	code *codeRow
	// out shifts a top-level row left of the measure (wide tables).
	out int
	// gutter is a mark drawn in the margin (fold chevron); gutterRight
	// puts it in the right margin (RTL headings).
	gutter      []sp
	gutterRight bool
}

type codeRow struct {
	x, view int
	content []sp
	width   int
}

// renderer holds the state of one Render call.
type renderer struct {
	doc   *md.Document
	opt   Options
	t     theme.Theme
	g     theme.Glyphs
	today time.Time

	ground  bool // paint the page ground
	raised  bool // raised grounds (code) are available
	hits    []pendingHit
	links   []Link
	measure int
	pane    int
	left    int

	calloutN int // callouts seen, for fold ids
	folds    []FoldInfo
	foldIdx  map[string]int
	hidden   [][2]int // source ranges hidden by folds, with the heading's row
	hideRow  []int
	depth    int // embed nesting depth
	footRows map[int]int
	titleH1  md.Block
}

// Render lays out doc for opt. It never fails; malformed input renders as
// text.
func Render(doc *md.Document, opt Options) *Page {
	if doc == nil {
		doc = md.Parse("")
	}
	r := newRenderer(doc, opt, 0)
	rows := r.document()
	return r.finish(rows)
}

func newRenderer(doc *md.Document, opt Options, depth int) *renderer {
	if opt.Width < 20 {
		opt.Width = 20
	}
	if opt.Measure <= 0 {
		opt.Measure = DefaultMeasure
	}
	if opt.Glyphs.Name == "" {
		opt.Glyphs = theme.UnicodeGlyphs
	}
	r := &renderer{doc: doc, opt: opt, t: opt.Theme, g: opt.Glyphs, depth: depth,
		foldIdx: map[string]int{}, footRows: map[int]int{}}
	r.today = opt.Today
	if r.today.IsZero() {
		r.today = time.Now()
	}
	r.pane = opt.Width
	r.measure = min(opt.Measure, r.pane-2*Margin)
	r.left = max(Margin, (r.pane-r.measure+1)/2)
	if r.left+r.measure > r.pane {
		r.left = r.pane - r.measure
	}
	r.ground = opt.PaintGround && !r.t.Ground.IsDefault()
	r.raised = !r.t.Raised.IsDefault()
	return r
}

// ---------------------------------------------------------------------------
// Styles

func (r *renderer) body() attr { return attr{st: theme.Style{FG: r.t.Text}, line: -1} }

// faint returns the faint ink, falling back to the SGR faint attribute on
// themes without colours.
func (r *renderer) faint() theme.Style {
	if r.t.Faint.IsDefault() {
		return theme.Style{Attrs: theme.Faint}
	}
	return theme.Style{FG: r.t.Faint}
}

func (r *renderer) muted() theme.Style  { return theme.Style{FG: r.t.Muted} }
func (r *renderer) accent() theme.Style { return theme.Style{FG: r.t.Accent} }

// hair is the style of hairline rules.
func (r *renderer) hair() theme.Style {
	if r.t.Border.IsDefault() {
		return r.faint()
	}
	return theme.Style{FG: r.t.Border}
}

func (r *renderer) codeGround() theme.Style {
	if r.raised {
		return theme.Style{BG: r.t.Raised}
	}
	return theme.Style{}
}

func mk(s string, st theme.Style) sp { return sp{Text: s, Style: attr{st: st, line: -1}} }

func (r *renderer) addHit(h pendingHit) int32 {
	r.hits = append(r.hits, h)
	return int32(len(r.hits))
}

// ---------------------------------------------------------------------------
// Document

// document renders the title block, the body (honouring heading folds) and
// the footnotes.
func (r *renderer) document() []row {
	var rows []row
	blocks := r.doc.Blocks
	if !r.opt.NoTitleBlock {
		rows, blocks = r.titleBlock(blocks)
	}
	body := r.sectioned(blocks, r.measure)
	if len(rows) == 0 {
		rows = body
	} else if len(body) > 0 {
		rows = append(append(rows, blankRow()), body...)
	}
	if fn := r.footnotes(r.measure); len(fn) > 0 {
		if len(rows) > 0 {
			rows = append(rows, blankRow(), blankRow())
		}
		rows = append(rows, fn...)
	}
	return rows
}

func blankRow() row { return row{blank: true, src0: -1, src1: -1} }

// skipped reports blocks that produce no output.
func skipped(b md.Block) bool {
	switch b.(type) {
	case *md.CommentBlock, *md.LinkRefDef, *md.FootnoteDef:
		return true
	}
	return false
}

// sectioned renders top-level blocks, applying heading folds.
func (r *renderer) sectioned(blocks []md.Block, w int) []row {
	rows := make([]row, 0, 8*len(blocks))
	first := true
	// afterFold is set after a folded section: folded headings stack like
	// a table of contents, one blank row apart, instead of keeping the two
	// rows of air an H1/H2 gets above running text.
	afterFold := false
	for i := 0; i < len(blocks); i++ {
		b := blocks[i]
		if skipped(b) {
			continue
		}
		if !first {
			rows = append(rows, blankRow())
			if h, ok := b.(*md.Heading); ok && h.Level <= 2 && !afterFold {
				rows = append(rows, blankRow())
			}
		}
		first = false
		afterFold = false
		h, ok := b.(*md.Heading)
		if !ok {
			rows = append(rows, r.block(b, w, ctx{})...)
			continue
		}
		id := "h:" + h.Slug
		// The section runs to the next heading of the same or higher rank.
		end := len(blocks)
		for j := i + 1; j < len(blocks); j++ {
			if h2, ok := blocks[j].(*md.Heading); ok && h2.Level <= h.Level {
				end = j
				break
			}
		}
		folded := r.foldState(id, false)
		lastSrc := h.EndLine
		for j := i + 1; j < end; j++ {
			lastSrc = max(lastSrc, blocks[j].BlockBase().EndLine)
		}
		hidden := 0
		for j := i + 1; j < end; j++ {
			if !skipped(blocks[j]) || hidden > 0 {
				hidden = lastSrc - blocks[j].BlockBase().StartLine + 1
				break
			}
		}
		hr := r.heading(h, w, ctx{}, id, folded, hidden)
		r.registerFold(id, folded, h.StartLine, KindHeading, len(rows))
		rows = append(rows, hr...)
		if folded && end > i+1 {
			r.hidden = append(r.hidden, [2]int{h.EndLine + 1, lastSrc})
			r.hideRow = append(r.hideRow, -1) // resolved in finish: the heading row
			r.markFoldedInside(blocks[i+1 : end])
			i = end - 1
			afterFold = true
		}
	}
	return rows
}

// foldState returns the effective state of fold id.
func (r *renderer) foldState(id string, def bool) bool {
	if v, ok := r.opt.Folds[id]; ok {
		return v
	}
	return def
}

// registerFold records a fold; rowIdx is a provisional index fixed up in
// finish (folds find their line by id).
func (r *renderer) registerFold(id string, folded bool, src int, k Kind, _ int) {
	if r.depth > 0 {
		return
	}
	if _, dup := r.foldIdx[id]; dup {
		return
	}
	r.foldIdx[id] = len(r.folds)
	r.folds = append(r.folds, FoldInfo{ID: id, Folded: folded, Line: -1, SrcLine: src, Kind: k})
}

// markFoldedInside registers folds of blocks hidden inside a folded section
// so the TUI still knows about them (zR must reach them).
func (r *renderer) markFoldedInside(blocks []md.Block) {
	for _, b := range blocks {
		if h, ok := b.(*md.Heading); ok {
			id := "h:" + h.Slug
			r.registerFold(id, r.foldState(id, false), h.StartLine, KindHeading, -1)
		}
	}
	md.WalkBlocks(blocks, func(b md.Block) bool {
		if c, ok := b.(*md.Callout); ok {
			r.calloutN++
			id := "c:" + strconv.Itoa(r.calloutN)
			r.registerFold(id, r.foldState(id, c.Fold == md.FoldClosed), c.StartLine, KindCallout, -1)
		}
		return true
	})
}

// ctx carries inherited layout state down the block tree.
type ctx struct {
	base  *attr // inherited text style (quotes: italic muted)
	depth int   // list depth for bullet choice
	quote bool
}

func (c ctx) attr(r *renderer) attr {
	if c.base != nil {
		return *c.base
	}
	return r.body()
}

// blocks renders a sequence of child blocks separated by blank rows (none in
// tight lists).
func (r *renderer) blocks(bs []md.Block, w int, c ctx, tight bool) []row {
	var rows []row
	first := true
	for _, b := range bs {
		if skipped(b) {
			continue
		}
		br := r.block(b, w, c)
		if len(br) == 0 {
			continue
		}
		if !first && !tight {
			rows = append(rows, blankRow())
		}
		first = false
		rows = append(rows, br...)
	}
	return rows
}

// block renders one block into rows no wider than w (tables may overflow).
func (r *renderer) block(b md.Block, w int, c ctx) []row {
	switch b := b.(type) {
	case *md.Paragraph:
		if rows, ok := r.embedParagraph(b, w, c); ok {
			return rows
		}
		if isPlaceholder(b.Inlines) {
			// "▣ alt  file" alone: continuation lines hang under the text.
			spans := r.inlines(b.Inlines, c.attr(r), nil)
			gw := text.Width(r.g.Image) + 1
			rows := r.wrapRows(spans, w, w-gw, b.StartLine, b.EndLine, KindParagraph, b.ID)
			if len(rows) > 1 && !rows[0].rtl {
				rows = append(rows[:1], prefix(rows[1:], nil, []sp{mk(spaces(gw), theme.Style{})}, false, w)...)
			}
			return rows
		}
		return r.paragraph(b.Inlines, c.attr(r), w, b.StartLine, b.EndLine, KindParagraph, b.ID)
	case *md.Heading:
		return r.heading(b, w, c, "", false, 0)
	case *md.ThematicBreak:
		return r.rule(b, w)
	case *md.BlockQuote:
		return r.quote(b, w, c)
	case *md.Callout:
		return r.callout(b, w, c)
	case *md.List:
		return r.list(b, w, c)
	case *md.ListItem:
		return r.blocks(b.Children, w, c, true)
	case *md.CodeBlock:
		return r.code(b, w)
	case *md.MathBlock:
		return r.mathBlock(b, w)
	case *md.HTMLBlock:
		return r.htmlBlock(b, w)
	case *md.Table:
		return r.table(b, w, c)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Leaves

// isPlaceholder reports a paragraph holding only one image or embed.
func isPlaceholder(inl []md.Inline) bool {
	n := 0
	for _, x := range inl {
		switch x := x.(type) {
		case *md.Image:
			n++
		case *md.WikiLink:
			if !x.Embed {
				return false
			}
			n++
		case *md.Text:
			if strings.TrimSpace(x.Value) != "" {
				return false
			}
		case *md.SoftBreak, *md.Comment:
		default:
			return false
		}
	}
	return n == 1
}

// paragraph wraps inlines to w and finishes the lines (alignment, bidi).
func (r *renderer) paragraph(inl []md.Inline, a attr, w, src0, src1 int, k Kind, id string) []row {
	spans := r.inlines(inl, a, nil)
	return r.wrapRows(spans, w, w, src0, src1, k, id)
}

// wrapRows wraps spans (first line first cells, the rest rest cells) into
// finished rows.
func (r *renderer) wrapRows(spans []sp, first, rest, src0, src1 int, k Kind, id string) []row {
	dir := spanDirection(spans)
	lines := text.WrapSpans(spans, first, rest)
	rows := make([]row, 0, len(lines))
	for i, ln := range lines {
		w := rest
		if i == 0 {
			w = first
		}
		ln = trimSpaces(ln)
		if i > 0 {
			ln = trimLeading(ln)
		}
		s0, s1 := spanLines(ln, src0, src1)
		rows = append(rows, row{spans: r.finishLine(ln, dir, w), src0: s0, src1: s1, kind: k,
			blockID: id, rtl: dir == text.RTL})
	}
	return rows
}

// spanLines returns the source line range covered by a wrapped line's spans,
// falling back to the block's range.
func spanLines(ln []sp, src0, src1 int) (int, int) {
	lo, hi := -1, -1
	for _, s := range ln {
		if s.Style.line < 0 {
			continue
		}
		l := int(s.Style.line)
		if lo < 0 || l < lo {
			lo = l
		}
		if l > hi {
			hi = l
		}
	}
	if lo < 0 {
		return src0, src1
	}
	return lo, hi
}

// trimSpaces drops trailing spaces of a wrapped line (leading spaces after a
// hard break are kept as the wrapper keeps them).
func trimSpaces(ln []sp) []sp {
	for len(ln) > 0 {
		last := &ln[len(ln)-1]
		t := strings.TrimRight(last.Text, " ")
		if t != "" {
			if t != last.Text {
				ln = append(ln[:len(ln)-1:len(ln)-1], sp{Text: t, Style: last.Style})
			}
			return ln
		}
		ln = ln[:len(ln)-1]
	}
	return ln
}

// trimLeading drops leading spaces of a continuation line (the wrapper
// keeps spaces after a hard break, which <br> in running text produces).
func trimLeading(ln []sp) []sp {
	for len(ln) > 0 {
		t := strings.TrimLeft(ln[0].Text, " ")
		if t != "" {
			if t != ln[0].Text {
				ln = append([]sp{{Text: t, Style: ln[0].Style}}, ln[1:]...)
			}
			return ln
		}
		ln = ln[1:]
	}
	return ln
}

func spanDirection(spans []sp) text.Direction {
	for _, s := range spans {
		if d := text.BaseDirection(s.Text); d != text.Neutral {
			return d
		}
	}
	return text.LTR
}

func spansW(spans []sp) int {
	w := 0
	for _, s := range spans {
		w += Width(s.Text)
	}
	return w
}

// pad returns spans padded with spaces (in style st) to w cells.
func pad(spans []sp, w int, st theme.Style) []sp {
	if n := w - spansW(spans); n > 0 {
		return append(spans, mk(spaces(n), st))
	}
	return spans
}

// heading renders a heading. id is its fold id ("" for headings that do not
// fold); hidden is the number of source lines a fold hides.
func (r *renderer) heading(h *md.Heading, w int, c ctx, id string, folded bool, hidden int) []row {
	a := r.body()
	switch {
	case h.Level <= 2:
		a.st = theme.Style{FG: r.t.Heading, Attrs: theme.Bold}
	case h.Level == 3:
		a.st = theme.Style{FG: r.t.Text, Attrs: theme.Bold}
	default:
		a.st = theme.Style{FG: r.t.Muted, Attrs: theme.Italic}
	}
	spans := r.inlines(h.Inlines, a, nil)
	if folded && hidden > 0 {
		label := r.g.Ellipsis + " " + plural(hidden, "line")
		spans = append(spans, mk(nbsp+nbsp, a.st.Without(theme.Bold|theme.Italic)), mk(strings.ReplaceAll(label, " ", nbsp), r.muted()))
	}
	rows := r.wrapRows(spans, w, w, h.StartLine, h.EndLine, KindHeading, h.ID)
	for i := range rows {
		rows[i].level = h.Level
	}
	if id != "" && len(rows) > 0 {
		glyph := r.g.FoldOpen
		if folded {
			glyph = r.g.FoldClosed
		}
		hit := r.addHit(pendingHit{kind: HitFold, fold: id})
		rows[0].gutter = []sp{{Text: glyph, Style: attr{st: r.faint(), hit: hit, line: -1}}}
		rows[0].gutterRight = rows[0].rtl
		rows[0].fold = id
		rows[0].folded = folded
	}
	if h.Level == 1 {
		rule := row{spans: []sp{mk(strings.Repeat(r.g.Rule, w/max(1, text.Width(r.g.Rule))), r.hair())},
			src0: h.EndLine, src1: h.EndLine, kind: KindHeading, level: 1}
		rows = append(rows, rule)
	}
	return rows
}

func plural(n int, word string) string {
	s := strconv.Itoa(n) + " " + word
	if n != 1 {
		s += "s"
	}
	return s
}

// rule renders a thematic break as a centred ornament.
func (r *renderer) rule(b *md.ThematicBreak, w int) []row {
	parts := strings.Fields(r.g.Ornament)
	var spans []sp
	for i, p := range parts {
		if i > 0 {
			spans = append(spans, mk("  ", theme.Style{}))
		}
		st := r.faint()
		if i == len(parts)/2 && len(parts)%2 == 1 {
			st = r.accent()
		}
		spans = append(spans, mk(p, st))
	}
	ow := spansW(spans)
	if ow > w {
		spans = []sp{mk(text.Truncate(r.g.Ornament, w, ""), r.faint())}
		ow = spansW(spans)
	}
	lead := (w - ow) / 2
	spans = append([]sp{mk(spaces(lead), theme.Style{})}, spans...)
	return []row{{spans: spans, src0: b.StartLine, src1: b.EndLine, kind: KindRule, blockID: b.ID}}
}

// mathBlock shows display math verbatim in the math ink: centred when
// every line fits, else indented two cells and wrapped.
func (r *renderer) mathBlock(b *md.MathBlock, w int) []row {
	st := theme.Style{FG: r.t.Math}
	if r.t.Math.IsDefault() {
		st = theme.Style{Attrs: theme.Italic}
	}
	lines := make([]string, len(b.Lines))
	for i, l := range b.Lines {
		lines[i] = cleanCode(l)
	}
	// Leading and trailing empty lines are dropped (src keeps the mapping).
	first := b.FirstLine
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
		first++
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	maxw := 0
	for _, l := range lines {
		maxw = max(maxw, text.Width(l))
	}
	indent := 2
	if maxw+4 <= w {
		indent = (w - maxw) / 2
	}
	rows := r.verbatim(lines, first, w, indent, st, KindMath, b.ID)
	if len(rows) == 0 {
		rows = append(rows, row{src0: b.StartLine, src1: b.EndLine, kind: KindMath})
	}
	rows[0].src0 = b.StartLine
	rows[len(rows)-1].src1 = b.EndLine
	return rows
}

// verbatim lays out raw lines indented by indent cells, wrapping long ones
// with a further two-cell hanging indent; first is the source line of
// lines[0].
func (r *renderer) verbatim(lines []string, first, w, indent int, st theme.Style, k Kind, id string) []row {
	var rows []row
	lead := mk(spaces(indent), theme.Style{})
	for i, l := range lines {
		src := first + i
		hang := 2
		if w-indent-hang < 8 {
			hang = 0
		}
		for j, part := range text.WrapWidths(l, max(1, w-indent), max(1, w-indent-hang)) {
			spans := []sp{lead}
			if j > 0 && hang > 0 {
				spans = append(spans, mk(spaces(hang), theme.Style{}))
			}
			spans = append(spans, mk(part, st))
			rows = append(rows, row{spans: trimSpaces(spans), src0: src, src1: src, kind: k, blockID: id})
		}
	}
	return rows
}

// htmlBlock shows raw HTML verbatim and faint, wrapped to the measure.
func (r *renderer) htmlBlock(b *md.HTMLBlock, w int) []row {
	lines := stripHTMLComments(b.Lines)
	var rows []row
	for i, l := range lines {
		if l == nil {
			continue
		}
		rows = append(rows, r.verbatim([]string{cleanCode(*l)}, b.StartLine+i, w, 0, r.faint(), KindHTML, b.ID)...)
	}
	return rows
}

// stripHTMLComments removes <!-- … --> comments (which may span lines)
// from raw HTML lines. A line left blank by the removal becomes nil so it
// is not shown at all; other lines keep their remaining text.
func stripHTMLComments(lines []string) []*string {
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
				rest = rest[j+3:]
				in = false
				continue
			}
			j := strings.Index(rest, "<!--")
			if j < 0 {
				b.WriteString(rest)
				break
			}
			had = true
			b.WriteString(rest[:j])
			rest = rest[j+4:]
			in = true
		}
		s := b.String()
		if had && strings.TrimSpace(s) == "" {
			continue
		}
		if had {
			s = strings.TrimLeft(s, " ")
		}
		out[i] = &s
	}
	return out
}

// ---------------------------------------------------------------------------
// Containers

// prefix puts first before the first row and rest before the others; for an
// RTL container the (mirrored) prefixes go on the right edge of a w-cell box.
// Blank rows get the prefix with trailing spaces trimmed.
func prefix(rows []row, first, rest []sp, rtl bool, w int) []row {
	pw := max(spansW(first), spansW(rest))
	for i := range rows {
		p := rest
		if i == 0 {
			p = first
		}
		rw := &rows[i]
		if rtl {
			body := pad(rw.spans, w-pw, theme.Style{})
			if rw.blank && isSpace(p) {
				continue
			}
			rw.spans = append(body, mirrorPrefix(p)...)
			if !isSpace(p) {
				rw.blank = false
			}
			continue
		}
		if len(rw.spans) == 0 {
			p = trimSpaces(append([]sp(nil), p...))
		}
		if rw.blank && !isSpace(p) {
			rw.blank = false // a quote bar keeps the row part of its block
		}
		rw.spans = append(append([]sp(nil), p...), rw.spans...)
		if rw.code != nil {
			rw.code.x += spansW(p)
		}
	}
	return rows
}

func isSpace(spans []sp) bool {
	for _, s := range spans {
		if strings.TrimSpace(s.Text) != "" {
			return false
		}
	}
	return true
}

// mirrorPrefix turns a left prefix ("• ", "12. ", "▎ ") into its right-edge
// mirror (" •", " .12", " ▎"): spans reversed, each reordered as RTL text.
func mirrorPrefix(p []sp) []sp {
	out := make([]sp, 0, len(p))
	for i := len(p) - 1; i >= 0; i-- {
		s := p[i]
		s.Text = text.ReorderString(s.Text, text.RTL)
		out = append(out, s)
	}
	return out
}

// blocksDirection is the direction of the first strong text in blocks.
func blocksDirection(bs []md.Block) text.Direction {
	d := text.Neutral
	md.WalkBlocks(bs, func(b md.Block) bool {
		if d != text.Neutral {
			return false
		}
		switch b := b.(type) {
		case *md.Paragraph:
			d = text.BaseDirection(md.PlainText(b.Inlines))
		case *md.Heading:
			d = text.BaseDirection(b.Text)
		case *md.CodeBlock, *md.Table, *md.MathBlock, *md.HTMLBlock:
			d = text.LTR
		}
		return d == text.Neutral
	})
	if d == text.Neutral {
		return text.LTR
	}
	return d
}

func (r *renderer) quote(b *md.BlockQuote, w int, c ctx) []row {
	bar := mk(r.g.Bar+" ", r.accent())
	bw := spansW([]sp{bar})
	qa := c.attr(r)
	qa.st = theme.Style{FG: r.t.Muted, Attrs: qa.st.Attrs | theme.Italic}
	if r.t.Muted.IsDefault() {
		qa.st.FG = theme.Default
	}
	cc := c
	cc.base = &qa
	cc.quote = true
	rows := r.blocks(b.Children, w-bw, cc, false)
	if len(rows) == 0 {
		rows = []row{{src0: b.StartLine, src1: b.EndLine}}
	}
	for i := range rows {
		if rows[i].kind == KindBlank || rows[i].kind == KindParagraph {
			rows[i].kind = KindQuote
		}
		if rows[i].src0 < 0 {
			rows[i].src0, rows[i].src1 = b.StartLine, b.EndLine
		}
	}
	rtl := blocksDirection(b.Children) == text.RTL
	return prefix(rows, []sp{bar}, []sp{bar}, rtl, w)
}

func (r *renderer) callout(b *md.Callout, w int, c ctx) []row {
	r.calloutN++
	id := "c:" + strconv.Itoa(r.calloutN)
	foldable := b.Fold != md.FoldNone
	folded := false
	if foldable {
		folded = r.foldState(id, b.Fold == md.FoldClosed)
		r.registerFold(id, folded, b.StartLine, KindCallout, -1)
	}
	hue := r.t.CalloutColor(b.Kind)
	hueSt := theme.Style{FG: hue}
	if hue.IsDefault() {
		hueSt = theme.Style{Attrs: theme.Bold}
	}
	bar := mk(r.g.Bar+" ", hueSt)
	bw := spansW([]sp{bar})
	inner := w - bw

	// Label line: glyph, title (or the capitalised kind), chevron.
	ta := r.body()
	ta.st = theme.Style{FG: hueSt.FG, Attrs: theme.Bold}
	var title []sp
	glyph := r.g.Callout(b.Kind)
	var glyphHit int32
	if foldable {
		glyphHit = r.addHit(pendingHit{kind: HitFold, fold: id})
	}
	title = append(title, sp{Text: glyph + nbsp, Style: attr{st: ta.st, hit: glyphHit, line: int32(b.StartLine)}})
	if b.Title != nil {
		title = r.inlines(b.Title, ta, title)
	} else {
		k := b.Kind
		if k == "" {
			k = "note"
		}
		title = append(title, sp{Text: strings.ToUpper(k[:1]) + k[1:], Style: attr{st: ta.st, line: int32(b.StartLine)}})
	}
	if foldable {
		ch := r.g.FoldOpen
		if folded {
			ch = r.g.FoldClosed
		}
		hit := r.addHit(pendingHit{kind: HitFold, fold: id})
		title = append(title, mk(nbsp, ta.st), sp{Text: ch, Style: attr{st: r.faint(), hit: hit, line: -1}})
	}
	rows := r.wrapRows(title, inner, inner, b.StartLine, b.StartLine, KindCallout, b.ID)
	if foldable && len(rows) > 0 {
		rows[0].fold = id
		rows[0].folded = folded
	}
	if !folded {
		ba := r.body()
		if c.quote {
			ba = c.attr(r)
		}
		cc := ctx{base: &ba, depth: c.depth}
		body := r.blocks(b.Children, inner, cc, false)
		for i := range body {
			if body[i].kind == KindParagraph || body[i].kind == KindBlank {
				body[i].kind = KindCallout
			}
			if body[i].src0 < 0 {
				body[i].src0, body[i].src1 = b.StartLine, b.EndLine
			}
		}
		rows = append(rows, body...)
	}
	rtl := false
	if b.Title != nil {
		rtl = text.BaseDirection(md.PlainText(b.Title)) == text.RTL
	} else {
		rtl = blocksDirection(b.Children) == text.RTL
	}
	return prefix(rows, []sp{bar}, []sp{bar}, rtl, w)
}

func (r *renderer) list(l *md.List, w int, c ctx) []row {
	// Ordered numbers are right-aligned to the widest number.
	numW := 0
	if l.Ordered {
		for i := range l.Items {
			numW = max(numW, len(strconv.Itoa(l.Start+i)))
		}
	}
	bullet := r.g.Bullets[min(c.depth, len(r.g.Bullets)-1)]
	delim := "."
	if l.Marker == ')' {
		delim = ")"
	}
	var rows []row
	for i, it := range l.Items {
		if i > 0 && !l.Tight {
			rows = append(rows, blankRow())
		}
		var marker []sp
		if l.Ordered {
			num := strconv.Itoa(l.Start + i)
			marker = append(marker, mk(spaces(numW-len(num))+num+delim, r.accent()))
		} else if it.Task == nil {
			marker = append(marker, mk(bullet, r.accent()))
		}
		textA := c.attr(r)
		kind := KindListItem
		if it.Task != nil {
			kind = KindTask
			box, st, ta := r.taskBox(it.Task, textA)
			textA = ta
			hit := r.addHit(pendingHit{kind: HitTask, task: TaskRef{Line: it.Task.Line, Col: it.Task.Col,
				Offset: it.Task.Offset, Width: it.Task.Width, State: it.Task.State}})
			if len(marker) > 0 {
				marker = append(marker, mk(" ", theme.Style{}))
			}
			marker = append(marker, sp{Text: box, Style: attr{st: st, hit: hit, line: int32(it.Task.Line)}})
		}
		marker = append(marker, mk(" ", theme.Style{}))
		mw := spansW(marker)
		cc := c
		cc.depth = c.depth + 1
		cc.base = &textA
		var body []row
		if it.Task != nil {
			body = r.taskBody(it, w-mw, cc, l.Tight)
		} else {
			body = r.blocks(it.Children, w-mw, cc, l.Tight)
		}
		if len(body) == 0 {
			body = []row{{src0: it.StartLine, src1: it.EndLine}}
		}
		for j := range body {
			if body[j].kind == KindParagraph || (j == 0 && body[j].kind == 0) {
				body[j].kind = kind
			}
			if body[j].src0 < 0 && !body[j].blank {
				body[j].src0, body[j].src1 = it.StartLine, it.EndLine
			}
			if body[j].blockID == "" && it.ID != "" && j == 0 {
				body[j].blockID = it.ID
			}
		}
		body[0].src0 = min(body[0].src0, it.StartLine)
		if body[0].src0 < 0 {
			body[0].src0 = it.StartLine
		}
		rest := []sp{mk(spaces(mw), theme.Style{})}
		rtl := blocksDirection(it.Children) == text.RTL
		rows = append(rows, prefix(body, marker, rest, rtl, w)...)
	}
	return rows
}
