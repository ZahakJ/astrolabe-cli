// Package termtest provides a virtual terminal for tests: it interprets the
// escape sequences folio emits and applies them to a cell grid, so tests can
// assert on what a user would see rather than on raw bytes.
//
// Supported: printable text with grapheme clustering and wide cells, CR LF
// BS HT, CUP/CUU/CUD/CUF/CUB/CHA/VPA, ED, EL, SGR (bold, faint, italic,
// underline incl. 4:3/4:4, reverse, strike, 16/256/RGB colours), DECSET/
// DECRST private modes (cursor, alternate screen, autowrap, synchronized
// output, bracketed paste, mouse), DECSCUSR cursor shape, DECSC/DECRC,
// OSC 8 hyperlinks and OSC 52 clipboard. Unknown sequences are consumed and
// ignored.
package termtest

import (
	"bytes"
	"encoding/base64"
	"strconv"
	"strings"
	"sync"

	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/theme"
)

// Cell is one cell of the virtual screen. The right half of a wide
// grapheme has Text "" and Width 0.
type Cell struct {
	Text  string
	Width int
	Style theme.Style
}

type grid struct {
	cells [][]Cell
}

func newGrid(w, h int) *grid {
	g := &grid{cells: make([][]Cell, h)}
	for y := range g.cells {
		g.cells[y] = blankRow(w)
	}
	return g
}

func blankRow(w int) []Cell {
	r := make([]Cell, w)
	for i := range r {
		r[i] = Cell{Text: " ", Width: 1}
	}
	return r
}

// VT is a virtual terminal. It is safe for concurrent use (Write may be
// called from one goroutine while another inspects the screen).
type VT struct {
	mu   sync.Mutex
	w, h int
	main *grid
	alt  *grid
	cur  *grid

	x, y        int
	pendingWrap bool
	style       theme.Style
	savedX      int
	savedY      int
	savedStyle  theme.Style

	modes       map[int]bool
	cursorShape int
	clipboard   string
	syncBegins  int
	syncEnds    int
	writes      int

	state  int
	params []byte
	inter  []byte
	osc    []byte
	text   []byte
}

const (
	stGround = iota
	stEsc
	stEscSkip // ESC ( X: one more byte
	stCSI
	stOSC
	stOSCEsc
	stString // DCS/APC/PM/SOS: ignored until ST
	stStringEsc
)

// New returns a w×h virtual terminal with autowrap on and the cursor
// visible, like a freshly opened xterm.
func New(w, h int) *VT {
	v := &VT{w: w, h: h, modes: map[int]bool{7: true, 25: true}}
	v.main = newGrid(w, h)
	v.alt = newGrid(w, h)
	v.cur = v.main
	return v
}

func (v *VT) reset() {
	v.main, v.alt = newGrid(v.w, v.h), newGrid(v.w, v.h)
	v.cur = v.main
	v.x, v.y, v.pendingWrap = 0, 0, false
	v.style = theme.Style{}
	v.modes = map[int]bool{7: true, 25: true}
	v.cursorShape = 0
}

// Write interprets p. It never fails.
func (v *VT) Write(p []byte) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.writes++
	for _, c := range p {
		v.feed(c)
	}
	v.flushText()
	return len(p), nil
}

// Writes returns how many Write calls the terminal received.
func (v *VT) Writes() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.writes
}

