package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
)

// styles are the composite styles the application draws with, derived once
// per theme change from the profile-adjusted theme.
type styles struct {
	base     theme.Style // page ink on the page ground
	muted    theme.Style
	faint    theme.Style
	accent   theme.Style
	heading  theme.Style
	danger   theme.Style
	panel    theme.Style // overlay / panel ink on the raised ground
	pMuted   theme.Style
	pFaint   theme.Style
	pAccent  theme.Style
	pDanger  theme.Style
	border   theme.Style // hairlines on the raised ground
	pageRule theme.Style // hairlines on the page ground (panel separators)
	sel      theme.Style // selected row ground
	selBar   theme.Style // the accent bar marking the selected row
	match    theme.Style // matched characters in fuzzy lists (fg only)
	status   theme.Style
	sMuted   theme.Style
	sFaint   theme.Style
	sAccent  theme.Style
	sDanger  theme.Style
	cursor   theme.Style // the reader's ▎ cursor bar
}

func makeStyles(th theme.Theme) styles {
	var s styles
	g, r := th.Ground, th.Raised
	if r.IsDefault() {
		r = g
	}
	s.base = theme.Style{FG: th.Text, BG: g}
	s.muted = theme.Style{FG: th.Muted, BG: g}
	s.faint = theme.Style{FG: th.Faint, BG: g}
	s.accent = theme.Style{FG: th.Accent, BG: g}
	s.heading = theme.Style{FG: th.Heading, BG: g, Attrs: theme.Bold}
	s.danger = theme.Style{FG: th.Danger, BG: g}
	s.panel = theme.Style{FG: th.Text, BG: r}
	s.pMuted = theme.Style{FG: th.Muted, BG: r}
	s.pFaint = theme.Style{FG: th.Faint, BG: r}
	s.pAccent = theme.Style{FG: th.Accent, BG: r}
	s.pDanger = theme.Style{FG: th.Danger, BG: r}
	s.border = theme.Style{FG: th.Border, BG: r}
	s.pageRule = theme.Style{FG: th.Border, BG: g}
	if th.Hover.IsDefault() {
		// 16 colours / monochrome: no painted grounds, so the selected row
		// is carried by attributes (DESIGN.md §6).
		s.sel = theme.Style{Attrs: theme.Bold | theme.Reverse}
		s.selBar = theme.Style{FG: th.Accent, Attrs: theme.Bold}
	} else {
		s.sel = theme.Style{FG: th.Text, BG: th.Hover}
		s.selBar = theme.Style{FG: th.Accent, BG: th.Hover}
	}
	s.match = theme.Style{FG: th.Accent, Attrs: theme.Bold}
	s.status = th.StatusBar
	if s.status.FG.IsDefault() {
		s.status.FG = th.Muted
	}
	sb := s.status.BG
	s.sMuted = theme.Style{FG: th.Muted, BG: sb}
	s.sFaint = theme.Style{FG: th.Faint, BG: sb}
	s.sAccent = theme.Style{FG: th.Accent, BG: sb}
	s.sDanger = theme.Style{FG: th.Danger, BG: sb, Attrs: theme.Bold}
	s.cursor = theme.Style{FG: th.Accent, BG: g, Attrs: theme.Bold}
	return s
}

// onSel returns st as it should look on the selected row: the selection
// ground replaces st's, keeping its ink when there is a ground to show it.
func (s styles) onSel(st theme.Style) theme.Style {
	if s.sel.BG.IsDefault() {
		st.Attrs |= s.sel.Attrs
		return st
	}
	st.BG = s.sel.BG
	return st
}

// disp prepares a short string (a title, a path) for display: with bidi on,
// right-to-left text is shaped and put in visual order (DESIGN.md §6). It
// reports whether the string was transformed, in which case byte positions
// into the original no longer apply.
func (a *app) disp(s string) (string, bool) {
	if !a.bidi || !text.HasRTL(s) {
		return s, false
	}
	return text.Visual(s, text.BaseDirection(s)), true
}

