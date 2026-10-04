package term

import (
	"io"
	"strconv"
	"unicode/utf8"

	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/theme"
)

// Escape sequences for terminal modes, exported so callers (and tests) can
// emit or recognise them.
const (
	SeqAltScreenOn       = "\x1b[?1049h"
	SeqAltScreenOff      = "\x1b[?1049l"
	SeqCursorHide        = "\x1b[?25l"
	SeqCursorShow        = "\x1b[?25h"
	SeqSyncBegin         = "\x1b[?2026h"
	SeqSyncEnd           = "\x1b[?2026l"
	SeqAutowrapOff       = "\x1b[?7l"
	SeqAutowrapOn        = "\x1b[?7h"
	SeqBracketedPasteOn  = "\x1b[?2004h"
	SeqBracketedPasteOff = "\x1b[?2004l"
	SeqMouseOn           = "\x1b[?1000h\x1b[?1006h" // press/release/wheel, SGR encoding
	SeqMouseOff          = "\x1b[?1006l\x1b[?1000l"
	SeqClear             = "\x1b[H\x1b[2J"
	SeqKittyKeysOn       = "\x1b[>1u" // kitty keyboard: disambiguate escape codes
	SeqKittyKeysOff      = "\x1b[<u"
)

// CursorShape is the terminal cursor's shape (DECSCUSR).
type CursorShape int

// Cursor shapes: the terminal's default, a steady block (Normal mode), a
// steady bar (Insert mode) and a steady underline.
const (
	CursorDefault CursorShape = iota
	CursorBlock
	CursorBar
	CursorUnderline
)

func (c CursorShape) seq() string {
	switch c {
	case CursorBlock:
		return "\x1b[2 q"
	case CursorBar:
		return "\x1b[6 q"
	case CursorUnderline:
		return "\x1b[4 q"
	}
	return "\x1b[0 q"
}

// Rect is a rectangle of cells.
type Rect struct{ X, Y, W, H int }

// Contains reports whether (x, y) lies inside r.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H
}

