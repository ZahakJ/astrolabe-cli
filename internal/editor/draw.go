package editor

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
)

// layoutRow records where one display row was drawn, for cursor placement
// and mouse hit-testing.
type layoutRow struct {
	y      int
	line   int
	row    int
	seg    rowSeg
	cells  []cellPos // logical order
	startX int
	endX   int
	rtl    bool
}

type cellPos struct {
	x, w int
	col  int
	rtl  bool
}

// Draw draws the editor into r: the styled source soft-wrapped to the
// page measure and centred in r, the cursor-line ground, the Visual
// selection, search matches and the [[ completer popup. It places the
// terminal cursor (block in Normal and Visual mode, bar in Insert mode)
// and returns where it went. While the command line is active the cursor
// is hidden; draw the prompt with DrawCommandLine.
func (e *Editor) Draw(s *term.Screen, r term.Rect) Cursor {
	e.rect = r
	e.layout = e.layout[:0]
	if r.W <= 0 || r.H <= 0 {
		return Cursor{}
	}
	if r.W != e.width || r.H != e.height {
		e.Resize(r.W, r.H)
	} else {
		e.syncStates()
		e.ensureVisible()
	}
	th := e.th
	base := th.Base()
	s.Fill(r, " ", base)
	pageW, gutterW := e.pageDims()
	left := r.X + (r.W-pageW-gutterW)/2
	x0 := left + gutterW
	e.pageX, e.pageW = x0, pageW
	right := r.X + r.W
	padL, padR := max(r.X, x0-1), min(right, x0+pageW+1)

	var sel *textRange
	if e.mode == ModeVisual || e.mode == ModeVisualLine {
		vr := e.visualRange()
		sel = &vr
	}
	hl := e.highlightRE()
	curV := e.cursorView()
	cursor := Cursor{}
	numW := gutterW - 1

	v := e.top
	stylesLine := -1
	var styles []theme.Style
	var matches [][]int
	var shaped *lineShape
	for y := r.Y; y < r.Y+r.H; y++ {
		line := v.line
		ln := e.buf.line(line)
		if stylesLine != line {
			stylesLine = line
			styles = e.lineStyles(line)
			matches = nil
			if hl != nil {
				matches = hl.findAll(ln)
			}
			shaped = nil
			if e.bidi && text.HasRTL(ln) {
				shaped = newLineShape(ln)
			}
		}
		rs := e.rows(line)
		seg := rs[v.row]
		st := e.stateAt(line)
		_, _, isFence := fenceOpen(ln)
		if (st.fence != 0 || isFence) && !th.Raised.IsDefault() {
			s.Fill(term.Rect{X: padL, Y: y, W: padR - padL, H: 1}, " ", base.Bg(th.Raised))
		}
		lr := e.drawRow(s, y, x0, pageW, padR, line, ln, styles, seg, v.row, len(rs), matches, sel, shaped)
		lr.row = v.row
		e.layout = append(e.layout, lr)
		if line == e.cur.Line && v.row == curV.row {
			cursor = e.cursorFor(lr, ln)
			if bg := th.CursorLine.BG; !bg.IsDefault() && e.mode != ModeVisual && e.mode != ModeVisualLine {
				s.Restyle(term.Rect{X: padL, Y: y, W: padR - padL, H: 1}, func(c theme.Style) theme.Style {
					if c.BG == base.BG {
						return c.Bg(bg)
					}
					return c
				})
			}
		}
		if e.number && v.row == 0 && numW > 0 {
			num := text.PadLeft(strconv.Itoa(line+1), numW)
			ns := base.Fg(th.Faint)
			if line == e.cur.Line {
				ns = base.Fg(th.Muted)
			}
			s.PutStringClip(left, y, num, ns, x0-1)
		}
		next, ok := e.nextRow(v)
		if !ok {
			break
		}
		v = next
	}
	switch e.mode {
	case ModeInsert:
		cursor.Shape = term.CursorBar
	default:
		cursor.Shape = term.CursorBlock
	}
	e.drawCompleter(s, r, cursor)
	if e.cmd.active || !cursor.Visible {
		cursor.Visible = false
		s.ShowCursor(false)
	} else {
		s.SetCursor(cursor.X, cursor.Y)
		s.SetCursorShape(cursor.Shape)
		s.ShowCursor(true)
	}
	return cursor
}

// lineShape holds the Arabic-shaped form of every cluster of a line, so
// joining is right across row breaks.
type lineShape struct {
	starts []int
	shaped []string
}

func newLineShape(s string) *lineShape {
	st := clusterStarts(s)
	gs := make([]string, len(st)-1)
	for i := range gs {
		gs[i] = s[st[i]:st[i+1]]
	}
	return &lineShape{starts: st, shaped: text.ShapeClusters(gs)}
}

