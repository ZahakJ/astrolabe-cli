package editor

import (
	"math"

	"github.com/ZahakJ/astrolabe-cli/internal/text"
)

// rowSeg is one display row of a logical line: bytes [start, end), drawn
// after indent cells (the hanging indent of continuation rows), starting at
// display column vstart of the logical line.
type rowSeg struct {
	start, end int
	indent     int
	vstart     int
}

// wrapCache memoises the display rows of line texts for one page width.
type wrapCache struct {
	width int
	m     map[string][]rowSeg
}

func (c *wrapCache) reset() { c.m = nil }

func (c *wrapCache) get(s string, width int) []rowSeg {
	if c.m == nil || c.width != width {
		c.m = make(map[string][]rowSeg)
		c.width = width
	}
	if r, ok := c.m[s]; ok {
		return r
	}
	if len(c.m) > 20000 {
		c.m = make(map[string][]rowSeg)
	}
	r := wrapLine(s, width)
	c.m[s] = r
	return r
}

// pageDims returns the page (measure) width and the line-number gutter
// width for the current view width. The page keeps a margin of two cells
// on each side when there is room, like the reader's page.
func (e *Editor) pageDims() (pageW, gutterW int) {
	if e.number {
		d := 1
		for n := e.buf.lineCount(); n >= 10; n /= 10 {
			d++
		}
		gutterW = max(d, 3) + 1
	}
	avail := e.width - gutterW
	margin := 0
	switch {
	case avail >= 40:
		margin = 2
	case avail >= 20:
		margin = 1
	}
	pageW = min(e.measure, avail-2*margin)
	if pageW < 1 {
		pageW = 1
	}
	return pageW, gutterW
}

func (e *Editor) rows(line int) []rowSeg {
	w, _ := e.pageDims()
	return e.wraps.get(e.buf.line(line), w)
}

// hangIndent is the hanging indent of a line's continuation rows: the
// content column of a list item or quote, else its leading blanks.
func hangIndent(s string, width int) int {
	h := 0
	if lp, ok := parseListPrefix(s); ok {
		h = vcolOf(s, lp.end)
	} else {
		h = wsWidth(leadingWS(s))
	}
	if h > width/2 {
		h = 0
	}
	return h
}

// wrapLine splits a line into display rows of at most width cells. Breaks
// follow UAX #14 (internal/text); blanks at a break stay at the end of the
// row they follow so every byte belongs to exactly one row and the cursor
// can sit on them.
func wrapLine(s string, width int) []rowSeg {
	if s == "" {
		return []rowSeg{{}}
	}
	plain := true
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f || s[i] >= 0x80 {
			plain = false
			break
		}
	}
	if plain && len(s) <= width {
		return []rowSeg{{start: 0, end: len(s)}}
	}
	// Build the display form (tabs expanded, controls as ^X) with a map
	// back to source offsets.
	disp := s
	var src []int
	if !plain {
		var b []byte
		src = make([]int, 0, len(s)+8)
		v := 0
		for i := 0; i < len(s); {
			j := nextCol(s, i)
			g := s[i:j]
			w := clusterWidth(g, v)
			switch {
			case g == "\t":
				for k := 0; k < w; k++ {
					b = append(b, ' ')
					src = append(src, i)
				}
			case len(g) == 1 && (g[0] < 0x20 || g[0] == 0x7f):
				b = append(b, '^', g[0]^0x40)
				src = append(src, i, i)
			default:
				b = append(b, g...)
				for k := 0; k < len(g); k++ {
					src = append(src, i)
				}
			}
			v += w
			i = j
		}
		src = append(src, len(s))
		disp = string(b)
	}
	if text.Width(disp) <= width && plain {
		return []rowSeg{{start: 0, end: len(s)}}
	}
	hang := hangIndent(s, width)
	ranges := text.WrapRanges(disp, width, width-hang)
	mapOff := func(o int) int {
		if src == nil {
			return o
		}
		if o >= len(src) {
			return len(s)
		}
		return src[o]
	}
	rows := make([]rowSeg, 0, len(ranges))
	for i, r := range ranges {
		st := mapOff(r.Start)
		if i == 0 {
			st = 0
		}
		if len(rows) > 0 && st <= rows[len(rows)-1].start {
			continue
		}
		rows = append(rows, rowSeg{start: st})
	}
	for i := range rows {
		if i+1 < len(rows) {
			rows[i].end = rows[i+1].start
		} else {
			rows[i].end = len(s)
		}
		if i > 0 {
			rows[i].indent = hang
		}
	}
	// display column at each row start, in one pass
	v, k := 0, 0
	for i := 0; i < len(s) && k < len(rows); {
		for k < len(rows) && rows[k].start == i {
			rows[k].vstart = v
			k++
		}
		j := nextCol(s, i)
		v += clusterWidth(s[i:j], v)
		i = j
	}
	return rows
}