// Intersect returns the overlap of r and o (zero size if none).
func (r Rect) Intersect(o Rect) Rect {
	x0, y0 := max(r.X, o.X), max(r.Y, o.Y)
	x1, y1 := min(r.X+r.W, o.X+o.W), min(r.Y+r.H, o.Y+o.H)
	if x1 <= x0 || y1 <= y0 {
		return Rect{X: x0, Y: y0}
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// Cell is one screen cell. A wide grapheme occupies its cell (Width 2) and
// the next one, a continuation with Text "" and Width 0. Zero-width marks
// are part of their base cell's Text.
type Cell struct {
	Text  string
	Width int
	Style theme.Style
}

func blank(st theme.Style) Cell { return Cell{Text: " ", Width: 1, Style: st} }

// Screen is a double-buffered cell grid. Draw into it with the Put/Fill
// helpers, then Flush to send only the cells that changed since the last
// Flush, in a single write wrapped in synchronized-update marks. A Screen
// is not safe for concurrent use.
type Screen struct {
	out   io.Writer
	enc   Encoder
	w, h  int
	cells []Cell
	prev  []Cell

	cursorX, cursorY int
	cursorVisible    bool
	cursorShape      CursorShape

	// What the terminal currently shows, to skip redundant output.
	termCursorVisible bool
	termCursorShape   CursorShape
	termCursorX       int
	termCursorY       int
	invalid           bool

	buf  []byte
	body []byte
}

// NewScreen returns a w×h screen writing to out with encoder enc. The
// first Flush redraws everything.
func NewScreen(out io.Writer, w, h int, enc Encoder) *Screen {
	s := &Screen{out: out, enc: enc, termCursorVisible: true, termCursorX: -1, termCursorShape: -1}
	s.Resize(w, h)
	return s
}

// Size returns the screen size in cells.
func (s *Screen) Size() (w, h int) { return s.w, s.h }

// Encoder returns the screen's encoder.
func (s *Screen) Encoder() Encoder { return s.enc }

// SetEncoder changes the encoder (e.g. after a theme or profile change) and
// schedules a full redraw.
func (s *Screen) SetEncoder(e Encoder) { s.enc = e; s.invalid = true }

// Resize changes the size, clearing the contents and scheduling a full
// redraw.
func (s *Screen) Resize(w, h int) {
	w, h = max(w, 0), max(h, 0)
	s.w, s.h = w, h
	s.cells = make([]Cell, w*h)
	s.prev = make([]Cell, w*h)
	for i := range s.cells {
		s.cells[i] = blank(theme.Style{})
	}
	s.invalid = true
}

// Invalidate schedules a full redraw on the next Flush (after the terminal
// was disturbed, e.g. by an external program or a resume).
func (s *Screen) Invalidate() {
	s.invalid = true
	// Assume the worst about the terminal's cursor state.
	s.termCursorX, s.termCursorVisible, s.termCursorShape = -1, true, -1
}

// Clear fills the whole screen with blanks in style st.
func (s *Screen) Clear(st theme.Style) {
	b := blank(st)
	for i := range s.cells {
		s.cells[i] = b
	}
}

// Cell returns the cell at (x, y); out-of-range positions return a blank.
func (s *Screen) Cell(x, y int) Cell {
	if x < 0 || y < 0 || x >= s.w || y >= s.h {
		return blank(theme.Style{})
	}
	return s.cells[y*s.w+x]
}

// SetCell draws grapheme g with style st at (x, y) and returns the number
// of cells it occupies (0 if clipped). A wide grapheme that does not fit
// before the right edge is drawn as a blank. Overwriting half of a wide
// grapheme blanks its other half.
func (s *Screen) SetCell(x, y int, g string, st theme.Style) int {
	if y < 0 || y >= s.h || x < 0 || x >= s.w {
		return 0
	}
	w := text.GraphemeWidth(g)
	if w == 0 {
		return 0
	}
	if w > 2 {
		w = 2
	}
	if w == 2 && x+1 >= s.w {
		g, w = " ", 1
	}
	row := y * s.w
	s.breakWide(x, y)
	if w == 2 {
		s.breakWide(x+1, y)
		s.cells[row+x] = Cell{Text: g, Width: 2, Style: st}
		s.cells[row+x+1] = Cell{Text: "", Width: 0, Style: st}
	} else {
		s.cells[row+x] = Cell{Text: g, Width: 1, Style: st}
	}
	return w
}

// breakWide blanks the other half of a wide grapheme overlapping (x, y).
func (s *Screen) breakWide(x, y int) {
	row := y * s.w
	c := s.cells[row+x]
	switch {
	case c.Width == 0 && x > 0:
		lead := &s.cells[row+x-1]
		if lead.Width == 2 {
			*lead = blank(lead.Style)
		}
	case c.Width == 2 && x+1 < s.w:
		s.cells[row+x+1] = blank(c.Style)
	}
}

// PutString draws s at (x, y) with style st, clipped to the screen, and
// returns the column after the last cell drawn. See PutStringClip.
func (s *Screen) PutString(x, y int, str string, st theme.Style) int {
	return s.PutStringClip(x, y, str, st, s.w)
}

// PutStringClip draws str from column x, stopping before column maxX (or the
// screen edge). Grapheme clusters are kept whole; zero-width clusters
// (orphan combining marks, ZWJ) attach to the previous cell; control
// characters are skipped except tab, which draws as one space (expand tabs
// beforehand for real tab stops). It returns the column after the last cell
// drawn.
func (s *Screen) PutStringClip(x, y int, str string, st theme.Style, maxX int) int {
	maxX = min(maxX, s.w)
	if y < 0 || y >= s.h {
		return x
	}
	text.EachGrapheme(str, func(g string, w int) bool {
		if x >= maxX {
			return false
		}
		if w == 0 {
			if g == "\t" {
				g, w = " ", 1
			} else {
				if len(g) == 1 || x <= 0 || x > s.w {
					return true // control character, or nothing to attach to
				}
				i := y*s.w + x - 1
				if s.cells[i].Width == 0 && x >= 2 {
					i--
				}
				s.cells[i].Text += g
				return true
			}
		}
		if x+w > maxX {
			// A wide grapheme straddling the clip edge: fill with blanks.
			for ; x < maxX; x++ {
				if x >= 0 {
					s.SetCell(x, y, " ", st)
				}
			}
			return false
		}
		if x < 0 {
			x += w
			return true
		}
		x += s.SetCell(x, y, g, st)
		return true
	})
	return x
}

// PutSpans draws styled spans from column x up to maxX and returns the
// column after the last cell drawn.
func (s *Screen) PutSpans(x, y int, spans []text.Span[theme.Style], maxX int) int {
	for _, sp := range spans {
		x = s.PutStringClip(x, y, sp.Text, sp.Style, maxX)
		if x >= maxX {
			break
		}
	}
	return x
}

// Fill fills rectangle r with grapheme g (a single-cell grapheme; "" means
// a space) in style st.
func (s *Screen) Fill(r Rect, g string, st theme.Style) {
	if g == "" || text.GraphemeWidth(g) != 1 {
		g = " "
	}
	r = r.Intersect(Rect{0, 0, s.w, s.h})
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			s.SetCell(x, y, g, st)
		}
	}
}