func (l *lineShape) at(col int) string { return l.shaped[clusterIndex(l.starts, col)] }

// highlightRE is the pattern whose matches are highlighted: the one being
// typed at the / prompt, else the last search while hlsearch is on.
func (e *Editor) highlightRE() *searchRE {
	if e.cmd.active && (e.cmd.kind == '/' || e.cmd.kind == '?') {
		return e.cmd.preview
	}
	if !e.search.hl || e.search.pattern == "" {
		return nil
	}
	if e.search.re == nil {
		re, err := compileVimPattern(e.search.pattern, e.search.ignorecase, e.search.smartcase && !e.search.noSmart)
		if err != nil {
			return nil
		}
		e.search.re = re
	}
	return e.search.re
}

type dcell struct {
	disp string
	w    int
	col  int
	st   theme.Style
	rtl  bool
}

func inSelection(sel *textRange, line, col int) bool {
	if sel == nil {
		return false
	}
	if sel.linewise {
		return line >= sel.start.Line && line <= sel.end.Line
	}
	p := Pos{line, col}
	return !p.Less(sel.start) && p.Less(sel.end)
}

// drawRow draws one display row and returns its layout.
func (e *Editor) drawRow(s *term.Screen, y, x0, pageW, maxX, line int, ln string, styles []theme.Style,
	seg rowSeg, row, nrows int, matches [][]int, sel *textRange, shaped *lineShape) layoutRow {
	th := e.th
	cells := make([]dcell, 0, seg.end-seg.start+1)
	v := seg.vstart
	mi := 0
	for i := seg.start; i < seg.end; {
		j := nextCol(ln, i)
		g := ln[i:j]
		w := clusterWidth(g, v)
		st := styles[i]
		disp := g
		switch {
		case g == "\t":
			disp = strings.Repeat(" ", w)
		case len(g) == 1 && (g[0] < 0x20 || g[0] == 0x7f):
			disp = "^" + string(rune(g[0]^0x40))
			st = st.Fg(th.Faint)
		case text.GraphemeWidth(g) == 0:
			r, _ := utf8.DecodeRuneInString(g)
			if r >= 0x80 && r < 0xa0 {
				disp = fmt.Sprintf("<%02x>", r)
			} else {
				disp = "◌" + g
			}
			st = st.Fg(th.Faint)
		}
		for mi < len(matches) && matches[mi][1] <= i && matches[mi][0] < i {
			mi++
		}
		if mi < len(matches) && matches[mi][0] <= i && i < matches[mi][1] {
			if line == e.cur.Line && matches[mi][0] == e.cur.Col {
				st = th.SearchCurrent.Over(st)
			} else {
				st = th.Search.Over(st)
			}
		}
		if inSelection(sel, line, i) {
			st = th.Selection.Over(st)
		}
		cells = append(cells, dcell{disp: disp, w: w, col: i, st: st})
		v += w
		i = j
	}
	// A selection that includes the line break shows one selected cell.
	if sel != nil && row == nrows-1 {
		p := Pos{line, len(ln)}
		if (sel.linewise && line >= sel.start.Line && line <= sel.end.Line && ln == "") ||
			(!sel.linewise && !p.Less(sel.start) && p.Less(sel.end)) {
			cells = append(cells, dcell{disp: " ", w: 1, col: len(ln), st: th.Selection.Over(th.Base())})
		}
	}
	lr := layoutRow{y: y, line: line, seg: seg}
	order := make([]int, len(cells))
	for i := range order {
		order[i] = i
	}
	if shaped != nil && len(cells) > 0 {
		base := text.BaseDirection(ln)
		if base == text.Neutral {
			base = text.LTR
		}
		lr.rtl = base == text.RTL
		gs := make([]string, len(cells))
		for i, c := range cells {
			if c.col < len(ln) && ln[c.col] != '\t' && c.disp == ln[c.col:nextCol(ln, c.col)] {
				cells[i].disp = shaped.at(c.col)
				if cells[i].disp == "" {
					cells[i].w = 0
				} else {
					cells[i].w = max(1, text.GraphemeWidth(cells[i].disp))
				}
			}
			gs[i] = ln[c.col:min(len(ln), nextCol(ln, c.col))]
			if gs[i] == "" || gs[i] == "\t" {
				gs[i] = " "
			}
		}
		bl := text.Reorder(gs, base)
		order = bl.VisualToLogical
		for i := range cells {
			if bl.IsRTL(i) {
				cells[i].rtl = true
				cells[i].disp = text.Mirror(cells[i].disp)
			}
		}
	}
	rowW := 0
	for _, c := range cells {
		rowW += c.w
	}
	startX := x0 + seg.indent
	if lr.rtl {
		startX = x0 + pageW - seg.indent - rowW
		if startX < x0 {
			startX = x0
		}
	}
	lr.startX = startX
	lr.cells = make([]cellPos, len(cells))
	x := startX
	for _, li := range order {
		c := cells[li]
		lr.cells[li] = cellPos{x: x, w: c.w, col: c.col, rtl: c.rtl}
		if c.disp != "" && x < maxX {
			s.PutStringClip(x, y, c.disp, c.st, maxX)
		}
		x += c.w
	}
	lr.endX = x
	return lr
}