// dispRanges is disp for a string carrying highlighted byte ranges (search
// matches): the ranges are carried through shaping and reordering, so a
// match stays highlighted on a line that mixes Arabic and Latin text.
func (a *app) dispRanges(s string, ranges [][2]int) (string, [][2]int) {
	if !a.bidi || !text.HasRTL(s) {
		return s, ranges
	}
	return text.VisualRanges(s, ranges)
}

// dispS is disp without the flag.
func (a *app) dispS(s string) string {
	d, _ := a.disp(s)
	return d
}

// dispFit is dispS truncated to w cells. A right-to-left string is cut at
// its logical end before it is put in visual order, so the ellipsis lands
// at the visual left, where reading ends, and the start of a title stays.
func (a *app) dispFit(s string, w int) string {
	if text.HasRTL(s) && text.BaseDirection(s) == text.RTL {
		t := text.Truncate(s, w, a.gl.Ellipsis)
		if a.bidi {
			t = text.Visual(t, text.RTL)
		}
		return t
	}
	return text.Truncate(a.dispS(s), w, a.gl.Ellipsis)
}

// dispMsg is dispFit for a status message: an English sentence that may
// quote an Arabic title, laid out left to right.
func (a *app) dispMsg(s string, w int) string {
	t := text.Truncate(s, w, a.gl.Ellipsis)
	if a.bidi && text.HasRTL(t) {
		t = text.Visual(t, text.LTR)
	}
	return t
}

// dispRangesFit is dispRanges truncated to w cells, cut like dispFit.
func (a *app) dispRangesFit(s string, ranges [][2]int, w int) (string, [][2]int) {
	if text.Width(s) > w {
		t := text.Truncate(s, w, a.gl.Ellipsis)
		if text.HasRTL(s) && text.BaseDirection(s) == text.RTL {
			keep := len(t) - len(a.gl.Ellipsis)
			var rs [][2]int
			for _, r := range ranges {
				if r[0] < keep {
					rs = append(rs, [2]int{r[0], min(r[1], keep)})
				}
			}
			return a.dispRanges(t, rs)
		}
		s2, rs := a.dispRanges(s, ranges)
		return text.Truncate(s2, w, a.gl.Ellipsis), rs
	}
	return a.dispRanges(s, ranges)
}

// dispPath is dispS for a vault path: Arabic folder and note names are
// shaped and reordered, but the path reads left to right (folders first).
func (a *app) dispPath(p string) string {
	if !a.bidi || !text.HasRTL(p) {
		return p
	}
	return text.VisualPath(p)
}

// dimBehind fades everything drawn so far, so an overlay stands out from
// the page beneath it.
func (a *app) dimBehind() {
	faint := a.th.Faint
	a.scr.Restyle(term.Rect{X: 0, Y: 0, W: a.w, H: a.h}, func(o theme.Style) theme.Style {
		if faint.IsDefault() {
			o.Attrs |= theme.Faint
			o.Attrs &^= theme.Bold
			return o
		}
		if !o.FG.IsDefault() || o.BG.IsDefault() {
			o.FG = faint
		}
		o.Attrs &^= theme.Bold
		return o
	})
}

// --- one-line text input ----------------------------------------------------

// lineInput is a one-line editable field: the input row of overlays, the
// reader's ":" and "/" lines and prompts. pos is a byte offset on a grapheme
// boundary.
type lineInput struct {
	text string
	pos  int
}

func (in *lineInput) set(s string) { in.text, in.pos = s, len(s) }

func (in *lineInput) insert(s string) {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	in.text = in.text[:in.pos] + s + in.text[in.pos:]
	in.pos += len(s)
}

// prevBoundary returns the grapheme boundary before byte offset p.
func prevBoundary(s string, p int) int {
	last := 0
	for _, g := range text.Graphemes(s[:p]) {
		last = g.Offset
	}
	return last
}

