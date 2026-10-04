package render

import (
	"strings"

	"github.com/rivo/uniseg"

	"github.com/ZahakJ/folio/internal/md"
	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/theme"
)

// MinColumn is the narrowest a shrunk table column may become before the
// table switches to the stacked record layout (DESIGN.md §5).
const MinColumn = 6

// colSep is the cell separator: a hairline with one cell of air each side.
const sepWidth = 3

// MaxWordFloor caps how much a long word raises a column's floor.
const MaxWordFloor = 12

// ColumnFloor is the narrowest column ColumnWidths accepts for a column of
// natural width nat whose longest unbreakable word is word cells: at least
// MinColumn, at least the longest word up to MaxWordFloor (so words are not
// cut mid-way), and never more than the natural width.
func ColumnFloor(nat, word int) int {
	return min(max(1, nat), max(MinColumn, min(word, MaxWordFloor)))
}

// ColumnWidths implements the table width algorithm of DESIGN.md §5. nat
// holds each column's natural (unwrapped) width, floor the narrowest each
// may become (see ColumnFloor; nil means min(nat, MinColumn)), and avail
// the cells the table may use, separators included (sepWidth between
// columns). Widths are natural when the table fits; otherwise cells are
// taken from the widest columns first, so narrow columns keep their natural
// width as long as possible. ok is false when a column would have to fall
// below its floor: the caller should use the stacked record layout.
func ColumnWidths(nat, floor []int, avail int) (widths []int, ok bool) {
	n := len(nat)
	widths = make([]int, n)
	total := sepWidth * max(0, n-1)
	for i, w := range nat {
		widths[i] = max(1, w)
		total += widths[i]
	}
	need := total - avail
	for need > 0 {
		// Find the widest width and how many columns share it, and the
		// next width below it.
		top, second, count := 0, 0, 0
		for _, w := range widths {
			switch {
			case w > top:
				second, top, count = top, w, 1
			case w == top:
				count++
			case w > second:
				second = w
			}
		}
		if top <= 1 {
			break
		}
		// Lower every widest column towards the next width, spreading
		// the reduction evenly.
		step := (top - second) * count
		if step > need {
			step = need
		}
		per, extra := step/count, step%count
		if per == 0 && extra == 0 {
			break
		}
		for i, w := range widths {
			if w == top {
				d := per
				if extra > 0 {
					d++
					extra--
				}
				widths[i] -= d
			}
		}
		need -= step
	}
	if need > 0 {
		return widths, false
	}
	for i, w := range widths {
		f := min(max(1, nat[i]), MinColumn)
		if floor != nil {
			f = floor[i]
		}
		if w < f {
			return widths, false
		}
	}
	return widths, true
}

// tcell is a laid-out table cell.
type tcell struct {
	spans []sp
	nat   int
	word  int // widest unbreakable segment
	dir   text.Direction
}

func (r *renderer) tableCells(row *md.TableRow, a attr) []tcell {
	cells := make([]tcell, len(row.Cells))
	for i, c := range row.Cells {
		spans := r.inlines(c.Inlines, a, nil)
		nat := 0
		for _, ln := range text.WrapSpans(spans, 1<<20, 1<<20) {
			nat = max(nat, spansW(trimSpaces(ln)))
		}
		word := longestWord(text.SpansText(spans))
		cells[i] = tcell{spans: spans, nat: nat, word: word, dir: spanDirection(spans)}
		if text.BaseDirection(text.SpansText(spans)) == text.Neutral {
			cells[i].dir = text.Neutral
		}
	}
	return cells
}

// longestWord is the width of the widest segment of s between line break
// opportunities (UAX #14), trailing blanks excluded.
func longestWord(s string) int {
	w := 0
	state := -1
	for len(s) > 0 {
		var seg string
		seg, s, _, state = uniseg.FirstLineSegmentInString(s, state)
		w = max(w, text.Width(strings.TrimRight(seg, " \n")))
	}
	return w
}