// cursorFor places the cursor on its row.
func (e *Editor) cursorFor(lr layoutRow, ln string) Cursor {
	c := Cursor{Y: lr.y, Visible: true}
	insert := e.mode == ModeInsert
	for _, cp := range lr.cells {
		if cp.col == e.cur.Col && cp.col < len(ln) {
			c.X = cp.x
			if insert && cp.rtl {
				c.X = cp.x + cp.w
			}
			return e.clipCursor(c)
		}
	}
	// At the end of the line (Insert mode) or on an empty line: after the
	// text in reading order, which is the left end of a right-to-left row.
	c.X = lr.endX
	if lr.rtl || ln == "" {
		c.X = lr.startX
	}
	return e.clipCursor(c)
}

func (e *Editor) clipCursor(c Cursor) Cursor {
	if c.X >= e.rect.X+e.rect.W {
		c.X = e.rect.X + e.rect.W - 1
	}
	if c.X < e.rect.X {
		c.X = e.rect.X
	}
	return c
}

// drawCompleter draws the [[ popup below (or above) the cursor.
func (e *Editor) drawCompleter(s *term.Screen, r term.Rect, cur Cursor) {
	c := &e.completer
	if !c.open || !cur.Visible {
		return
	}
	th := e.th
	gl := e.gl
	items := c.items
	n := min(len(items), 8)
	rows := max(n, 1)
	w := 0
	for _, it := range items[:n] {
		lw := text.Width(it.Label)
		if it.Detail != "" && it.Detail != it.Label {
			lw += 2 + text.Width(it.Detail)
		}
		w = max(w, lw)
	}
	empty := "no matching note — Enter links “" + c.query + "”"
	if n == 0 {
		w = text.Width(empty)
	}
	w = clampInt(w+4, 24, min(64, r.W))
	h := rows + 2
	x := cur.X - text.Width(c.query) - 3
	if x+w > r.X+r.W {
		x = r.X + r.W - w
	}
	if x < r.X {
		x = r.X
	}
	y := cur.Y + 1
	if y+h > r.Y+r.H {
		y = cur.Y - h
		if y < r.Y {
			y = r.Y
			h = min(h, r.H)
		}
	}
	panel := theme.Style{FG: th.Text, BG: th.Raised}
	s.Box(term.Rect{X: x, Y: y, W: w, H: h}, gl, theme.Style{FG: th.Border, BG: th.Raised}, panel)
	if n == 0 {
		s.PutStringClip(x+2, y+1, text.Truncate(empty, w-4, gl.Ellipsis), panel.Fg(th.Muted), x+w-1)
		return
	}
	first := 0
	if c.sel >= n {
		first = c.sel - n + 1
	}
	for i := 0; i < n && y+1+i < y+h-1; i++ {
		k := first + i
		it := items[k]
		ry := y + 1 + i
		rs := panel
		if k == c.sel {
			if th.Hover.IsDefault() {
				rs = rs.With(theme.Reverse)
			} else {
				rs = rs.Bg(th.Hover)
			}
			s.Fill(term.Rect{X: x + 1, Y: ry, W: w - 2, H: 1}, " ", rs)
			s.PutStringClip(x+1, ry, gl.Selected, rs.Fg(th.Accent), x+2)
		}
		maxX := x + w - 2
		lx := x + 2
		label := text.Truncate(it.Label, maxX-lx, gl.Ellipsis)
		m, ok := text.FuzzyMatch(c.query, label)
		pos := m.Positions
		if !ok {
			pos = nil
		}
		if e.bidi && text.HasRTL(label) {
			// Shown shaped and in visual order, highlights carried along;
			// a right-to-left label is cut at its logical end.
			label, pos = text.VisualPositions(text.Truncate(it.Label, maxX-lx, gl.Ellipsis), pos)
		}
		hit := map[int]bool{}
		for _, p := range pos {
			hit[p] = true
		}
		for _, g := range text.Graphemes(label) {
			st := rs
			if hit[g.Offset] {
				st = st.Fg(th.Accent).With(theme.Bold)
			}
			lx = s.PutStringClip(lx, ry, g.Text, st, maxX)
		}
		if it.Detail != "" && it.Detail != it.Label && lx+3 < maxX {
			d := text.TruncateLeft(it.Detail, maxX-lx-2, gl.Ellipsis)
			if e.bidi && text.HasRTL(d) {
				d = text.VisualPath(d) // a path: folders first
			}
			s.PutStringClip(lx+2, ry, d, rs.Fg(th.Muted), maxX)
		}
	}
}