// nextBoundary returns the grapheme boundary after byte offset p.
func nextBoundary(s string, p int) int {
	if p >= len(s) {
		return len(s)
	}
	gs := text.Graphemes(s[p:])
	if len(gs) == 0 {
		return len(s)
	}
	return p + len(gs[0].Text)
}

func isWordRune(r rune) bool {
	return r != ' ' && r != '/' && r != '-' && r != '.' && r != '_'
}

// handleKey applies an editing key. It reports whether the key was
// consumed and whether the text changed.
func (in *lineInput) handleKey(k term.KeyEvent) (handled, changed bool) {
	switch {
	case k.Key == term.KeyRune && k.Mod&(term.ModCtrl|term.ModAlt) == 0:
		in.insert(string(k.Rune))
		return true, true
	case k.Key == term.KeyBackspace && k.Mod&term.ModAlt != 0,
		k.Key == term.KeyRune && k.Mod == term.ModCtrl && k.Rune == 'w':
		if in.pos == 0 {
			return true, false
		}
		p := in.pos
		for p > 0 {
			r, n := utf8.DecodeLastRuneInString(in.text[:p])
			if isWordRune(r) {
				break
			}
			p -= n
		}
		for p > 0 {
			r, n := utf8.DecodeLastRuneInString(in.text[:p])
			if !isWordRune(r) {
				break
			}
			p -= n
		}
		in.text = in.text[:p] + in.text[in.pos:]
		in.pos = p
		return true, true
	case k.Key == term.KeyBackspace:
		if in.pos == 0 {
			return true, false
		}
		p := prevBoundary(in.text, in.pos)
		in.text = in.text[:p] + in.text[in.pos:]
		in.pos = p
		return true, true
	case k.Key == term.KeyDelete, k.Key == term.KeyRune && k.Mod == term.ModCtrl && k.Rune == 'd':
		if in.pos >= len(in.text) {
			return true, false
		}
		n := nextBoundary(in.text, in.pos)
		in.text = in.text[:in.pos] + in.text[n:]
		return true, true
	case k.Key == term.KeyLeft, k.Key == term.KeyRune && k.Mod == term.ModCtrl && k.Rune == 'b':
		if in.pos > 0 {
			in.pos = prevBoundary(in.text, in.pos)
		}
		return true, false
	case k.Key == term.KeyRight, k.Key == term.KeyRune && k.Mod == term.ModCtrl && k.Rune == 'f':
		in.pos = nextBoundary(in.text, in.pos)
		return true, false
	case k.Key == term.KeyHome, k.Key == term.KeyRune && k.Mod == term.ModCtrl && k.Rune == 'a':
		in.pos = 0
		return true, false
	case k.Key == term.KeyEnd, k.Key == term.KeyRune && k.Mod == term.ModCtrl && k.Rune == 'e':
		in.pos = len(in.text)
		return true, false
	case k.Key == term.KeyRune && k.Mod == term.ModCtrl && k.Rune == 'u':
		changed := in.pos > 0
		in.text = in.text[in.pos:]
		in.pos = 0
		return true, changed
	case k.Key == term.KeyRune && k.Mod == term.ModCtrl && k.Rune == 'k':
		changed := in.pos < len(in.text)
		in.text = in.text[:in.pos]
		return true, changed
	}
	return false, false
}

// draw draws the field in [x, x+w) of row y and returns the cursor column.
// When the text is wider than the field it scrolls so the cursor stays
// visible. With bidi, right-to-left text is shown shaped and in visual
// order (DESIGN.md §6) while editing stays logical; the cursor sits on the
// visual cell of the grapheme after it.
func (in *lineInput) draw(s *term.Screen, x, y, w int, st theme.Style, bidi bool) int {
	if w <= 0 {
		return x
	}
	if bidi && text.HasRTL(in.text) && text.Width(in.text) < w {
		return in.drawVisual(s, x, y, w, st)
	}
	before := text.Width(in.text[:in.pos])
	start := 0
	if before >= w {
		// Scroll: drop leading graphemes until the cursor fits.
		need := before - w + 1
		for _, g := range text.Graphemes(in.text) {
			if need <= 0 {
				break
			}
			need -= g.Width
			start = g.Offset + len(g.Text)
		}
	}
	shown := in.text[start:]
	s.PutStringClip(x, y, shown, st, x+w)
	return x + text.Width(in.text[start:in.pos])
}