// table renders a GFM table: hairline separators, a bold header with a rule
// under it, no outer verticals. w is the box width; rows may exceed it up to
// the pane width (top-level tables are then centred on the measure).
func (r *renderer) table(t *md.Table, w int, c ctx) []row {
	ncol := len(t.Align)
	if ncol == 0 || t.Header == nil {
		return nil
	}
	body := c.attr(r)
	head := body
	head.st = head.st.With(theme.Bold)
	if h := r.t.Heading; !h.IsDefault() && c.base == nil {
		head.st.FG = r.t.Text
	}
	header := r.tableCells(t.Header, head)
	rows := make([][]tcell, len(t.Rows))
	for i, tr := range t.Rows {
		rows[i] = r.tableCells(tr, body)
	}
	align := append([]md.Align(nil), t.Align...)
	// An RTL table (its first strong header text is RTL) is mirrored:
	// the first column is drawn on the right.
	rtl := false
	for _, hc := range header {
		if hc.dir != text.Neutral {
			rtl = hc.dir == text.RTL
			break
		}
	}
	if rtl {
		reverseCells(header)
		for _, rr := range rows {
			reverseCells(rr)
		}
		for i, j := 0, len(align)-1; i < j; i, j = i+1, j-1 {
			align[i], align[j] = align[j], align[i]
		}
		// The delimiter row's sides are logical in a mirrored table:
		// ":--" means the start of the line, which is the right here.
		for i, a := range align {
			switch a {
			case md.AlignLeft:
				align[i] = md.AlignRight
			case md.AlignRight:
				align[i] = md.AlignLeft
			}
		}
	}
	nat := make([]int, ncol)
	floor := make([]int, ncol)
	for i := range nat {
		nat[i] = header[i].nat
		word := header[i].word
		for _, rr := range rows {
			nat[i] = max(nat[i], rr[i].nat)
			word = max(word, rr[i].word)
		}
		floor[i] = ColumnFloor(nat[i], word)
	}
	// The table may grow past the box into the free pane width.
	avail := w
	top := w == r.measure && r.depth == 0
	if top {
		avail = max(w, r.pane-2)
	} else {
		avail = w + max(0, (r.pane-2)-(r.left+r.measure))
	}
	widths, ok := ColumnWidths(nat, floor, avail)
	if !ok {
		return r.stacked(t, header, rows, w, rtl)
	}
	total := sepWidth * (ncol - 1)
	for _, cw := range widths {
		total += cw
	}
	sep := mk(" "+r.g.VLine+" ", r.hair())
	var out []row
	emit := func(cells []tcell, line int, isHead bool) bool {
		wrapped := false
		lines := make([][][]sp, ncol)
		height := 1
		for i, cell := range cells {
			lines[i] = r.cellLines(cell, widths[i])
			if len(lines[i]) > 1 {
				wrapped = true
			}
			height = max(height, len(lines[i]))
		}
		for li := 0; li < height; li++ {
			var spans []sp
			for i := range cells {
				if i > 0 {
					spans = append(spans, sep)
				}
				var ln []sp
				if li < len(lines[i]) {
					ln = lines[i][li]
				}
				spans = append(spans, alignCell(ln, widths[i], align[i], cells[i].dir)...)
			}
			if total < w {
				spans = trimSpaces(spans)
			}
			out = append(out, row{spans: trimTrail(spans), src0: line, src1: line, kind: KindTable, blockID: t.ID, rtl: rtl})
		}
		return wrapped
	}
	ruleRow := func(line int) row {
		var spans []sp
		for i, cw := range widths {
			if i > 0 {
				spans = append(spans, mk(r.g.Rule+r.g.Cross+r.g.Rule, r.hair()))
			}
			spans = append(spans, mk(strings.Repeat(r.g.Rule, cw), r.hair()))
		}
		return row{spans: spans, src0: line, src1: line, kind: KindTable, blockID: t.ID}
	}
	emit(header, t.Header.Line, true)
	out = append(out, ruleRow(t.Header.Line+1))
	// When any body row wraps, rows are separated by hairlines so wrapped
	// cells read as one record.
	anyWrap := false
	for i, rr := range rows {
		for j, cell := range rr {
			if cell.nat > widths[j] {
				anyWrap = true
			}
		}
		_ = i
	}
	for i, rr := range rows {
		if i > 0 && anyWrap {
			out = append(out, ruleRow(t.Rows[i].Line))
		}
		emit(rr, t.Rows[i].Line, false)
	}
	out[0].src0 = t.StartLine
	// Placement: a table wider than the box is centred on the measure at
	// the top level (shifting left into the margin), else it overflows to
	// the right.
	if total > w && top {
		// A small overflow simply extends into the right margin; a larger
		// one centres the table on the measure. Either way it stays one
		// cell clear of both pane edges.
		over := total - w
		shift := 0
		if over > 4 {
			shift = (over + 1) / 2
		}
		if end := r.left - shift + total; end > r.pane-1 {
			shift += end - (r.pane - 1)
		}
		shift = max(0, min(shift, r.left-1))
		for i := range out {
			out[i].out = shift
		}
	} else if total < w && rtl {
		for i := range out {
			out[i].spans = append([]sp{mk(spaces(w-total), theme.Style{})}, out[i].spans...)
		}
	}
	return out
}

