package render

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
)

// Span is a run of text in one style: the unit lines are made of. It is the
// span type internal/term's Encoder.AppendSpans and Screen.PutSpans take.
type Span = text.Span[theme.Style]

// DefaultMeasure is the page measure used when Options.Measure is 0
// (DESIGN.md §5 and §9).
const DefaultMeasure = 78

// Margin is the minimum number of cells kept free on each side of the
// measure. The left margin holds the reader's cursor bar (column Left−3) and
// the fold chevrons (column Left−2).
const Margin = 3

// EmbedLines is the number of rendered lines of a note embed shown in the
// page (DESIGN.md §5: "the first 12 lines of the target").
const EmbedLines = 12

// Options controls one rendering. Only Width is required.
type Options struct {
	// Width is the pane width in cells (the whole area the page is drawn
	// in, margins included). Values below 20 are treated as 20.
	Width int
	// Measure is the maximum text measure (0 = DefaultMeasure). The
	// effective measure is min(Measure, Width − 2·Margin).
	Measure int
	// Theme supplies the colours. Pass the variant for the output profile
	// (term.ThemeFor): styles are authored in the theme's colours and the
	// serialiser downsamples them. A theme whose Raised colour is the
	// default (Basic16, Mono) gets the unpainted forms: code blocks framed by
	// a faint bar, inline code in backticks.
	Theme theme.Theme
	// Glyphs is the glyph set; the zero value means theme.UnicodeGlyphs.
	Glyphs theme.Glyphs
	// Bidi enables the display transform for right-to-left text: Arabic
	// shaping and bidi reordering of every line (DESIGN.md §6 bidi=on).
	// Leave it off for pipes and terminals that do bidi themselves; RTL
	// blocks are right-aligned either way.
	Bidi bool
	// Folds overrides fold states by fold id: true folded, false open. Ids
	// missing from the map take their default: headings open, callouts as
	// written ("[!x]-" folded), the properties line folded. See Page.Folds
	// for the ids present in a page.
	Folds map[string]bool
	// Resolver resolves links and supplies embed content. With a nil
	// Resolver every internal link renders as resolved and note embeds as
	// placeholders.
	Resolver Resolver
	// Today is the date overdue tasks are judged against (zero = now).
	Today time.Time
	// PaintGround pads every line to Width and paints the theme's ground
	// behind all text (DESIGN.md §6 ground=on). When false the terminal's
	// own background shows through and lines are not padded.
	PaintGround bool
	// Title is the fallback title (normally the file name without .md),
	// used when the note has frontmatter but neither a title field nor a
	// leading H1.
	Title string
	// Backlinks is the number of notes linking here, shown as a chip in
	// the title block when positive.
	Backlinks int
	// NoTitleBlock suppresses the title block (used for embed previews).
	NoTitleBlock bool
}

// Resolver connects the renderer to the vault without importing it.
type Resolver interface {
	// Resolve reports the vault-relative path a link points to. Wiki is
	// true for [[wikilinks]] and embeds, false for Markdown links to
	// relative paths. ok false means the link is broken.
	Resolve(q LinkQuery) (path string, ok bool)
	// Embed returns the title and raw Markdown source of the resolved note
	// path, for ![[Note]] embeds. ok false renders a placeholder.
	Embed(path string) (title, source string, ok bool)
}

// LinkQuery is a link as written, passed to Resolver.Resolve.
type LinkQuery struct {
	// Target is the note or file as written ("" for a same-note anchor,
	// which the renderer resolves itself and never passes on).
	Target string
	// Heading and Block are the fragment ("Heading" or block id).
	Heading, Block string
	// Wiki distinguishes [[wikilinks]] from Markdown links.
	Wiki bool
	// Embed is true for ![[embeds]].
	Embed bool
}

// LinkStatus classifies a link for styling and navigation.
type LinkStatus uint8

// Link statuses.
const (
	LinkResolved LinkStatus = iota // an internal link to an existing note, file or heading
	LinkBroken                     // an internal link whose target does not exist
	LinkExternal                   // a URL with a scheme (https:, mailto:, …)
)

// String returns "resolved", "broken" or "external".
func (s LinkStatus) String() string {
	switch s {
	case LinkBroken:
		return "broken"
	case LinkExternal:
		return "external"
	}
	return "resolved"
}

// LinkKind tells how a link was written.
type LinkKind uint8