// rowOf returns the row of rows containing byte col.
func rowOf(rows []rowSeg, col int) int {
	r := 0
	for i := 1; i < len(rows); i++ {
		if rows[i].start <= col {
			r = i
		}
	}
	return r
}

func (e *Editor) cursorView() viewPos {
	return viewPos{e.cur.Line, rowOf(e.rows(e.cur.Line), e.cur.Col)}
}

// cursorX is the cursor's x offset within the page (logical order).
func (e *Editor) cursorX() int {
	rs := e.rows(e.cur.Line)
	r := rs[rowOf(rs, e.cur.Col)]
	return r.indent + vcolOf(e.buf.line(e.cur.Line), e.cur.Col) - r.vstart
}

func (e *Editor) nextRow(v viewPos) (viewPos, bool) {
	if v.row+1 < len(e.rows(v.line)) {
		return viewPos{v.line, v.row + 1}, true
	}
	if v.line+1 < e.buf.lineCount() {
		return viewPos{v.line + 1, 0}, true
	}
	return v, false
}

func (e *Editor) prevRow(v viewPos) (viewPos, bool) {
	if v.row > 0 {
		return viewPos{v.line, v.row - 1}, true
	}
	if v.line > 0 {
		return viewPos{v.line - 1, len(e.rows(v.line-1)) - 1}, true
	}
	return v, false
}

func (e *Editor) back(v viewPos, n int) viewPos {
	for i := 0; i < n; i++ {
		p, ok := e.prevRow(v)
		if !ok {
			break
		}
		v = p
	}
	return v
}

func (e *Editor) fwd(v viewPos, n int) viewPos {
	for i := 0; i < n; i++ {
		p, ok := e.nextRow(v)
		if !ok {
			break
		}
		v = p
	}
	return v
}

func (v viewPos) less(w viewPos) bool {
	return v.line < w.line || (v.line == w.line && v.row < w.row)
}

// rowsBetween counts rows from a forward to b, giving up past limit.
func (e *Editor) rowsBetween(a, b viewPos, limit int) int {
	if b.less(a) {
		return -1
	}
	n := 0
	for a != b && n <= limit {
		p, ok := e.nextRow(a)
		if !ok {
			break
		}
		a = p
		n++
	}
	return n
}

func (e *Editor) lastRow() viewPos {
	l := e.buf.lineCount() - 1
	return viewPos{l, len(e.rows(l)) - 1}
}

func (e *Editor) scrolloff() int { return min(3, (e.height-1)/2) }

// clampTop keeps the top row valid after edits.
func (e *Editor) clampTop() {
	if e.top.line >= e.buf.lineCount() {
		e.top = viewPos{e.buf.lineCount() - 1, 0}
	}
	if e.top.line < 0 {
		e.top = viewPos{}
	}
	if n := len(e.rows(e.top.line)); e.top.row >= n {
		e.top.row = n - 1
	}
}

// ensureVisible scrolls so that the cursor row is on screen with
// scrolloff=3 rows of context (fewer near the ends of the buffer).
func (e *Editor) ensureVisible() {
	e.clampTop()
	h := e.height
	so := e.scrolloff()
	c := e.cursorView()
	if c.less(e.top) {
		e.top = e.back(c, so)
		return
	}
	d := e.rowsBetween(e.top, c, h)
	if d < so {
		e.top = e.back(c, so)
		return
	}
	below := e.rowsBetween(c, e.fwd(c, so), so)
	if d > h-1-below {
		e.top = e.back(c, h-1-below)
	}
}