func (v *VT) feed(c byte) {
	switch v.state {
	case stGround:
		switch {
		case c == 0x1b:
			v.flushText()
			v.state = stEsc
		case c < 0x20 || c == 0x7f:
			v.flushText()
			v.control(c)
		default:
			v.text = append(v.text, c)
		}
	case stEsc:
		v.state = stGround
		switch c {
		case '[':
			v.state = stCSI
			v.params = v.params[:0]
			v.inter = v.inter[:0]
		case ']':
			v.state = stOSC
			v.osc = v.osc[:0]
		case 'P', '_', '^', 'X':
			v.state = stString
		case '(', ')', '*', '+':
			v.state = stEscSkip
		case '7':
			v.savedX, v.savedY, v.savedStyle = v.x, v.y, v.style
		case '8':
			v.x, v.y, v.style = v.savedX, v.savedY, v.savedStyle
			v.pendingWrap = false
		case 'c':
			v.reset()
		case 'D':
			v.lineFeed()
		case 'M':
			if v.y > 0 {
				v.y--
			}
		case 'E':
			v.x = 0
			v.lineFeed()
		}
	case stEscSkip:
		v.state = stGround
	case stCSI:
		switch {
		case c >= 0x30 && c <= 0x3f:
			v.params = append(v.params, c)
		case c >= 0x20 && c <= 0x2f:
			v.inter = append(v.inter, c)
		case c >= 0x40 && c <= 0x7e:
			v.csi(c)
			v.state = stGround
		case c == 0x1b:
			v.state = stEsc
		default:
			// Control characters inside CSI are executed (VT100 behaviour).
			v.control(c)
		}
	case stOSC:
		switch c {
		case 0x07:
			v.oscDone()
		case 0x1b:
			v.state = stOSCEsc
		default:
			v.osc = append(v.osc, c)
		}
	case stOSCEsc:
		if c == '\\' {
			v.oscDone()
		} else {
			v.state = stEsc
			v.feed(c)
		}
	case stString:
		if c == 0x1b {
			v.state = stStringEsc
		} else if c == 0x07 {
			v.state = stGround
		}
	case stStringEsc:
		if c == '\\' {
			v.state = stGround
		} else {
			v.state = stString
		}
	}
}

func (v *VT) control(c byte) {
	switch c {
	case '\r':
		v.x = 0
		v.pendingWrap = false
	case '\n', 0x0b, 0x0c:
		v.lineFeed()
	case 0x08:
		if v.x > 0 {
			v.x--
		}
		v.pendingWrap = false
	case '\t':
		v.x = min((v.x/8+1)*8, v.w-1)
		v.pendingWrap = false
	}
}

func (v *VT) lineFeed() {
	v.pendingWrap = false
	if v.y == v.h-1 {
		v.scrollUp()
	} else {
		v.y++
	}
}

func (v *VT) scrollUp() {
	copy(v.cur.cells, v.cur.cells[1:])
	v.cur.cells[v.h-1] = blankRow(v.w)
}

func (v *VT) flushText() {
	if len(v.text) == 0 {
		return
	}
	s := string(v.text)
	v.text = v.text[:0]
	text.EachGrapheme(s, func(g string, w int) bool {
		v.put(g, w)
		return true
	})
}

func (v *VT) put(g string, w int) {
	if w == 0 {
		// Combining mark or ZWJ: attach to the previous cell.
		x := v.x - 1
		if v.pendingWrap {
			x = v.x
		}
		if x >= 0 && x < v.w {
			row := v.cur.cells[v.y]
			if row[x].Width == 0 && x > 0 {
				x--
			}
			row[x].Text += g
		}
		return
	}
	if v.pendingWrap && v.modes[7] {
		v.x = 0
		v.lineFeed()
	}
	v.pendingWrap = false
	if v.x+w > v.w {
		if v.modes[7] {
			v.x = 0
			v.lineFeed()
		} else {
			v.x = v.w - w
		}
	}
	if v.x < 0 || v.w == 0 {
		return
	}
	row := v.cur.cells[v.y]
	v.breakWide(row, v.x)
	if w == 2 {
		v.breakWide(row, v.x+1)
		row[v.x] = Cell{Text: g, Width: 2, Style: v.style}
		row[v.x+1] = Cell{Text: "", Width: 0, Style: v.style}
	} else {
		row[v.x] = Cell{Text: g, Width: 1, Style: v.style}
	}
	v.x += w
	if v.x >= v.w {
		v.x = v.w - 1
		v.pendingWrap = v.modes[7]
	}
}

func (v *VT) breakWide(row []Cell, x int) {
	if x >= len(row) {
		return
	}
	switch {
	case row[x].Width == 0 && x > 0:
		row[x-1] = Cell{Text: " ", Width: 1, Style: row[x-1].Style}
	case row[x].Width == 2 && x+1 < len(row):
		row[x+1] = Cell{Text: " ", Width: 1, Style: row[x+1].Style}
	}
}