// Restyle replaces the style of every cell in r with fn(style), keeping the
// text — for cursor-line grounds, selections and search highlights drawn
// over already rendered content.
func (s *Screen) Restyle(r Rect, fn func(theme.Style) theme.Style) {
	r = r.Intersect(Rect{0, 0, s.w, s.h})
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			c := &s.cells[y*s.w+x]
			c.Style = fn(c.Style)
		}
	}
}

// HLine draws a horizontal line of glyph from (x, y), n cells long.
func (s *Screen) HLine(x, y, n int, glyph string, st theme.Style) {
	for i := 0; i < n; i++ {
		s.SetCell(x+i, y, glyph, st)
	}
}

// VLine draws a vertical line of glyph from (x, y), n cells tall.
func (s *Screen) VLine(x, y, n int, glyph string, st theme.Style) {
	for i := 0; i < n; i++ {
		s.SetCell(x, y+i, glyph, st)
	}
}

// Box draws a hairline border around r using the glyph set's corners and
// lines, and fills the inside with blanks in st (a raised panel). Rects
// smaller than 2×2 are filled only.
func (s *Screen) Box(r Rect, g theme.Glyphs, border, fill theme.Style) {
	s.Fill(r, " ", fill)
	if r.W < 2 || r.H < 2 {
		return
	}
	x1, y1 := r.X+r.W-1, r.Y+r.H-1
	s.HLine(r.X+1, r.Y, r.W-2, g.Rule, border)
	s.HLine(r.X+1, y1, r.W-2, g.Rule, border)
	s.VLine(r.X, r.Y+1, r.H-2, g.VLine, border)
	s.VLine(x1, r.Y+1, r.H-2, g.VLine, border)
	s.SetCell(r.X, r.Y, g.CornerTL, border)
	s.SetCell(x1, r.Y, g.CornerTR, border)
	s.SetCell(r.X, y1, g.CornerBL, border)
	s.SetCell(x1, y1, g.CornerBR, border)
}

// SetCursor places the terminal cursor at (x, y) for the next Flush.
func (s *Screen) SetCursor(x, y int) { s.cursorX, s.cursorY = x, y }

// ShowCursor shows or hides the terminal cursor from the next Flush.
func (s *Screen) ShowCursor(v bool) { s.cursorVisible = v }

// SetCursorShape sets the cursor shape from the next Flush.
func (s *Screen) SetCursorShape(c CursorShape) { s.cursorShape = c }

// Cursor returns the cursor position and visibility set for the next Flush.
func (s *Screen) Cursor() (x, y int, visible bool) { return s.cursorX, s.cursorY, s.cursorVisible }

