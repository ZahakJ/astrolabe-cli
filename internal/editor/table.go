package editor

import (
	"strings"

	"github.com/ZahakJ/folio/internal/text"
)

// mdTable is a pipe table around the cursor: lines start..end, the second
// of which is the delimiter row.
type mdTable struct {
	start, end int
	prefix     string // indentation or quote markers before the rows
	outer      bool   // rows start with a pipe
	rows       [][]string
	align      []byte // 'l', 'r', 'c' or 0 per column
	ncols      int
}

// tableAt finds the table containing line l (outside code).
func (e *Editor) tableAt(l int) (*mdTable, bool) {
	b := e.buf
	if !isPipeRow(b.line(l)) {
		return nil, false
	}
	start, end := l, l
	for start > 0 && isPipeRow(b.line(start-1)) && !e.inCode(start-1) {
		start--
	}
	for end+1 < b.lineCount() && isPipeRow(b.line(end+1)) && !e.inCode(end+1) {
		end++
	}
	// the header is the row just above the first delimiter row
	d := -1
	for i := start; i <= end; i++ {
		if isDelimRow(b.line(i)) {
			d = i
			break
		}
	}
	if d <= start {
		return nil, false
	}
	start = d - 1
	if l < start {
		return nil, false
	}
	t := &mdTable{start: start, end: end}
	head := b.line(start)
	pre := tablePrefix(head)
	t.prefix = pre
	t.outer = strings.HasPrefix(strings.TrimLeft(head[len(pre):], " "), "|")
	for i := start; i <= end; i++ {
		s := b.line(i)
		if strings.HasPrefix(s, pre) {
			s = s[len(pre):]
		}
		cells := splitCells(s)
		t.rows = append(t.rows, cells)
		if len(cells) > t.ncols {
			t.ncols = len(cells)
		}
	}
	t.align = make([]byte, t.ncols)
	for c, cell := range t.rows[1] {
		cell = strings.TrimSpace(cell)
		l, r := strings.HasPrefix(cell, ":"), strings.HasSuffix(cell, ":")
		switch {
		case l && r:
			t.align[c] = 'c'
		case r:
			t.align[c] = 'r'
		case l:
			t.align[c] = 'l'
		}
	}
	for i := range t.rows {
		for len(t.rows[i]) < t.ncols {
			t.rows[i] = append(t.rows[i], "")
		}
	}
	return t, true
}

// tablePrefix returns the quote markers and indentation before a row.
func tablePrefix(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '>') {
		i++
	}
	return s[:i]
}

// splitCellsRaw splits a row on unescaped pipes outside code spans and
// wikilinks, without trimming.
func splitCellsRaw(s string) []string {
	var cells []string
	start := 0
	inCode := 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\':
			i++
		case s[i] == '`':
			n := 0
			for i+n < len(s) && s[i+n] == '`' {
				n++
			}
			if inCode == 0 {
				inCode = n
			} else if inCode == n {
				inCode = 0
			}
			i += n - 1
		case inCode == 0 && strings.HasPrefix(s[i:], "[["):
			if j := strings.Index(s[i:], "]]"); j > 0 {
				i += j + 1
			}
		case s[i] == '|' && inCode == 0:
			cells = append(cells, s[start:i])
			start = i + 1
		}
	}
	return append(cells, s[start:])
}