// params parses ";"-separated numbers; sub-parameters after ":" are kept
// in the returned groups.
func parseParams(p []byte) [][]int {
	if len(p) == 0 {
		return nil
	}
	var out [][]int
	for _, part := range bytes.Split(p, []byte{';'}) {
		var g []int
		for _, sub := range bytes.Split(part, []byte{':'}) {
			n, err := strconv.Atoi(string(sub))
			if err != nil {
				n = -1 // empty or invalid: "default"
			}
			g = append(g, n)
		}
		out = append(out, g)
	}
	return out
}

func param(ps [][]int, i, def int) int {
	if i < len(ps) && len(ps[i]) > 0 && ps[i][0] > 0 {
		return ps[i][0]
	}
	return def
}

func (v *VT) csi(final byte) {
	priv := byte(0)
	raw := v.params
	if len(raw) > 0 && (raw[0] == '?' || raw[0] == '>' || raw[0] == '<' || raw[0] == '=') {
		priv, raw = raw[0], raw[1:]
	}
	ps := parseParams(raw)
	if len(v.inter) > 0 {
		if v.inter[0] == ' ' && final == 'q' {
			v.cursorShape = param(ps, 0, 0)
			if len(ps) > 0 && ps[0][0] == 0 {
				v.cursorShape = 0
			}
		}
		return
	}
	switch priv {
	case '?':
		if final == 'h' || final == 'l' {
			for _, g := range ps {
				v.setMode(g[0], final == 'h')
			}
		}
		return
	case '>', '<', '=':
		return // kitty keyboard flags, XTMODKEYS…: ignored
	}
	switch final {
	case 'H', 'f':
		v.x = clamp(param(ps, 1, 1)-1, 0, v.w-1)
		v.y = clamp(param(ps, 0, 1)-1, 0, v.h-1)
		v.pendingWrap = false
	case 'A':
		v.y = clamp(v.y-param(ps, 0, 1), 0, v.h-1)
		v.pendingWrap = false
	case 'B':
		v.y = clamp(v.y+param(ps, 0, 1), 0, v.h-1)
		v.pendingWrap = false
	case 'C':
		v.x = clamp(v.x+param(ps, 0, 1), 0, v.w-1)
		v.pendingWrap = false
	case 'D':
		v.x = clamp(v.x-param(ps, 0, 1), 0, v.w-1)
		v.pendingWrap = false
	case 'G':
		v.x = clamp(param(ps, 0, 1)-1, 0, v.w-1)
		v.pendingWrap = false
	case 'd':
		v.y = clamp(param(ps, 0, 1)-1, 0, v.h-1)
		v.pendingWrap = false
	case 'J':
		v.eraseDisplay(paramZero(ps))
	case 'K':
		v.eraseLine(paramZero(ps))
	case 'm':
		v.sgr(ps)
	case 's':
		v.savedX, v.savedY = v.x, v.y
	case 'u':
		v.x, v.y = v.savedX, v.savedY
	}
}

func paramZero(ps [][]int) int {
	if len(ps) > 0 && len(ps[0]) > 0 && ps[0][0] > 0 {
		return ps[0][0]
	}
	return 0
}

func clamp(n, lo, hi int) int { return max(lo, min(n, hi)) }

func (v *VT) blankCell() Cell {
	// Erase uses the current background (bce), like xterm.
	return Cell{Text: " ", Width: 1, Style: theme.Style{BG: v.style.BG}}
}

func (v *VT) eraseLine(mode int) {
	row := v.cur.cells[v.y]
	from, to := 0, v.w
	switch mode {
	case 0:
		from = v.x
	case 1:
		to = v.x + 1
	}
	for x := from; x < to && x < v.w; x++ {
		row[x] = v.blankCell()
	}
}