// unstableWidth reports clusters whose width terminals disagree on (emoji
// sequences); after drawing one the terminal cursor position is re-sent.
func unstableWidth(g string) bool {
	if len(g) < 4 || utf8.RuneCountInString(g) < 2 {
		return false
	}
	for _, r := range g {
		if r == 0xFE0F || r == 0x200D || r >= 0x1F000 {
			return true
		}
	}
	return false
}

func appendCUP(b []byte, x, y int) []byte {
	b = append(b, "\x1b["...)
	b = strconv.AppendInt(b, int64(y+1), 10)
	b = append(b, ';')
	b = strconv.AppendInt(b, int64(x+1), 10)
	return append(b, 'H')
}

// Render appends the bytes Flush would write to dst and commits the frame
// as the new "previous" state. It appends nothing when neither the cells nor
// the cursor changed. Most callers use Flush.
func (s *Screen) Render(dst []byte) []byte {
	body := s.renderCells(s.body[:0])
	s.body = body
	cursorChanged := s.cursorVisible != s.termCursorVisible ||
		(s.cursorVisible && (s.cursorX != s.termCursorX || s.cursorY != s.termCursorY ||
			s.cursorShape != s.termCursorShape))
	if len(body) == 0 && !cursorChanged {
		return dst
	}
	b := append(dst, SeqSyncBegin...)
	if len(body) > 0 && s.termCursorVisible {
		b = append(b, SeqCursorHide...) // no cursor flicker while drawing
	}
	b = append(b, body...)
	if s.cursorVisible {
		b = appendCUP(b, s.cursorX, s.cursorY)
		if s.cursorShape != s.termCursorShape {
			b = append(b, s.cursorShape.seq()...)
			s.termCursorShape = s.cursorShape
		}
		if len(body) > 0 || !s.termCursorVisible {
			b = append(b, SeqCursorShow...)
		}
		s.termCursorX, s.termCursorY = s.cursorX, s.cursorY
	} else if len(body) == 0 && s.termCursorVisible {
		b = append(b, SeqCursorHide...)
	}
	s.termCursorVisible = s.cursorVisible
	return append(b, SeqSyncEnd...)
}

// renderCells appends the changed cells (everything after Invalidate).
func (s *Screen) renderCells(b []byte) []byte {
	if s.invalid {
		b = append(b, Reset...)
		b = append(b, SeqClear...)
	}
	var cur theme.Style
	styled := false
	link := ""
	tx, ty := -1, -1
	for y := 0; y < s.h; y++ {
		row := y * s.w
		for x := 0; x < s.w; x++ {
			c := s.cells[row+x]
			if c.Width == 0 {
				continue // continuation: drawn with its lead
			}
			if !s.invalid && c == s.prev[row+x] &&
				(c.Width == 1 || x+1 >= s.w || s.cells[row+x+1] == s.prev[row+x+1]) {
				continue
			}
			if tx != x || ty != y {
				b = appendCUP(b, x, y)
				tx, ty = x, y
			}
			st := c.Style
			st.Link = ""
			if !styled || st != cur {
				b = s.enc.AppendSGR(b, st)
				cur, styled = st, true
			}
			if c.Style.Link != link && s.enc.Hyperlinks {
				b = s.enc.AppendLink(b, c.Style.Link)
				link = c.Style.Link
			}
			b = append(b, c.Text...)
			tx += c.Width
			if unstableWidth(c.Text) {
				tx = -1
			}
		}
	}
	if link != "" {
		b = s.enc.AppendLink(b, "")
	}
	if styled {
		b = append(b, Reset...)
	}
	copy(s.prev, s.cells)
	s.invalid = false
	return b
}

// Flush writes the changes since the previous Flush in a single write. It
// writes nothing when nothing changed.
func (s *Screen) Flush() error {
	s.buf = s.Render(s.buf[:0])
	if len(s.buf) == 0 {
		return nil
	}
	_, err := s.out.Write(s.buf)
	return err
}