// trimTrail removes trailing plain spaces (cells pad with spaces).
func trimTrail(spans []sp) []sp {
	return trimSpaces(spans)
}

func reverseCells(c []tcell) {
	for i, j := 0, len(c)-1; i < j; i, j = i+1, j-1 {
		c[i], c[j] = c[j], c[i]
	}
}

// cellLines wraps a cell to w cells and finishes each line (bidi).
func (r *renderer) cellLines(c tcell, w int) [][]sp {
	dir := c.dir
	if dir == text.Neutral {
		dir = text.LTR
	}
	var out [][]sp
	for _, ln := range text.WrapSpans(c.spans, w, w) {
		ln = trimSpaces(ln)
		out = append(out, r.bidiLine(ln, dir))
	}
	return out
}

// alignCell pads a cell line to w cells. Unaligned RTL cells sit right.
func alignCell(ln []sp, w int, a md.Align, dir text.Direction) []sp {
	gap := w - spansW(ln)
	if gap <= 0 {
		return ln
	}
	if a == md.AlignNone && dir == text.RTL {
		a = md.AlignRight
	}
	var l, rgt int
	switch a {
	case md.AlignRight:
		l = gap
	case md.AlignCenter:
		l = gap / 2
		rgt = gap - l
	default:
		rgt = gap
	}
	out := make([]sp, 0, len(ln)+2)
	if l > 0 {
		out = append(out, mk(spaces(l), theme.Style{}))
	}
	out = append(out, ln...)
	if rgt > 0 {
		out = append(out, mk(spaces(rgt), theme.Style{}))
	}
	return out
}

// stacked renders a table too narrow for its columns as records: one group
// of "header  value" lines per row, groups separated by a short hairline.
func (r *renderer) stacked(t *md.Table, header []tcell, rows [][]tcell, w int, rtl bool) []row {
	// The key column: as wide as the widest header, but no more than a
	// third of the box unless a single header word needs it (up to half).
	hw, word := 0, 0
	for _, h := range header {
		hw = max(hw, h.nat)
		word = max(word, h.word)
	}
	hw = min(hw, max(4, w/3, min(word, w/2)))
	valW := max(1, w-hw-2)
	keySt := r.muted().With(theme.Bold)
	if r.t.Muted.IsDefault() {
		keySt = theme.Style{Attrs: theme.Bold}
	}
	var out []row
	for i, rr := range rows {
		line := t.Rows[i].Line
		if i > 0 {
			out = append(out, row{spans: []sp{mk(strings.Repeat(r.g.Rule, min(w, 3*text.Width(r.g.Rule))), r.hair())},
				src0: line, src1: line, kind: KindTable, blockID: t.ID})
		}
		for j, cell := range rr {
			// The key: the header text, plain, wrapped within its column.
			key := strings.ReplaceAll(text.SpansText(header[j].spans), "\n", " ")
			keys := text.Wrap(key, hw)
			vals := r.cellLines(cell, valW)
			if len(vals) == 0 {
				vals = [][]sp{nil}
			}
			for k := 0; k < max(len(keys), len(vals)); k++ {
				kt := ""
				if k < len(keys) {
					kt = keys[k]
				}
				var v []sp
				if k < len(vals) {
					v = vals[k]
				}
				var spans []sp
				if rtl {
					spans = append(spans, alignCell(v, valW, md.AlignRight, text.RTL)...)
					spans = append(spans, mk("  ", theme.Style{}), mk(text.PadLeft(kt, hw), keySt))
				} else {
					spans = append(spans, mk(text.PadRight(kt, hw), keySt), mk("  ", theme.Style{}))
					spans = append(spans, v...)
				}
				out = append(out, row{spans: trimSpaces(spans), src0: line, src1: line, kind: KindTable, blockID: t.ID, rtl: rtl})
			}
		}
	}
	if len(out) > 0 {
		out[0].src0 = t.StartLine
	}
	return out
}
