package term

import (
	"strings"

	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
)

// Emitting rows for terminals that reverse right-to-left runs themselves
// (bidi=runs, DESIGN.md §6). The transform is applied last, on whole rows
// as they are sent, so everything drawn over the text (truncation,
// selections, cursor lines, overlays, highlights) is already in place: only
// the text of each run moves, every cell keeps its own style.

// Face bits for text.RunCell: bold and italic select another font face, so
// a change of either ends a terminal run.
const (
	faceBold   = 1
	faceItalic = 2
)

func runFace(st theme.Style) uint8 {
	var f uint8
	if st.Attrs&theme.Bold != 0 {
		f |= faceBold
	}
	if st.Attrs&theme.Italic != 0 {
		f |= faceItalic
	}
	return f
}

// runsRow returns the row of cells as they must be emitted with BidiRuns:
// cells itself when nothing changes, else a copy in s.emit with the text of
// every right-to-left run reversed.
func (s *Screen) runsRow(cells []Cell) []Cell {
	found := false
	for i := range cells {
		if t := cells[i].Text; t != "" && t[0] >= 0xD6 && text.MayHaveRTLRuns(t) {
			found = true
			break
		}
	}
	if !found {
		return cells
	}
	s.runRow = s.runRow[:0]
	for _, c := range cells {
		g := c.Text
		if c.Width != 1 {
			g = "" // wide cells and their continuations never join a run
		}
		s.runRow = append(s.runRow, text.RunCell{Text: g, Face: runFace(c.Style)})
	}
	if !text.ReverseRTLRuns(s.runRow) {
		return cells
	}
	s.emit = append(s.emit[:0], cells...)
	for i, rc := range s.runRow {
		if s.emit[i].Width == 1 {
			s.emit[i].Text = rc.Text
		}
	}
	return s.emit
}

// runsSpans is runsRow for a line of styled spans (AppendSpans).
func runsSpans(spans []text.Span[theme.Style]) []text.Span[theme.Style] {
	found := false
	for _, sp := range spans {
		if text.MayHaveRTLRuns(sp.Text) {
			found = true
			break
		}
	}
	if !found {
		return spans
	}
	type cell struct {
		g  string
		st theme.Style
	}
	var cells []cell
	for _, sp := range spans {
		text.EachGrapheme(sp.Text, func(g string, w int) bool {
			if w == 0 && len(g) > 1 && len(cells) > 0 {
				cells[len(cells)-1].g += g // an orphan mark rides on its base
				return true
			}
			cells = append(cells, cell{g, sp.Style}) // wide ones never join a run (below)
			return true
		})
	}
	row := make([]text.RunCell, len(cells))
	for i, c := range cells {
		row[i] = text.RunCell{Text: c.g, Face: runFace(c.st)}
		if text.GraphemeWidth(c.g) != 1 {
			row[i].Text = ""
		}
	}
	if !text.ReverseRTLRuns(row) {
		return spans
	}
	out := make([]text.Span[theme.Style], 0, len(spans))
	for i, c := range cells {
		g := c.g
		if row[i].Text != "" {
			g = row[i].Text
		}
		if n := len(out); n > 0 && out[n-1].Style == c.st {
			out[n-1].Text += g
			continue
		}
		out = append(out, text.Span[theme.Style]{Text: g, Style: c.st})
	}
	return out
}

// RunsLine applies the bidi=runs transform to one line of output that is
// already serialised (text in visual order with SGR and OSC sequences, no
// newline): the text of each right-to-left run is reversed, while every
// escape sequence stays before the same cell, so colours and attributes
// stay where they were. Bold and italic are tracked from the SGR sequences
// because they end runs. Lines without right-to-left text are returned
// unchanged. It is for writers that compose lines from styled fragments
// (astrolabe's CLI output); Screen and AppendSpans apply the transform
// themselves when Encoder.BidiRuns is set.
func RunsLine(s string) string {
	if !text.MayHaveRTLRuns(s) {
		return s
	}
	type cell struct {
		pre string // escape sequences emitted before this cell
		g   string
	}
	var (
		cells []cell
		row   []text.RunCell
		face  uint8
		pend  = 0 // start of the escapes not yet attached to a cell
	)
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			j := escapeEnd(s, i)
			if j-i >= 3 && s[i+1] == '[' && s[j-1] == 'm' {
				face = sgrFace(face, s[i+2:j-1])
			}
			i = j
			continue
		}
		k := strings.IndexByte(s[i:], 0x1b)
		if k < 0 {
			k = len(s)
		} else {
			k += i
		}
		pos := i
		text.EachGrapheme(s[i:k], func(g string, w int) bool {
			start := pos
			pos += len(g)
			if w == 0 && len(g) > 1 && len(cells) > 0 && pend == start {
				cells[len(cells)-1].g += g
				return true
			}
			rt := g
			if w != 1 {
				rt = ""
			}
			cells = append(cells, cell{pre: s[pend:start], g: g})
			row = append(row, text.RunCell{Text: rt, Face: face})
			pend = pos
			return true
		})
		i = k
	}
	// Combining marks appended above must be part of the run cell's text.
	for i := range row {
		if row[i].Text != "" {
			row[i].Text = cells[i].g
		}
	}
	if !text.ReverseRTLRuns(row) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i, c := range cells {
		b.WriteString(c.pre)
		if row[i].Text != "" {
			b.WriteString(row[i].Text)
		} else {
			b.WriteString(c.g)
		}
	}
	b.WriteString(s[pend:])
	return b.String()
}

// escapeEnd returns the index after the escape sequence starting at s[i]
// (ESC): CSI up to its final byte, OSC/DCS/APC/PM up to BEL or ST, else the
// two-byte sequence.
func escapeEnd(s string, i int) int {
	if i+1 >= len(s) {
		return len(s)
	}
	switch s[i+1] {
	case '[':
		j := i + 2
		for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
			j++
		}
		if j < len(s) {
			j++ // final byte
		}
		return j
	case ']', 'P', '_', '^':
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
		return len(s)
	}
	return i + 2
}

// sgrFace applies the SGR parameters params ("0;1;38;2;1;2;3") to the bold
// and italic bits of face.
func sgrFace(face uint8, params string) uint8 {
	ps := strings.Split(params, ";")
	for k := 0; k < len(ps); k++ {
		switch ps[k] {
		case "", "0":
			face = 0
		case "1":
			face |= faceBold
		case "22":
			face &^= faceBold
		case "3":
			face |= faceItalic
		case "23":
			face &^= faceItalic
		case "38", "48", "58":
			// Extended colour: skip its arguments (5;n or 2;r;g;b).
			if k+1 < len(ps) {
				switch ps[k+1] {
				case "5":
					k += 2
				case "2":
					k += 4
				}
			}
		}
	}
	return face
}