func (v *VT) eraseDisplay(mode int) {
	switch mode {
	case 0:
		v.eraseLine(0)
		for y := v.y + 1; y < v.h; y++ {
			v.fillRow(y)
		}
	case 1:
		v.eraseLine(1)
		for y := 0; y < v.y; y++ {
			v.fillRow(y)
		}
	default:
		for y := 0; y < v.h; y++ {
			v.fillRow(y)
		}
	}
}

func (v *VT) fillRow(y int) {
	for x := range v.cur.cells[y] {
		v.cur.cells[y][x] = v.blankCell()
	}
}

func (v *VT) setMode(m int, on bool) {
	switch m {
	case 1049, 1047, 47:
		if on && v.cur != v.alt {
			v.savedX, v.savedY = v.x, v.y
			v.alt = newGrid(v.w, v.h)
			v.cur = v.alt
		} else if !on && v.cur == v.alt {
			v.cur = v.main
			v.x, v.y = v.savedX, v.savedY
		}
	case 2026:
		if on {
			v.syncBegins++
		} else {
			v.syncEnds++
		}
	}
	v.modes[m] = on
}

func (v *VT) sgr(ps [][]int) {
	if len(ps) == 0 {
		v.style = theme.Style{Link: v.style.Link}
		return
	}
	for i := 0; i < len(ps); i++ {
		g := ps[i]
		n := g[0]
		switch {
		case n <= 0:
			v.style = theme.Style{Link: v.style.Link}
		case n == 1:
			v.style.Attrs |= theme.Bold
		case n == 2:
			v.style.Attrs |= theme.Faint
		case n == 3:
			v.style.Attrs |= theme.Italic
		case n == 4:
			v.style.Attrs &^= theme.Underline | theme.CurlyUnderline | theme.DottedUnderline
			sub := 1
			if len(g) > 1 {
				sub = g[1]
			}
			switch sub {
			case 0:
			case 3:
				v.style.Attrs |= theme.CurlyUnderline
			case 4:
				v.style.Attrs |= theme.DottedUnderline
			default:
				v.style.Attrs |= theme.Underline
			}
		case n == 7:
			v.style.Attrs |= theme.Reverse
		case n == 9:
			v.style.Attrs |= theme.Strike
		case n == 22:
			v.style.Attrs &^= theme.Bold | theme.Faint
		case n == 23:
			v.style.Attrs &^= theme.Italic
		case n == 24:
			v.style.Attrs &^= theme.Underline | theme.CurlyUnderline | theme.DottedUnderline
		case n == 27:
			v.style.Attrs &^= theme.Reverse
		case n == 29:
			v.style.Attrs &^= theme.Strike
		case n >= 30 && n <= 37:
			v.style.FG = theme.ANSI(uint8(n - 30))
		case n >= 90 && n <= 97:
			v.style.FG = theme.ANSI(uint8(n - 90 + 8))
		case n == 39:
			v.style.FG = theme.Default
		case n >= 40 && n <= 47:
			v.style.BG = theme.ANSI(uint8(n - 40))
		case n >= 100 && n <= 107:
			v.style.BG = theme.ANSI(uint8(n - 100 + 8))
		case n == 49:
			v.style.BG = theme.Default
		case n == 38 || n == 48:
			var c theme.Color
			if len(g) > 1 { // colon form 38:2::r:g:b or 38:5:n
				c = extColor(g[1:])
			} else {
				var used int
				c, used = extColorSemi(ps[i+1:])
				i += used
			}
			if n == 38 {
				v.style.FG = c
			} else {
				v.style.BG = c
			}
		}
	}
}

func extColor(g []int) theme.Color {
	switch {
	case len(g) >= 2 && g[0] == 5:
		return theme.ANSI(uint8(max(g[1], 0)))
	case len(g) >= 5 && g[0] == 2: // 2:colourspace:r:g:b
		return theme.RGB(uint8(max(g[2], 0)), uint8(max(g[3], 0)), uint8(max(g[4], 0)))
	case len(g) == 4 && g[0] == 2:
		return theme.RGB(uint8(max(g[1], 0)), uint8(max(g[2], 0)), uint8(max(g[3], 0)))
	}
	return theme.Default
}

