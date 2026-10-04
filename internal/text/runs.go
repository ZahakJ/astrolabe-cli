package text

import (
	"unicode"
	"unicode/utf8"
)

// Terminal runs (DESIGN.md §6, bidi=runs).
//
// Some terminals (kitty) do no bidi but shape text with HarfBuzz one run at
// a time, where a run is a stretch of adjacent cells drawn with the same
// font; a blank cell always ends a run. A run whose first character of a
// real script is Arabic or Hebrew is laid out right to left: the terminal
// reverses its glyphs in place. astrolabe emits lines already in visual order,
// so on such a terminal every right-to-left word would be reversed a second
// time and read mirrored.
//
// RTLRuns finds the stretches of a visual row that such a terminal will
// reverse, so the caller can emit their text pre-reversed and let the
// terminal's reversal restore the visual order. Fonts are not known to
// astrolabe; the model assumes what holds for common setups: right-to-left
// letters (shaped into presentation forms, which usually come from a
// fallback font) do not share a font with ASCII punctuation, digits,
// Latin text, blanks or astrolabe's glyphs, and a change of bold or italic
// selects another face and so ends a run.

// Run classes. Letters and Arabic-script digits come from different fonts
// in common setups (a programming font with base Arabic letters and digits
// but no presentation forms, plus a fallback for the forms), so they form
// separate runs.
const (
	runNone = iota
	runLetters
	runDigits
)

// runClass classifies one cell's grapheme cluster for RTLRuns.
func runClass(g string) int {
	if g == "" || g[0] < 0x80 {
		return runNone
	}
	r, _ := utf8.DecodeRuneInString(g)
	switch {
	case !mayStartRun(r):
		return runNone
	case r >= 0x0660 && r <= 0x0669, r >= 0x06F0 && r <= 0x06F9:
		// Arabic-Indic digits: script Arabic, so a terminal run of them is
		// laid out right to left, though bidi shows them left to right.
		return runDigits
	}
	if !unicode.IsLetter(r) || RuneDirection(r) != RTL {
		return runNone
	}
	return runLetters
}

// RTLRuns calls fn(start, end) for every terminal run of a visual row of n
// cells that a run-reversing terminal lays out right to left: a maximal
// stretch [start, end) of at least two consecutive cells that are all
// strong right-to-left letters (Arabic, presentation forms and lam-alef
// ligatures included, Hebrew; combining marks ride in their base cell's
// text), or all Arabic-script digits, with the same face (bold and italic
// bits). cell(i) returns the grapheme cluster of cell i ("" for an empty
// cell or the second half of a wide one) and its face. A one-cell run is
// skipped: reversing it changes nothing.
func RTLRuns(n int, cell func(i int) (g string, face uint8), fn func(start, end int)) {
	start, cls := -1, runNone
	var face uint8
	end := func(i int) {
		if start >= 0 && i-start >= 2 {
			fn(start, i)
		}
		start, cls = -1, runNone
	}
	for i := 0; i < n; i++ {
		g, f := cell(i)
		c := runClass(g)
		if c == runNone {
			end(i)
			continue
		}
		if start >= 0 && (c != cls || f != face) {
			end(i)
		}
		if start < 0 {
			start, cls, face = i, c, f
		}
	}
	end(n)
}

// RunCell is one cell of a visual row for ReverseRTLRuns.
type RunCell struct {
	// Text is the grapheme cluster ("" for an empty cell or the second
	// half of a wide one).
	Text string
	// Face holds the bold and italic bits: a change ends a terminal run.
	Face uint8
}

// ReverseRTLRuns reverses, in place, the text of every terminal run of row
// (see RTLRuns), leaving faces where they are. It reports whether anything
// changed. Applying it twice restores the row.
func ReverseRTLRuns(row []RunCell) bool {
	changed := false
	RTLRuns(len(row), func(i int) (string, uint8) { return row[i].Text, row[i].Face }, func(s, e int) {
		for a, b := s, e-1; a < b; a, b = a+1, b-1 {
			row[a].Text, row[b].Text = row[b].Text, row[a].Text
		}
		changed = true
	})
	return changed
}

// MayHaveRTLRuns is a fast check that s contains a character that can start
// a terminal run; rows without one need no RTLRuns pass.
func MayHaveRTLRuns(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0xD6 { // ASCII, continuation bytes, and lead bytes below U+0580
			continue
		}
		r, _ := utf8.DecodeRuneInString(s[i:])
		if mayStartRun(r) {
			return true
		}
	}
	return false
}

// mayStartRun reports whether r lies in a block holding right-to-left
// letters or Arabic-script digits.
func mayStartRun(r rune) bool {
	return r >= 0x0590 && r < 0x0900 || // Hebrew, Arabic, Syriac, Thaana, NKo, Samaritan, Mandaic, Arabic Extended
		r >= 0xFB1D && r < 0xFF00 || // Hebrew and Arabic presentation forms
		r >= 0x10800 && r < 0x11000 || r >= 0x1E800 && r < 0x1F000 // historic and other RTL scripts
}

// RunsString applies ReverseRTLRuns to a line of unstyled text in visual
// order (one cell per grapheme cluster, one face), for terminals that
// reverse right-to-left runs themselves. Lines without right-to-left
// letters or Arabic digits are returned unchanged.
func RunsString(s string) string {
	if !MayHaveRTLRuns(s) {
		return s
	}
	gs := Graphemes(s)
	row := make([]RunCell, len(gs))
	for i, g := range gs {
		row[i].Text = g.Text
	}
	if !ReverseRTLRuns(row) {
		return s
	}
	b := make([]byte, 0, len(s))
	for _, c := range row {
		b = append(b, c.Text...)
	}
	return string(b)
}