// drawVisual is draw for a field holding right-to-left text that fits. The
// returned column is where the bar cursor goes: at the visual edge of the
// insertion point, which for right-to-left text is the right edge of the
// grapheme after the cursor.
func (in *lineInput) drawVisual(s *term.Screen, x, y, w int, st theme.Style) int {
	gs := text.Graphemes(in.text)
	cl := make([]string, len(gs))
	for i, g := range gs {
		cl[i] = g.Text
	}
	shaped := text.ShapeClusters(cl)
	bl := text.Reorder(cl, text.BaseDirection(in.text))
	col := make([]int, len(gs)) // visual column of each logical grapheme
	cx := x
	for _, l := range bl.VisualToLogical {
		col[l] = cx
		g := shaped[l]
		if bl.IsRTL(l) {
			g = text.Mirror(g)
		}
		if g != "" {
			cx = s.PutStringClip(cx, y, g, st, x+w)
		}
	}
	cellW := func(i int) int { return text.Width(shaped[i]) }
	cur := 0
	for cur < len(gs) && gs[cur].Offset < in.pos {
		cur++
	}
	switch {
	case len(gs) == 0:
		return x
	case cur < len(gs) && bl.IsRTL(cur):
		return col[cur] + cellW(cur)
	case cur < len(gs):
		return col[cur]
	case bl.IsRTL(len(gs) - 1):
		return col[len(gs)-1]
	}
	return col[len(gs)-1] + cellW(len(gs)-1)
}

// --- selectable list ----------------------------------------------------------

// listView keeps the selection and scroll offset of a vertical list.
type listView struct {
	n, sel, top int
}

func (l *listView) setLen(n int) {
	l.n = n
	if l.sel >= n {
		l.sel = n - 1
	}
	if l.sel < 0 {
		l.sel = 0
	}
}

func (l *listView) move(d int) {
	if l.n == 0 {
		return
	}
	l.sel += d
	if l.sel < 0 {
		l.sel = 0
	}
	if l.sel >= l.n {
		l.sel = l.n - 1
	}
}

// scroll keeps the selection within h visible rows.
func (l *listView) scroll(h int) {
	if h <= 0 {
		return
	}
	if l.sel < l.top {
		l.top = l.sel
	}
	if l.sel >= l.top+h {
		l.top = l.sel - h + 1
	}
	if l.top > l.n-h {
		l.top = l.n - h
	}
	if l.top < 0 {
		l.top = 0
	}
}

// listKey applies the common list navigation keys; it reports whether the
// key was one of them. vim enables j/k/g/G (lists without a text input).
func (l *listView) listKey(k term.KeyEvent, page int, vim bool) bool {
	ks := k.String()
	switch ks {
	case "down", "ctrl+n", "ctrl+j":
		l.move(1)
	case "up", "ctrl+p", "ctrl+k":
		l.move(-1)
	case "pgdown", "ctrl+d", "ctrl+f":
		l.move(max(1, page))
	case "pgup", "ctrl+u", "ctrl+b":
		l.move(-max(1, page))
	default:
		if !vim {
			return false
		}
		switch ks {
		case "j":
			l.move(1)
		case "k":
			l.move(-1)
		case "g", "home":
			l.sel = 0
		case "G", "end":
			l.move(l.n)
		default:
			return false
		}
	}
	return true
}

// --- panels -------------------------------------------------------------------