// Link kinds.
const (
	LinkWiki     LinkKind = iota // [[Note]] / [[Note|label]] / [[#Heading]]
	LinkMarkdown                 // [text](dest), [text][ref], autolinks
	LinkURL                      // a bare URL in running text
	LinkEmbed                    // ![[Note]] or ![[file.png]]
	LinkImage                    // ![alt](src)
)

// Link is one navigable link of the page, in document order.
type Link struct {
	Kind   LinkKind
	Status LinkStatus
	// Text is the label as displayed (before shortening of bare URLs).
	Text string
	// Target is the note or path as written ("" for same-note anchors);
	// Heading and Block hold the fragment.
	Target, Heading, Block string
	// Path is the resolved vault-relative path (resolved internal links).
	Path string
	// URL is the destination of external links.
	URL string
	// SrcLine and SrcCol are the 0-based source line and byte column of
	// the link's first byte; Offset is that byte's offset in the source.
	SrcLine, SrcCol, Offset int
	// Line is the index of the first rendered line showing the link;
	// Regions lists every piece of it (a link may wrap over lines).
	Line    int
	Regions []Region
}

// Region is a horizontal range [X0, X1) of cells on rendered line Line.
type Region struct {
	Line, X0, X1 int
}

// Kind is the kind of block a rendered line belongs to.
type Kind uint8

// Line kinds.
const (
	KindBlank      Kind = iota // spacing between blocks
	KindTitle                  // title block (title, chips, hairline)
	KindProperties             // the folded/open properties lines
	KindHeading
	KindParagraph
	KindListItem
	KindTask
	KindQuote
	KindCallout
	KindCode
	KindTable
	KindRule
	KindMath
	KindHTML
	KindEmbed
	KindFootnote
)

var kindNames = [...]string{"blank", "title", "properties", "heading", "paragraph",
	"list", "task", "quote", "callout", "code", "table", "rule", "math", "html", "embed", "footnote"}

// String returns a short lowercase name of the kind.
func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "unknown"
}

// HitKind is the kind of an interactive region.
type HitKind uint8

// Hit kinds.
const (
	HitLink     HitKind = iota + 1 // Hit.Link indexes Page.Links
	HitTask                        // Hit.Task locates the task's state character
	HitFold                        // Hit.Fold is the fold id
	HitFootnote                    // Hit.Footnote is the footnote number
)

// Hit is an interactive region of a rendered line.
type Hit struct {
	// X0 and X1 bound the region: cells [X0, X1) of the line.
	X0, X1 int
	Kind   HitKind
	// Link is the index into Page.Links (HitLink).
	Link int
	// Task locates the task box in the source (HitTask).
	Task TaskRef
	// Fold is the fold id (HitFold).
	Fold string
	// Footnote is the 1-based footnote number (HitFootnote); the
	// definition is at Page.Footnotes[Footnote].
	Footnote int
}

// TaskRef locates a task's state character in the source: toggling means
// rewriting Source[Offset:Offset+Width]. Line and Col are 0-based.
type TaskRef struct {
	Line, Col, Offset, Width int
	State                    rune
}

// Line is one rendered line of the page, in final visual order: spans are
// laid out from column 0 of the pane, margins and indentation included.
type Line struct {
	Spans []Span
	// Width is the cell width of Spans (≤ the pane width).
	Width int
	// SrcStart and SrcEnd are the 0-based inclusive source lines the
	// line came from; -1 for lines with no source (spacing, ornaments of
	// the title block are mapped to the frontmatter).
	SrcStart, SrcEnd int
	Kind             Kind
	// BlockID is the Obsidian block id of the line's block, if any.
	BlockID string
	// Level is the heading level (1–6) of heading lines, else 0.
	Level int
	// Fold is the fold id when the line carries a chevron; Folded is its
	// state.
	Fold   string
	Folded bool
	// Hits are the line's interactive regions, left to right.
	Hits []Hit
	// RTL marks lines of right-to-left blocks.
	RTL bool
	// Code is set on code block content lines: the reader scrolls them
	// horizontally with Scrolled.
	Code *CodeView
}

// CodeView describes the horizontally scrollable viewport of a code line.
type CodeView struct {
	// X is the column where the viewport starts and View its width.
	X, View int
	// Content is the whole highlighted source line.
	Content []Span
	// Width is the cell width of Content.
	Width int
	// Ellipsis is the clip mark and EllipsisStyle its style.
	Ellipsis      string
	EllipsisStyle theme.Style
}

