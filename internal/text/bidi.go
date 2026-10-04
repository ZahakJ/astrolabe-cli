package text

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/bidi"
)

// Direction is a paragraph or character direction.
type Direction int

// Directions. Neutral means "no strong character": callers usually treat it
// as LTR, or inherit the direction of the surrounding block.
const (
	LTR Direction = iota
	RTL
	Neutral
)

// String returns "ltr", "rtl" or "neutral".
func (d Direction) String() string {
	switch d {
	case LTR:
		return "ltr"
	case RTL:
		return "rtl"
	}
	return "neutral"
}

// RuneDirection returns the strong direction of r: LTR for bidi class L,
// RTL for R and AL, Neutral otherwise.
func RuneDirection(r rune) Direction {
	p, _ := bidi.LookupRune(r)
	switch p.Class() {
	case bidi.L:
		return LTR
	case bidi.R, bidi.AL:
		return RTL
	}
	return Neutral
}

// BaseDirection returns the direction of the first strong character of s
// (Unicode bidi rules P2–P3, ignoring isolates), or Neutral if s has none.
func BaseDirection(s string) Direction {
	for _, r := range s {
		if r < 0x80 {
			if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
				return LTR
			}
			continue
		}
		if d := RuneDirection(r); d != Neutral {
			return d
		}
	}
	return Neutral
}

// HasRTL reports whether s contains any right-to-left character (bidi class
// R or AL, or an Arabic number). Lines without one never need reordering
// in an LTR paragraph, so callers can skip the bidi pass.
func HasRTL(s string) bool {
	for _, r := range s {
		if r < 0x590 {
			continue
		}
		p, _ := bidi.LookupRune(r)
		switch p.Class() {
		case bidi.R, bidi.AL, bidi.AN:
			return true
		}
	}
	return false
}

// BidiLine is the result of reordering one display line of grapheme
// clusters. All slices are indexed per cluster.
type BidiLine struct {
	// Levels holds the resolved embedding level of each cluster in logical
	// order; odd levels are right-to-left.
	Levels []uint8
	// VisualToLogical[v] is the logical index of the cluster drawn at
	// visual position v (left to right).
	VisualToLogical []int
	// LogicalToVisual is the inverse permutation.
	LogicalToVisual []int
}

// IsRTL reports whether logical cluster i is drawn right-to-left (and so
// should be mirrored if it is a mirrored character).
func (b BidiLine) IsRTL(i int) bool { return b.Levels[i]%2 == 1 }

// Reorder applies the Unicode bidi algorithm (UAX #9) to one line given as
// grapheme clusters in logical order, with paragraph direction base (Neutral
// = first strong character, defaulting to LTR). Levels are resolved with
// golang.org/x/text/unicode/bidi; reordering follows rule L2, and trailing
// whitespace takes the paragraph level (L1).
//
// Explicit directional embeddings, overrides and isolates (U+202A–U+202E,
// U+2066–U+2069) are ignored: they are treated as boundary neutrals. Notes
// rarely contain them, and ignoring them keeps the result well defined.
func Reorder(clusters []string, base Direction) BidiLine {
	n := len(clusters)
	out := BidiLine{
		Levels:          make([]uint8, n),
		VisualToLogical: make([]int, n),
		LogicalToVisual: make([]int, n),
	}
	if base == Neutral {
		base = BaseDirection(strings.Join(clusters, ""))
		if base == Neutral {
			base = LTR
		}
	}
	para := uint8(0)
	if base == RTL {
		para = 1
	}
	for i := range out.Levels {
		out.Levels[i] = para
	}
	if n > 0 {
		levels := runeLevels(clusters, para)
		ri := 0
		for i, c := range clusters {
			if c == "" { // e.g. the alef slot of a lam-alef ligature
				if i > 0 {
					out.Levels[i] = out.Levels[i-1]
				}
				continue
			}
			out.Levels[i] = levels[ri]
			ri += len([]rune(c))
		}
	}
	order := reorderLevels(out.Levels)
	copy(out.VisualToLogical, order)
	for v, l := range order {
		out.LogicalToVisual[l] = v
	}
	return out
}

// runeLevels resolves an embedding level for every rune of the clusters.
func runeLevels(clusters []string, para uint8) []uint8 {
	var runes []rune
	mark := '‎' // LRM forces paragraph level 0
	if para == 1 {
		mark = '‏' // RLM forces paragraph level 1
	}
	runes = append(runes, mark)
	for _, c := range clusters {
		for _, r := range c {
			runes = append(runes, analysisRune(r))
		}
	}
	levels := make([]uint8, len(runes))
	var p bidi.Paragraph
	if _, err := p.SetString(string(runes)); err == nil {
		if o, err := p.Order(); err == nil {
			for i := 0; i < o.NumRuns(); i++ {
				run := o.Run(i)
				start, end := run.Pos()
				lv := uint8(0)
				if run.Direction() == bidi.RightToLeft {
					lv = 1
				}
				for j := start; j <= end && j < len(levels); j++ {
					levels[j] = lv
				}
			}
		}
	}
	// x/text exposes only run directions; recover the true levels. Without
	// explicit embeddings the only levels are para, para+1 and 2: at
	// paragraph level 1 every even run is level 2; at paragraph level 0 an
	// even character is level 2 exactly when it resolved to a number (EN or
	// AN after rules W1–W7, rule I1).
	if para == 1 {
		for i, lv := range levels {
			if lv == 0 {
				levels[i] = 2
			}
		}
	} else {
		numeric := resolveNumbers(runes)
		for i, lv := range levels {
			if lv == 0 && numeric[i] {
				levels[i] = 2
			}
		}
	}
	return levels[1:]
}