// panelRect returns a centred overlay rectangle of at most w×h cells, never
// larger than 80×24 (DESIGN.md §4.2) nor than the screen minus a margin.
func panelRect(sw, sh, w, h int) term.Rect {
	if w > 80 {
		w = 80
	}
	if h > 24 {
		h = 24
	}
	if w > sw-2 {
		w = sw - 2
	}
	if h > sh-2 {
		h = sh - 2
	}
	if w < 4 {
		w = min(4, sw)
	}
	if h < 3 {
		h = min(3, sh)
	}
	x := (sw - w) / 2
	// Sit slightly above the centre: the eye looks there first.
	y := (sh - 1 - h) / 3
	if y < 0 {
		y = 0
	}
	return term.Rect{X: x, Y: y, W: w, H: h}
}

// drawPanel draws a raised panel with a hairline border and an optional
// title set into the top border, and returns the inner rectangle (one cell
// of padding left and right).
func (a *app) drawPanel(r term.Rect, title string) term.Rect {
	s := a.scr
	s.Box(r, a.gl, a.st.border, a.st.panel)
	if title != "" && r.W > 8 {
		t := " " + text.Truncate(title, r.W-6, a.gl.Ellipsis) + " "
		s.PutString(r.X+2, r.Y, t, a.st.pMuted)
	}
	return term.Rect{X: r.X + 2, Y: r.Y + 1, W: r.W - 4, H: r.H - 2}
}

// panelSeparator draws a hairline across the panel at row y, joined to the
// border with tees.
func (a *app) panelSeparator(r term.Rect, y int) {
	s := a.scr
	s.HLine(r.X+1, y, r.W-2, a.gl.Rule, a.st.border)
	s.SetCell(r.X, y, a.gl.TeeRight, a.st.border)
	s.SetCell(r.X+r.W-1, y, a.gl.TeeLeft, a.st.border)
}

// drawRow paints the ground of a list row (selected or not) across the
// panel's inner width, plus the selection bar in the padding column.
func (a *app) drawRow(inner term.Rect, y int, selected bool) {
	if selected {
		a.scr.Fill(term.Rect{X: inner.X - 1, Y: y, W: inner.W + 1, H: 1}, " ", a.st.sel)
		a.scr.SetCell(inner.X-1, y, a.gl.Selected, a.st.selBar)
	}
}

// putHighlighted draws str with the bytes at positions (sorted byte
// offsets) in the match style, clipped to maxX; it returns the next column.
func (a *app) putHighlighted(x, y int, str string, st theme.Style, positions []int, maxX int) int {
	if len(positions) == 0 {
		return a.scr.PutStringClip(x, y, str, st, maxX)
	}
	hl := st
	hl.FG = a.st.match.FG
	hl.Attrs |= a.st.match.Attrs
	pi := 0
	for _, g := range text.Graphemes(str) {
		if x+g.Width > maxX {
			break
		}
		on := false
		for pi < len(positions) && positions[pi] < g.Offset+len(g.Text) {
			if positions[pi] >= g.Offset {
				on = true
			}
			pi++
		}
		cs := st
		if on {
			cs = hl
		}
		x += a.scr.SetCell(x, y, g.Text, cs)
	}
	return x
}

// putRanges draws str with the byte ranges in hl style (search matches).
func (a *app) putRanges(x, y int, str string, st, hl theme.Style, ranges [][2]int, maxX int) int {
	ri := 0
	for _, g := range text.Graphemes(str) {
		if x+g.Width > maxX {
			break
		}
		for ri < len(ranges) && ranges[ri][1] <= g.Offset {
			ri++
		}
		cs := st
		if ri < len(ranges) && ranges[ri][0] <= g.Offset && g.Offset < ranges[ri][1] {
			cs = hl
		}
		if g.Text == "\t" {
			x += a.scr.SetCell(x, y, " ", cs)
			continue
		}
		x += a.scr.SetCell(x, y, g.Text, cs)
	}
	return x
}

// fmtCount formats n with thousands separators (1,240).
func fmtCount(n int) string {
	s := itoa(n)
	if n < 1000 && n > -1000 {
		return s
	}
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [24]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmtCount(n) + " " + one
	}
	return fmtCount(n) + " " + many
}