func extColorSemi(ps [][]int) (theme.Color, int) {
	if len(ps) >= 2 && ps[0][0] == 5 {
		return theme.ANSI(uint8(max(ps[1][0], 0))), 2
	}
	if len(ps) >= 4 && ps[0][0] == 2 {
		return theme.RGB(uint8(max(ps[1][0], 0)), uint8(max(ps[2][0], 0)), uint8(max(ps[3][0], 0))), 4
	}
	return theme.Default, len(ps)
}

func (v *VT) oscDone() {
	v.state = stGround
	s := string(v.osc)
	switch {
	case strings.HasPrefix(s, "8;"):
		// 8;params;URL
		rest := s[2:]
		if i := strings.IndexByte(rest, ';'); i >= 0 {
			v.style.Link = rest[i+1:]
		}
	case strings.HasPrefix(s, "52;"):
		parts := strings.SplitN(s, ";", 3)
		if len(parts) == 3 {
			if b, err := base64.StdEncoding.DecodeString(parts[2]); err == nil {
				v.clipboard = string(b)
			}
		}
	}
}

// Resize changes the terminal size, keeping the top-left contents.
func (v *VT) Resize(w, h int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, g := range []*grid{v.main, v.alt} {
		cells := make([][]Cell, h)
		for y := range cells {
			cells[y] = blankRow(w)
			if y < len(g.cells) {
				copy(cells[y], g.cells[y])
			}
		}
		g.cells = cells
	}
	v.w, v.h = w, h
	v.x, v.y = clamp(v.x, 0, w-1), clamp(v.y, 0, h-1)
}

// Size returns the terminal size.
func (v *VT) Size() (w, h int) { return v.w, v.h }

// Cell returns the cell at (x, y) of the active screen.
func (v *VT) Cell(x, y int) Cell {
	v.mu.Lock()
	defer v.mu.Unlock()
	if y < 0 || y >= v.h || x < 0 || x >= v.w {
		return Cell{}
	}
	return v.cur.cells[y][x]
}

// Row returns the text of row y exactly as displayed (wide graphemes once,
// trailing blanks kept).
func (v *VT) Row(y int) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if y < 0 || y >= v.h {
		return ""
	}
	var b strings.Builder
	for _, c := range v.cur.cells[y] {
		b.WriteString(c.Text)
	}
	return b.String()
}

// Lines returns every row with trailing spaces trimmed.
func (v *VT) Lines() []string {
	out := make([]string, v.h)
	for y := range out {
		out[y] = strings.TrimRight(v.Row(y), " ")
	}
	return out
}

// String returns the screen as text: rows with trailing spaces trimmed,
// joined by newlines, trailing empty rows dropped.
func (v *VT) String() string {
	ls := v.Lines()
	for len(ls) > 0 && ls[len(ls)-1] == "" {
		ls = ls[:len(ls)-1]
	}
	return strings.Join(ls, "\n")
}

// Cursor returns the cursor position (0-based).
func (v *VT) Cursor() (x, y int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.x, v.y
}

// CursorVisible reports DECTCEM (mode 25).
func (v *VT) CursorVisible() bool { return v.Mode(25) }

// CursorShape returns the last DECSCUSR value (0 default, 2 block, 6 bar…).
func (v *VT) CursorShape() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cursorShape
}

// AltScreen reports whether the alternate screen is active.
func (v *VT) AltScreen() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cur == v.alt
}

// Mode reports a DEC private mode (e.g. 7 autowrap, 2004 bracketed paste,
// 1000/1006 mouse).
func (v *VT) Mode(m int) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.modes[m]
}

// Clipboard returns the last text set with OSC 52.
func (v *VT) Clipboard() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.clipboard
}

// SyncBalanced reports whether every synchronized-update begin (?2026h) was
// matched by an end, and how many frames were sent.
func (v *VT) SyncBalanced() (balanced bool, frames int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.syncBegins == v.syncEnds, v.syncBegins
}

// Style returns the style of the cell at (x, y).
func (v *VT) Style(x, y int) theme.Style { return v.Cell(x, y).Style }