// MaxScroll is the largest useful horizontal offset of the code line.
func (c *CodeView) MaxScroll() int {
	if c == nil || c.Width <= c.View {
		return 0
	}
	return c.Width - c.View + Width(c.Ellipsis)
}

// Width returns the cell width of s (text.Width, memoised for the short
// strings — glyphs, markers, prefixes — the renderer measures over and
// over).
func Width(s string) int {
	if len(s) > 24 {
		return text.Width(s)
	}
	ascii := true
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 0x80 || c < 0x20 || c == 0x7f {
			ascii = false
			break
		}
	}
	if ascii {
		return len(s)
	}
	if w, ok := widthCache.Load(s); ok {
		return w.(int)
	}
	w := text.Width(s)
	if widthCacheN.Add(1) < 1<<14 {
		widthCache.Store(s, w)
	}
	return w
}

var (
	widthCache  sync.Map // string → int
	widthCacheN atomic.Int64
)

// spansWidth is text.SpansWidth with the memoised Width.
func spansWidth(spans []Span) int {
	w := 0
	for _, s := range spans {
		w += Width(s.Text)
	}
	return w
}

// Scrolled returns the line with its code viewport showing Content from
// cell off onwards, clip marks on whichever sides are cut. Lines without a
// code viewport, and off ≤ 0, return the line as rendered.
func (l Line) Scrolled(off int) Line {
	c := l.Code
	if c == nil || off <= 0 {
		return l
	}
	if m := c.MaxScroll(); off > m {
		off = m
	}
	if off <= 0 {
		return l
	}
	before := sliceSpans(l.Spans, 0, c.X)
	after := sliceSpans(l.Spans, c.X+c.View, l.Width)
	view := clipView(c.Content, c.Width, off, c.View, c.Ellipsis, c.EllipsisStyle)
	out := l
	out.Spans = mergeSpans(append(append(before, view...), after...))
	out.Hits = nil
	return out
}

// clipView cuts content to [off, off+view) cells, marking cut edges.
func clipView(content []Span, width, off, view int, ell string, ellSt theme.Style) []Span {
	ew := text.Width(ell)
	if ew >= view {
		ell, ew = "", 0
	}
	var out []Span
	left := off > 0 && ew > 0
	from := off
	avail := view
	if left {
		out = append(out, Span{Text: ell, Style: ellSt})
		from += ew
		avail -= ew
	}
	right := from+avail < width && ew > 0
	if right {
		avail -= ew
	}
	body := sliceSpans(content, from, from+avail)
	out = append(out, body...)
	if w := text.SpansWidth(body); w < avail {
		st := ellSt
		if len(content) > 0 {
			st = content[0].Style
		}
		st.Attrs = 0
		st.FG = theme.Default
		out = append(out, Span{Text: strings.Repeat(" ", avail-w), Style: st})
	}
	if right {
		out = append(out, Span{Text: ell, Style: ellSt})
	}
	return out
}

// sliceSpans returns the cells [from, to) of spans; wide graphemes cut by an
// edge become spaces.
func sliceSpans(spans []Span, from, to int) []Span {
	var out []Span
	col := 0
	for _, sp := range spans {
		w := text.Width(sp.Text)
		end := col + w
		if end > from && col < to {
			lo, hi := max(from, col)-col, min(to, end)-col
			if lo == 0 && hi == w {
				out = append(out, sp)
			} else if t := text.Slice(sp.Text, lo, hi); t != "" {
				out = append(out, Span{Text: t, Style: sp.Style})
			}
		}
		col = end
		if col >= to {
			break
		}
	}
	return out
}

// mergeSpans joins adjacent spans of equal style and drops empty ones, in
// place (spans is overwritten). Each merged run is built with a single
// allocation.
func mergeSpans(spans []Span) []Span {
	out := spans[:0]
	for i := 0; i < len(spans); {
		if spans[i].Text == "" {
			i++
			continue
		}
		j, n := i+1, len(spans[i].Text)
		for j < len(spans) && (spans[j].Text == "" || spans[j].Style == spans[i].Style) {
			n += len(spans[j].Text)
			j++
		}
		if j == i+1 {
			out = append(out, spans[i])
		} else {
			var b strings.Builder
			b.Grow(n)
			for _, s := range spans[i:j] {
				b.WriteString(s.Text)
			}
			out = append(out, Span{Text: b.String(), Style: spans[i].Style})
		}
		i = j
	}
	return out
}

// Text returns the line's characters without styling.
func (l Line) Text() string { return text.SpansText(l.Spans) }