// analysisRune neutralises characters the analysis must not see: explicit
// embedding controls become ZWSP (class BN) and paragraph separators become
// spaces, so a line is always analysed as one paragraph.
func analysisRune(r rune) rune {
	switch {
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return '​'
	case r == '\n' || r == '\r' || r == 0x85 || r == 0x2029 || (r >= 0x1c && r <= 0x1e):
		return ' '
	}
	return r
}

// resolveNumbers runs the weak-type rules W1–W7 over runes (whose first
// rune is the strong paragraph mark) and reports which runes end as EN or AN.
func resolveNumbers(runes []rune) []bool {
	n := len(runes)
	cls := make([]bidi.Class, n)
	for i, r := range runes {
		p, _ := bidi.LookupRune(r)
		cls[i] = p.Class()
	}
	// Indices of characters that survive X9 (BN removed).
	idx := make([]int, 0, n)
	for i, c := range cls {
		if c != bidi.BN {
			idx = append(idx, i)
		}
	}
	t := make([]bidi.Class, len(idx))
	for k, i := range idx {
		t[k] = cls[i]
	}
	// W1: NSM takes the type of the previous character.
	for k := range t {
		if t[k] == bidi.NSM {
			if k == 0 {
				t[k] = bidi.L
			} else {
				t[k] = t[k-1]
			}
		}
	}
	// W2: EN preceded (through weak types) by AL becomes AN. W3: AL → R.
	last := bidi.L
	for k := range t {
		switch t[k] {
		case bidi.L, bidi.R, bidi.AL:
			last = t[k]
		case bidi.EN:
			if last == bidi.AL {
				t[k] = bidi.AN
			}
		}
	}
	for k := range t {
		if t[k] == bidi.AL {
			t[k] = bidi.R
		}
	}
	// W4: a single ES between ENs → EN; a single CS between numbers of the
	// same type → that type.
	for k := 1; k+1 < len(t); k++ {
		switch t[k] {
		case bidi.ES:
			if t[k-1] == bidi.EN && t[k+1] == bidi.EN {
				t[k] = bidi.EN
			}
		case bidi.CS:
			if t[k-1] == bidi.EN && t[k+1] == bidi.EN {
				t[k] = bidi.EN
			} else if t[k-1] == bidi.AN && t[k+1] == bidi.AN {
				t[k] = bidi.AN
			}
		}
	}
	// W5: a sequence of ETs adjacent to an EN becomes EN.
	for k := 0; k < len(t); k++ {
		if t[k] != bidi.ET {
			continue
		}
		e := k
		for e < len(t) && t[e] == bidi.ET {
			e++
		}
		if (k > 0 && t[k-1] == bidi.EN) || (e < len(t) && t[e] == bidi.EN) {
			for j := k; j < e; j++ {
				t[j] = bidi.EN
			}
		}
		k = e - 1
	}
	// W6 (remaining separators become neutral) does not affect the result.
	// W7: EN preceded by strong L (or sos L) becomes L.
	last = bidi.L
	for k := range t {
		switch t[k] {
		case bidi.L, bidi.R:
			last = t[k]
		case bidi.EN:
			if last == bidi.L {
				t[k] = bidi.L
			}
		}
	}
	out := make([]bool, n)
	for k, i := range idx {
		out[i] = t[k] == bidi.EN || t[k] == bidi.AN
	}
	// BN characters take the numeric status of their predecessor.
	for i := 1; i < n; i++ {
		if cls[i] == bidi.BN {
			out[i] = out[i-1]
		}
	}
	return out
}

// reorderLevels implements rule L2: from the highest level down to the
// lowest odd level, reverse every maximal run at that level or above.
// It returns the visual-to-logical permutation.
func reorderLevels(levels []uint8) []int {
	n := len(levels)
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	var hi, lo uint8 = 0, 255
	for _, l := range levels {
		hi = max(hi, l)
		if l%2 == 1 {
			lo = min(lo, l)
		}
	}
	if lo == 255 {
		return order // nothing right-to-left
	}
	for lv := hi; lv >= lo && lv > 0; lv-- {
		for i := 0; i < n; {
			if levels[order[i]] < lv {
				i++
				continue
			}
			j := i
			for j < n && levels[order[j]] >= lv {
				j++
			}
			for a, b := i, j-1; a < b; a, b = a+1, b-1 {
				order[a], order[b] = order[b], order[a]
			}
			i = j
		}
	}
	return order
}

