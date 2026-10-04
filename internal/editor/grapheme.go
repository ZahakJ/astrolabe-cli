package editor

import (
	"unicode"
	"unicode/utf8"

	"github.com/ZahakJ/folio/internal/text"
	"github.com/rivo/uniseg"
)

// tabStop is the display width of a tab stop (DESIGN.md: tabs display as
// spaces to the next multiple of 4).
const tabStop = 4

// clusterCache memoises grapheme-cluster starts for recently used lines,
// so repeated motions over a long line stay linear.
type clusterCache struct {
	entries [4]clusterEntry
	next    int
}

type clusterEntry struct {
	s      string
	starts []int // cluster start offsets, followed by len(s)
	valid  bool
}

func (c *clusterCache) starts(s string) []int {
	for i := range c.entries {
		e := &c.entries[i]
		if e.valid && len(e.s) == len(s) && e.s == s {
			return e.starts
		}
	}
	st := clusterStarts(s)
	e := &c.entries[c.next]
	c.next = (c.next + 1) % len(c.entries)
	*e = clusterEntry{s: s, starts: st, valid: true}
	return st
}

func clusterStarts(s string) []int {
	st := make([]int, 0, len(s)+1)
	if isASCII(s) {
		for i := 0; i < len(s); i++ {
			st = append(st, i)
		}
		return append(st, len(s))
	}
	off := 0
	state := -1
	rest := s
	for len(rest) > 0 {
		var cl string
		cl, rest, _, state = uniseg.FirstGraphemeClusterInString(rest, state)
		st = append(st, off)
		off += len(cl)
	}
	return append(st, len(s))
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

var clusters clusterCache

// clusterIndex returns the index of the cluster containing byte offset col
// (len(starts)-1 when col is at the end).
func clusterIndex(starts []int, col int) int {
	lo, hi := 0, len(starts)-1
	for lo < hi {
		m := (lo + hi + 1) / 2
		if starts[m] <= col {
			lo = m
		} else {
			hi = m - 1
		}
	}
	return lo
}

// nextCol returns the byte offset of the cluster after the one at col.
func nextCol(s string, col int) int {
	if col >= len(s) {
		return len(s)
	}
	if s[col] < 0x80 && (col+1 >= len(s) || s[col+1] < 0x80) && s[col] != '\r' {
		return col + 1
	}
	st := clusters.starts(s)
	i := clusterIndex(st, col)
	if i+1 < len(st) {
		return st[i+1]
	}
	return len(s)
}

// prevCol returns the byte offset of the cluster before col.
func prevCol(s string, col int) int {
	if col <= 0 {
		return 0
	}
	if col <= len(s) && s[col-1] < 0x80 && (col-1 == 0 || s[col-2] < 0x80) && s[col-1] != '\n' {
		return col - 1
	}
	st := clusters.starts(s)
	i := clusterIndex(st, col)
	if st[i] == col && i > 0 {
		return st[i-1]
	}
	return st[i]
}

// snapCol moves col back to the start of the cluster containing it.
func snapCol(s string, col int) int {
	if col >= len(s) {
		return len(s)
	}
	if col <= 0 {
		return 0
	}
	if isASCIIAround(s, col) {
		return col
	}
	st := clusters.starts(s)
	return st[clusterIndex(st, col)]
}

func isASCIIAround(s string, col int) bool {
	return s[col] < 0x80 && s[col-1] < 0x80
}

// lastCol returns the start of the last cluster of s (0 if empty): the
// rightmost position the Normal-mode cursor may take.
func lastCol(s string) int {
	if s == "" {
		return 0
	}
	return prevCol(s, len(s))
}

// clusterAt returns the cluster starting at col.
func clusterAt(s string, col int) string {
	if col >= len(s) {
		return ""
	}
	return s[col:nextCol(s, col)]
}

// countClusters returns the number of clusters in s[:col].
func countClusters(s string, col int) int {
	if isASCII(s[:col]) {
		return col
	}
	st := clusters.starts(s)
	return clusterIndex(st, col)
}

// clusterWidth is the display width of a cluster at display column vcol.
func clusterWidth(g string, vcol int) int {
	if g == "\t" {
		return tabStop - vcol%tabStop
	}
	if len(g) == 1 && (g[0] < 0x20 || g[0] == 0x7f) {
		return 2 // ^X
	}
	w := text.GraphemeWidth(g)
	if w == 0 {
		r, _ := utf8.DecodeRuneInString(g)
		if r >= 0x80 && r < 0xa0 {
			return 4 // <xx>
		}
		return 1 // orphan mark, drawn on a dotted circle
	}
	return w
}

// vcolOf returns the display column of byte offset col in s.
func vcolOf(s string, col int) int {
	v := 0
	for i := 0; i < col && i < len(s); {
		j := nextCol(s, i)
		v += clusterWidth(s[i:j], v)
		i = j
	}
	return v
}

// colAtVcol returns the byte offset of the cluster covering display column
// v (the last cluster when v is past the end; len(s) if allowEnd and v is
// past the end).
func colAtVcol(s string, v int, allowEnd bool) int {
	cur := 0
	i := 0
	for i < len(s) {
		j := nextCol(s, i)
		w := clusterWidth(s[i:j], cur)
		if cur+w > v {
			return i
		}
		cur += w
		i = j
	}
	if allowEnd {
		return len(s)
	}
	return lastCol(s)
}

// Character classes, following Vim's cls(): 0 blank, 1 punctuation, 2 word
// characters, and separate classes for scripts that Vim treats as their own
// words (CJK ideographs, kana, hangul, emoji).
func runeClass(r rune, big bool) int {
	if r == ' ' || r == '\t' || r == 0 || r == ' ' || r == '　' {
		return 0
	}
	if big {
		return 1
	}
	if r < 0x80 {
		if r == '_' || ('0' <= r && r <= '9') || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') {
			return 2
		}
		if unicode.IsSpace(r) {
			return 0
		}
		return 1
	}
	switch {
	case unicode.IsSpace(r):
		return 0
	case unicode.Is(unicode.Han, r):
		return 3
	case unicode.Is(unicode.Hiragana, r):
		return 4
	case unicode.Is(unicode.Katakana, r):
		return 5
	case unicode.Is(unicode.Hangul, r):
		return 6
	case r >= 0x1f000 && r <= 0x1faff, r >= 0x2600 && r <= 0x27bf:
		return 7 // emoji and pictographs
	case unicode.IsLetter(r), unicode.IsDigit(r), unicode.IsMark(r), unicode.IsNumber(r):
		return 2
	}
	return 1
}

func clusterClass(g string, big bool) int {
	if g == "" {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(g)
	return runeClass(r, big)
}

func isWordRune(r rune) bool { return runeClass(r, false) >= 2 && runeClass(r, false) != 7 }

func isBlank(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' {
			return false
		}
	}
	return true
}

// firstNonBlank returns the byte offset of the first non-blank character
// of s (the last cluster if the line is all blanks, as Vim's ^ does).
func firstNonBlank(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' {
			return i
		}
	}
	return lastCol(s)
}

// leadingWS returns the leading blanks of s.
func leadingWS(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i]
}

// wsWidth is the display width of a run of leading blanks.
func wsWidth(s string) int { return vcolOf(s, len(s)) }