// splitCells returns the trimmed cells of a row, dropping the empty edge
// cells created by outer pipes.
func splitCells(s string) []string {
	t := strings.TrimSpace(s)
	cells := splitCellsRaw(t)
	if len(cells) > 1 && strings.HasPrefix(t, "|") {
		cells = cells[1:]
	}
	if len(cells) > 1 && strings.HasSuffix(t, "|") && !strings.HasSuffix(t, `\|`) {
		cells = cells[:len(cells)-1]
	}
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// format renders the table with every column padded to its widest cell
// (display width, so wide and combining characters line up) and the
// delimiter row regenerated from the alignments.
func (t *mdTable) format() []string {
	widths := make([]int, t.ncols)
	for i, r := range t.rows {
		if i == 1 {
			continue
		}
		for c, cell := range r {
			if w := text.Width(cell); w > widths[c] {
				widths[c] = w
			}
		}
	}
	for c := range widths {
		min := 3
		if t.align[c] == 'c' {
			min = 5
		} else if t.align[c] != 0 {
			min = 4
		}
		if widths[c] < min {
			widths[c] = min
		}
	}
	out := make([]string, len(t.rows))
	for i, r := range t.rows {
		var sb strings.Builder
		sb.WriteString(t.prefix)
		if t.outer {
			sb.WriteString("| ")
		}
		for c := 0; c < t.ncols; c++ {
			if c > 0 {
				sb.WriteString(" | ")
			}
			w := widths[c]
			if i == 1 {
				sb.WriteString(delimCell(t.align[c], w))
				continue
			}
			cell := r[c]
			switch t.align[c] {
			case 'r':
				sb.WriteString(text.PadLeft(cell, w))
			case 'c':
				sb.WriteString(text.Center(cell, w))
			default:
				if c == t.ncols-1 && !t.outer {
					sb.WriteString(cell)
				} else {
					sb.WriteString(text.PadRight(cell, w))
				}
			}
		}
		if t.outer {
			sb.WriteString(" |")
		}
		out[i] = sb.String()
		if !t.outer {
			out[i] = strings.TrimRight(out[i], " ")
		}
	}
	return out
}

func delimCell(a byte, w int) string {
	switch a {
	case 'l':
		return ":" + strings.Repeat("-", w-1)
	case 'r':
		return strings.Repeat("-", w-1) + ":"
	case 'c':
		return ":" + strings.Repeat("-", w-2) + ":"
	}
	return strings.Repeat("-", w)
}

// cellIndex returns the index of the cell containing byte col of a row.
func cellIndex(s, prefix string, outer bool, col int) int {
	if col > len(s) {
		col = len(s)
	}
	body := s
	off := 0
	if strings.HasPrefix(s, prefix) {
		body = s[len(prefix):]
		off = len(prefix)
	}
	cells := splitCellsRaw(body)
	idx := 0
	pos := off
	for i, c := range cells {
		end := pos + len(c)
		if col <= end {
			idx = i
			break
		}
		pos = end + 1
		idx = i
	}
	if outer && strings.HasPrefix(strings.TrimLeft(body, " "), "|") {
		idx--
	}
	if idx < 0 {
		idx = 0
	}
	return idx
}

// cellStart returns the byte offset where the cursor goes in cell c of a
// formatted row: after the cell's content (so typing appends to it), or
// one blank in for an empty cell.
func cellStart(s, prefix string, outer bool, c int) int {
	body := s[len(prefix):]
	cells := splitCellsRaw(body)
	if outer {
		c++
	}
	if c >= len(cells) {
		return len(s)
	}
	pos := len(prefix)
	for i := 0; i < c; i++ {
		pos += len(cells[i]) + 1
	}
	cell := cells[c]
	if strings.TrimSpace(cell) == "" {
		return pos + min(1, len(cell))
	}
	return pos + len(strings.TrimRight(cell, " "))
}

// tableNav re-aligns the table and moves to the next (or previous) cell;
// Tab in the last cell adds a row.
func (e *Editor) tableNav(t *mdTable, forward bool) {
	b := e.buf
	row := e.cur.Line - t.start
	col := cellIndex(b.line(e.cur.Line), t.prefix, t.outer, e.cur.Col)
	if col >= t.ncols {
		col = t.ncols - 1
	}
	lines := t.format()
	for i, l := range lines {
		b.setLine(t.start+i, l)
	}
	if forward {
		col++
		if col >= t.ncols {
			col = 0
			row++
			if row == 1 {
				row = 2
			}
			if row >= len(t.rows) {
				empty := &mdTable{prefix: t.prefix, outer: t.outer, ncols: t.ncols, align: t.align,
					rows: [][]string{t.rows[0], t.rows[1], make([]string, t.ncols)}}
				nl := empty.format()[2]
				last := t.start + len(t.rows) - 1
				b.insert(Pos{last, len(b.line(last))}, "\n"+nl)
				row = len(t.rows)
			}
		}
	} else {
		col--
		if col < 0 {
			row--
			if row == 1 {
				row = 0
			}
			if row < 0 {
				row, col = 0, 0
			} else {
				col = t.ncols - 1
			}
		}
	}
	l := t.start + row
	e.cur = Pos{l, cellStart(b.line(l), t.prefix, t.outer, col)}
}