// HitAt returns the hit region containing column x, if any.
func (l Line) HitAt(x int) (Hit, bool) {
	for _, h := range l.Hits {
		if x >= h.X0 && x < h.X1 {
			return h, true
		}
	}
	return Hit{}, false
}

// OutlineEntry is one heading of the page outline.
type OutlineEntry struct {
	Level int
	Text  string
	Slug  string
	// SrcLine is the heading's source line; Line its rendered line (for
	// a heading hidden by a fold, the line of the folded section that
	// hides it), Visible false in that case.
	SrcLine, Line int
	Visible       bool
	// Fold is the heading's fold id ("" for headings that cannot fold,
	// such as those nested in quotes or lists).
	Fold string
}

// FoldInfo is one foldable element of the page.
type FoldInfo struct {
	ID     string
	Folded bool
	// Line is the rendered line carrying the chevron (-1 when the element
	// itself is hidden inside another fold).
	Line int
	// SrcLine is the element's first source line.
	SrcLine int
	// Kind is KindHeading, KindCallout or KindProperties.
	Kind Kind
}

// Page is a rendered note.
type Page struct {
	Lines []Line
	// Links lists every link shown on the page in document order.
	Links []Link
	// Outline lists every heading in document order.
	Outline []OutlineEntry
	// Folds lists every foldable element in document order.
	Folds []FoldInfo
	// Footnotes maps a footnote number to the rendered line of its
	// definition.
	Footnotes map[int]int
	// Width is the pane width, Measure the text measure and Left the
	// column where the measure starts. CursorX is the column for the
	// reader's cursor bar and ChevronX the fold chevrons' column.
	Width, Measure, Left, CursorX, ChevronX int

	// srcFirst[s] is the first rendered line for source line s.
	srcFirst []int
}

// LineForSource returns the first rendered line showing source line src
// (0-based). Source lines with no rendering of their own (blank lines,
// hidden comments, folded sections) map to the nearest rendered line: the
// next block, or the fold that hides them.
func (p *Page) LineForSource(src int) int {
	if len(p.Lines) == 0 {
		return 0
	}
	if len(p.srcFirst) == 0 {
		return 0
	}
	if src < 0 {
		src = 0
	}
	if src >= len(p.srcFirst) {
		src = len(p.srcFirst) - 1
	}
	return p.srcFirst[src]
}

// SourceLine returns the source line (0-based) rendered line i came from.
// Spacing lines report the block above them (or below, at the top).
func (p *Page) SourceLine(i int) int {
	if len(p.Lines) == 0 {
		return 0
	}
	if i < 0 {
		i = 0
	}
	if i >= len(p.Lines) {
		i = len(p.Lines) - 1
	}
	for j := i; j >= 0; j-- {
		if s := p.Lines[j].SrcStart; s >= 0 {
			if j < i && p.Lines[j].SrcEnd >= 0 {
				return p.Lines[j].SrcEnd
			}
			return s
		}
	}
	for j := i + 1; j < len(p.Lines); j++ {
		if s := p.Lines[j].SrcStart; s >= 0 {
			return s
		}
	}
	return 0
}

// LinkAt returns the index of the link covering column x of line i, or -1.
func (p *Page) LinkAt(i, x int) int {
	if i < 0 || i >= len(p.Lines) {
		return -1
	}
	for _, h := range p.Lines[i].Hits {
		if h.Kind == HitLink && x >= h.X0 && x < h.X1 {
			return h.Link
		}
	}
	return -1
}

// FocusSpans returns line i's spans with link number link drawn in style st
// (normally Theme.LinkFocus): the Tab-highlighted link.
func (p *Page) FocusSpans(i, link int, st theme.Style) []Span {
	if i < 0 || i >= len(p.Lines) {
		return nil
	}
	l := p.Lines[i]
	if link < 0 || link >= len(p.Links) {
		return l.Spans
	}
	var out []Span
	pos := 0
	for _, r := range p.Links[link].Regions {
		if r.Line != i {
			continue
		}
		out = append(out, sliceSpans(l.Spans, pos, r.X0)...)
		for _, sp := range sliceSpans(l.Spans, r.X0, r.X1) {
			sp.Style = theme.Style{FG: st.FG, BG: st.BG, Attrs: st.Attrs, Link: sp.Style.Link}
			out = append(out, sp)
		}
		pos = r.X1
	}
	if pos == 0 {
		return l.Spans
	}
	out = append(out, sliceSpans(l.Spans, pos, l.Width)...)
	return mergeSpans(out)
}