// DrawCommandLine draws the ":" or search prompt into the one-row rect r
// (normally the status bar's row) and places the terminal cursor in it. It
// does nothing when the command line is not active.
func (e *Editor) DrawCommandLine(s *term.Screen, r term.Rect) Cursor {
	if !e.cmd.active || r.W <= 1 || r.H <= 0 {
		return Cursor{}
	}
	th := e.th
	st := theme.Style{FG: th.Text, BG: th.StatusBar.BG}
	s.Fill(term.Rect{X: r.X, Y: r.Y, W: r.W, H: 1}, " ", st)
	s.PutStringClip(r.X, r.Y, string(rune(e.cmd.kind)), st.Fg(th.Accent).With(theme.Bold), r.X+1)
	avail := r.W - 2
	t := e.cmd.text
	pos := e.cmd.pos
	// scroll horizontally so the cursor stays visible
	start := 0
	for text.Width(t[start:pos]) > avail && start < pos {
		start = nextCol(t, start)
	}
	s.PutStringClip(r.X+1, r.Y, t[start:], st, r.X+r.W)
	c := Cursor{X: r.X + 1 + text.Width(t[start:pos]), Y: r.Y, Shape: term.CursorBar, Visible: true}
	s.SetCursor(c.X, c.Y)
	s.SetCursorShape(c.Shape)
	s.ShowCursor(true)
	return c
}

// HandleMouse handles a mouse event in screen coordinates (as delivered by
// internal/term): the wheel scrolls three rows, a click places the cursor
// and a drag selects (Visual mode).
func (e *Editor) HandleMouse(ev term.MouseEvent) Result {
	switch ev.Button {
	case term.MouseWheelUp:
		e.scrollLines(-3)
		return Result{}
	case term.MouseWheelDown:
		e.scrollLines(3)
		return Result{}
	}
	if ev.Button != term.MouseLeft && !(ev.Action == term.MouseMotion && e.mouseDown) && ev.Action != term.MouseRelease {
		return Result{}
	}
	if e.cmd.active {
		return Result{}
	}
	p, ok := e.posAt(ev.X, ev.Y)
	switch ev.Action {
	case term.MousePress:
		if !ok {
			return Result{}
		}
		if e.mode == ModeVisual || e.mode == ModeVisualLine {
			e.exitVisual()
		}
		if e.mode == ModeInsert {
			e.breakInsert()
		}
		e.pend = pending{}
		e.cur = p
		e.clampCursor()
		e.setWant()
		e.mouseDown = true
		e.mouseAnchor = e.cur
	case term.MouseMotion:
		if !e.mouseDown || !ok {
			return Result{}
		}
		if p != e.mouseAnchor && e.mode == ModeNormal {
			e.mode = ModeVisual
			e.anchor = e.mouseAnchor
		}
		e.cur = p
		e.clampCursor()
		e.setWant()
	case term.MouseRelease:
		e.mouseDown = false
	}
	e.ensureVisible()
	return Result{}
}

// posAt maps a screen cell to a buffer position using the last Draw.
func (e *Editor) posAt(x, y int) (Pos, bool) {
	if len(e.layout) == 0 || !e.rect.Contains(x, y) {
		return Pos{}, false
	}
	var lr *layoutRow
	for i := range e.layout {
		if e.layout[i].y == y {
			lr = &e.layout[i]
			break
		}
	}
	if lr == nil {
		last := e.layout[len(e.layout)-1]
		if y < last.y {
			return Pos{}, false
		}
		lr = &last
	}
	ln := e.buf.line(lr.line)
	best, bestD := -1, 1<<30
	for i, c := range lr.cells {
		if c.col >= len(ln) {
			continue
		}
		if x >= c.x && x < c.x+max(c.w, 1) {
			best = i
			break
		}
		d := x - c.x
		if d < 0 {
			d = -d
		}
		if d < bestD {
			best, bestD = i, d
		}
	}
	if best < 0 {
		return Pos{lr.line, lr.seg.start}, true
	}
	col := lr.cells[best].col
	if !lr.rtl && x >= lr.endX && e.mode == ModeInsert && lr.row == len(e.rows(lr.line))-1 {
		col = len(ln)
	}
	return Pos{lr.line, col}, true
}