// displayMove moves the cursor one display row (gj / gk), keeping the
// remembered x offset. It returns false at the buffer's ends.
func (e *Editor) displayMove(down bool) bool {
	c := e.cursorView()
	var t viewPos
	var ok bool
	if down {
		t, ok = e.nextRow(c)
	} else {
		t, ok = e.prevRow(c)
	}
	if !ok {
		return false
	}
	s := e.buf.line(t.line)
	rs := e.rows(t.line)
	seg := rs[t.row]
	wantX := e.wantX
	target := seg.vstart
	if wantX > seg.indent {
		if wantX >= math.MaxInt32 {
			target = math.MaxInt32
		} else {
			target = seg.vstart + wantX - seg.indent
		}
	}
	col := seg.start
	v := seg.vstart
	for col < seg.end {
		j := nextCol(s, col)
		w := clusterWidth(s[col:j], v)
		if v+w > target {
			break
		}
		v += w
		col = j
	}
	if col >= seg.end && t.row+1 < len(rs) && seg.end > seg.start {
		col = prevCol(s, seg.end) // stay on this row
	}
	e.cur = Pos{t.line, col}
	e.clampCursor()
	if e.wantX >= math.MaxInt32 {
		e.want = math.MaxInt32
	} else {
		e.want = vcolOf(s, e.cur.Col)
	}
	return true
}

// screenLine returns the target of H, M and L.
func (e *Editor) screenLine(tok string, count int, hasCount bool) Pos {
	h := e.height
	so := e.scrolloff()
	top := e.top
	bottom := e.fwd(top, h-1)
	var t viewPos
	switch tok {
	case "H":
		n := count - 1
		if top != (viewPos{}) {
			n = max(n, so)
		}
		t = e.fwd(top, n)
		if bottom.less(t) {
			t = bottom
		}
	case "L":
		n := count - 1
		if bottom != e.lastRow() {
			n = max(n, so)
		}
		t = e.back(bottom, n)
		if t.less(top) {
			t = top
		}
	default:
		rows := e.rowsBetween(top, bottom, h)
		t = e.fwd(top, rows/2)
	}
	return Pos{t.line, firstNonBlank(e.buf.line(t.line))}
}

// maxTop is the furthest the view may scroll by half pages: the last line
// at the bottom of the window.
func (e *Editor) maxTop() viewPos { return e.back(e.lastRow(), e.height-1) }

func (e *Editor) scrollHalf(down bool, count int, hasCount bool) {
	n := e.height / 2
	if hasCount {
		n = count
	}
	if n < 1 {
		n = 1
	}
	c := e.cursorView()
	if down {
		if c == e.lastRow() {
			return
		}
		nt := e.fwd(e.top, n)
		if mt := e.maxTop(); mt.less(nt) {
			nt = maxView(mt, e.top)
		}
		e.top = nt
	} else {
		if c == (viewPos{}) {
			return
		}
		e.top = e.back(e.top, n)
	}
	for i := 0; i < n; i++ {
		if !e.displayMove(down) {
			break
		}
	}
	e.keepCursorInView()
}

func maxView(a, b viewPos) viewPos {
	if a.less(b) {
		return b
	}
	return a
}

func (e *Editor) scrollPage(down bool, count int) {
	n := max(1, e.height-2) * max(1, count)
	if down {
		nt := e.fwd(e.top, n)
		if e.top == nt {
			return
		}
		e.top = nt
	} else {
		if e.top == (viewPos{}) {
			return
		}
		e.top = e.back(e.top, n)
	}
	e.keepCursorInView()
}

// scrollLines scrolls the view by n rows (Ctrl-e / Ctrl-y), moving the
// cursor only as far as scrolloff requires.
func (e *Editor) scrollLines(n int) {
	if n > 0 {
		nt := e.fwd(e.top, n)
		last := e.lastRow()
		if last.less(nt) {
			nt = last
		}
		e.top = nt
	} else {
		e.top = e.back(e.top, -n)
	}
	e.keepCursorInView()
}

// keepCursorInView moves the cursor into the window (respecting
// scrolloff) after the view scrolled.
func (e *Editor) keepCursorInView() {
	so := e.scrolloff()
	c := e.cursorView()
	lo := e.top
	if lo != (viewPos{}) {
		lo = e.fwd(e.top, so)
	}
	bottom := e.fwd(e.top, e.height-1)
	hi := bottom
	if bottom != e.lastRow() {
		hi = e.back(bottom, so)
	}
	var t viewPos
	switch {
	case c.less(lo):
		t = lo
	case hi.less(c):
		t = hi
	default:
		return
	}
	for i := 0; e.cursorView() != t && i < 100000; i++ {
		if !e.displayMove(e.cursorView().less(t)) {
			break
		}
	}
}

// scrollCursorTo scrolls so the cursor row is n rows from the top (zt zz
// zb), within scrolloff.
func (e *Editor) scrollCursorTo(n int) {
	so := e.scrolloff()
	n = clampInt(n, so, max(so, e.height-1-so))
	e.top = e.back(e.cursorView(), n)
}