// mirrors maps characters with the Bidi_Mirrored property to their mirror
// image (the common subset of BidiMirroring.txt).
var mirrors = map[rune]rune{}

func init() {
	pairs := "()[]{}<>«»‹›⁅⁆⁽⁾₍₎≤≥≦≧≨≩≪≫≮≯≰≱≲≳≺≻≼≽⊂⊃⊄⊅⊆⊇⊈⊉⊊⊋⊏⊐⊑⊒⊢⊣∈∋∉∌∊∍⟨⟩⟪⟫⟦⟧⟮⟯❨❩❪❫❬❭❮❯❰❱❲❳❴❵⦃⦄⦅⦆⦇⦈⦉⦊⦋⦌⦗⦘〈〉《》「」『』【】〔〕〖〗〘〙〚〛（）［］｛｝＜＞｟｠｢｣"
	rs := []rune(pairs)
	for i := 0; i+1 < len(rs); i += 2 {
		mirrors[rs[i]] = rs[i+1]
		mirrors[rs[i+1]] = rs[i]
	}
}

// Mirror returns the mirrored form of a grapheme cluster drawn right-to-left
// (rule L4): "(" ↔ ")", "«" ↔ "»", "≤" ↔ "≥" and so on. Clusters without a
// mirror are returned unchanged; trailing combining marks are preserved.
func Mirror(g string) string {
	r, size := utf8.DecodeRuneInString(g)
	if m, ok := mirrors[r]; ok {
		return string(m) + g[size:]
	}
	return g
}

// VisualOrder returns cells rearranged into visual (left-to-right display)
// order. cluster extracts each cell's grapheme cluster. It also returns the
// BidiLine so callers can map a cursor and decide which cells to mirror.
// Cells are not modified; apply Mirror to cells where BidiLine.IsRTL holds.
func VisualOrder[T any](cells []T, cluster func(T) string, base Direction) ([]T, BidiLine) {
	cl := make([]string, len(cells))
	for i, c := range cells {
		cl[i] = cluster(c)
	}
	bl := Reorder(cl, base)
	out := make([]T, len(cells))
	for v, l := range bl.VisualToLogical {
		out[v] = cells[l]
	}
	return out, bl
}

// ReorderString returns s (one line) in visual order with mirrored
// characters swapped in right-to-left runs. It does not shape Arabic; see
// Visual for the full display transform.
func ReorderString(s string, base Direction) string {
	if base != RTL && !HasRTL(s) {
		return s
	}
	gs := Graphemes(s)
	cl := make([]string, len(gs))
	for i, g := range gs {
		cl[i] = g.Text
	}
	return joinVisual(cl, base)
}

func joinVisual(cl []string, base Direction) string {
	bl := Reorder(cl, base)
	var b strings.Builder
	for _, l := range bl.VisualToLogical {
		if bl.IsRTL(l) {
			b.WriteString(Mirror(cl[l]))
		} else {
			b.WriteString(cl[l])
		}
	}
	return b.String()
}

// Visual is the complete display transform for one line on a terminal that
// does no bidi itself: Arabic shaping into presentation forms, then bidi
// reordering with mirroring. Lines with no right-to-left content in an LTR
// paragraph are returned unchanged.
func Visual(s string, base Direction) string {
	if base != RTL && !HasRTL(s) {
		return s
	}
	gs := Graphemes(s)
	cl := make([]string, len(gs))
	for i, g := range gs {
		cl[i] = g.Text
	}
	return joinVisual(ShapeClusters(cl), base)
}

// VisualRanges is Visual for a line carrying highlighted byte ranges (search
// matches): the ranges are carried through shaping and reordering, so a
// match stays highlighted on a line that mixes Arabic and Latin text. A
// range may come out split where reordering separates its pieces.
func VisualRanges(s string, ranges [][2]int) (string, [][2]int) {
	if !HasRTL(s) {
		return s, ranges
	}
	gs := Graphemes(s)
	cl := make([]string, len(gs))
	for i, g := range gs {
		cl[i] = g.Text
	}
	shaped := ShapeClusters(cl)
	bl := Reorder(cl, BaseDirection(s))
	inRange := func(off int) bool {
		for _, r := range ranges {
			if off >= r[0] && off < r[1] {
				return true
			}
		}
		return false
	}
	var b strings.Builder
	var out [][2]int
	for _, l := range bl.VisualToLogical {
		piece := shaped[l]
		if piece == "" {
			continue
		}
		if bl.IsRTL(l) {
			piece = Mirror(piece)
		}
		start := b.Len()
		b.WriteString(piece)
		if inRange(gs[l].Offset) {
			if n := len(out); n > 0 && out[n-1][1] == start {
				out[n-1][1] = b.Len()
			} else {
				out = append(out, [2]int{start, b.Len()})
			}
		}
	}
	return b.String(), out
}
