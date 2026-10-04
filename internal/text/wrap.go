package text

import (
	"strings"

	"github.com/rivo/uniseg"
)

// Range is a half-open byte range [Start, End) of a wrapped line within the
// source string. Spaces dropped at a soft break lie between one line's End
// and the next line's Start; a hard line break character is never inside a
// range.
type Range struct {
	Start, End int
}

// WrapRanges word-wraps s and returns the byte range of each display line.
// The first line holds at most first cells, every later line at most rest
// cells (a hanging indent is first < rest or first > rest, with the prefix
// drawn by the caller). Breaks follow the Unicode line breaking algorithm
// (UAX #14): at spaces, after hyphens, and between CJK ideographs; a word
// longer than the line is broken between grapheme clusters; a grapheme is
// never split. Hard line breaks ("\n", "\r\n", U+2028 …) always break and
// start the next line at rest width. Leading spaces at the start of the text
// and after a hard break are kept; spaces at a soft break are dropped.
//
// Empty input yields one empty line; a trailing hard break does not add an
// empty line. Tabs should be expanded first (ExpandTabs). Widths below 1
// are treated as 1.
func WrapRanges(s string, first, rest int) []Range {
	if first < 1 {
		first = 1
	}
	if rest < 1 {
		rest = 1
	}
	w := wrapper{avail: first, rest: rest, paraStart: true, lineStart: -1}
	state := -1
	pos := 0
	for len(s) > 0 {
		var seg string
		seg, s, _, state = uniseg.FirstLineSegmentInString(s, state)
		segStart := pos
		pos += len(seg)
		body, hard := trimHardBreak(seg)
		content := strings.TrimRight(body, " ")
		spaces := len(body) - len(content)
		if w.paraStart && w.empty() && w.leadStart < 0 {
			w.leadStart = segStart
		}
		if content == "" {
			w.pend += spaces
		} else {
			w.place(segStart, content)
			w.pend = spaces
		}
		if hard {
			w.hardBreak(segStart + len(body))
		}
	}
	if !w.empty() || len(w.lines) == 0 {
		if w.empty() {
			w.lines = append(w.lines, Range{pos, pos})
		} else {
			w.lines = append(w.lines, Range{w.lineStart, w.lineEnd})
		}
	}
	return w.lines
}

type wrapper struct {
	lines     []Range
	avail     int // width of the current line
	rest      int // width of continuation lines
	cur       int // cells used on the current line
	pend      int // pending spaces (cells) before the next content
	lineStart int // byte start of the current line, -1 when empty
	lineEnd   int
	paraStart bool // current line is the first of a hard-broken paragraph
	leadStart int  // byte start of leading spaces of the paragraph, -1 if none
}

func (w *wrapper) empty() bool { return w.lineStart < 0 }

func (w *wrapper) emit() {
	w.lines = append(w.lines, Range{w.lineStart, w.lineEnd})
	w.lineStart = -1
	w.cur = 0
	w.pend = 0
	w.avail = w.rest
	w.paraStart = false
	w.leadStart = -1
}

func (w *wrapper) hardBreak(at int) {
	if w.empty() {
		w.lines = append(w.lines, Range{at, at})
	} else {
		w.lines = append(w.lines, Range{w.lineStart, w.lineEnd})
	}
	w.lineStart = -1
	w.cur = 0
	w.pend = 0
	w.avail = w.rest
	w.paraStart = true
	w.leadStart = -1
}

// place puts a run of non-space content starting at byte start.
func (w *wrapper) place(start int, content string) {
	cw := Width(content)
	end := start + len(content)
	if !w.empty() {
		if w.cur+w.pend+cw <= w.avail {
			w.cur += w.pend + cw
			w.lineEnd = end
			return
		}
		w.emit()
	}
	// The line is empty. Keep paragraph-leading spaces if they fit with the
	// content; otherwise drop them.
	lead := 0
	ls := start
	if w.paraStart && w.leadStart >= 0 && w.pend > 0 && w.pend+cw <= w.avail {
		lead = w.pend
		ls = w.leadStart
	}
	if lead+cw <= w.avail {
		w.lineStart, w.lineEnd, w.cur = ls, end, lead+cw
		return
	}
	// Over-long word: break between grapheme clusters.
	lineStart, used := start, 0
	off := start
	EachGrapheme(content, func(g string, gw int) bool {
		if used > 0 && used+gw > w.avail {
			w.lineStart, w.lineEnd = lineStart, off
			w.emit()
			lineStart, used = off, 0
		}
		used += gw
		off += len(g)
		return true
	})
	w.lineStart, w.lineEnd, w.cur = lineStart, end, used
}

// trimHardBreak removes a trailing mandatory line break from a segment.
func trimHardBreak(seg string) (string, bool) {
	switch {
	case strings.HasSuffix(seg, "\r\n"):
		return seg[:len(seg)-2], true
	case strings.HasSuffix(seg, "\n"), strings.HasSuffix(seg, "\r"),
		strings.HasSuffix(seg, "\v"), strings.HasSuffix(seg, "\f"):
		return seg[:len(seg)-1], true
	case strings.HasSuffix(seg, " "), strings.HasSuffix(seg, " "):
		return seg[:len(seg)-3], true
	case strings.HasSuffix(seg, "\u0085"):
		return seg[:len(seg)-2], true
	}
	return seg, false
}

// Wrap word-wraps s to width cells (see WrapRanges for the rules).
func Wrap(s string, width int) []string {
	return WrapWidths(s, width, width)
}

// WrapWidths wraps s with first-line width first and continuation width rest.
func WrapWidths(s string, first, rest int) []string {
	rs := WrapRanges(s, first, rest)
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = s[r.Start:r.End]
	}
	return out
}

// WrapIndent wraps s to width cells, prefixing the first line with
// firstPrefix and every later line with restPrefix; prefix widths count
// against width. This is the hanging indent of list items:
//
//	WrapIndent(text, 40, "• ", "  ")
func WrapIndent(s string, width int, firstPrefix, restPrefix string) []string {
	lines := WrapWidths(s, width-Width(firstPrefix), width-Width(restPrefix))
	for i := range lines {
		if i == 0 {
			lines[i] = firstPrefix + lines[i]
		} else {
			lines[i] = restPrefix + lines[i]
		}
	}
	return lines
}

// Span is a run of text carrying an opaque style or payload S (for example
// a theme.Style, or a struct holding a style and a link target).
type Span[S any] struct {
	Text  string
	Style S
}

// SpansWidth is the total cell width of spans.
func SpansWidth[S any](spans []Span[S]) int {
	w := 0
	for _, sp := range spans {
		w += Width(sp.Text)
	}
	return w
}

// SpansText concatenates the text of spans.
func SpansText[S any](spans []Span[S]) string {
	var b strings.Builder
	for _, sp := range spans {
		b.WriteString(sp.Text)
	}
	return b.String()
}

// WrapSpans word-wraps a styled span sequence exactly as WrapRanges wraps
// its concatenated text: break opportunities are found across span
// boundaries, and spans are split where lines break. Each returned line is a
// span slice (empty spans are dropped; an empty line is an empty slice).
func WrapSpans[S any](spans []Span[S], first, rest int) [][]Span[S] {
	full := SpansText(spans)
	ranges := WrapRanges(full, first, rest)
	out := make([][]Span[S], len(ranges))
	si, sStart := 0, 0 // current span index and its byte start in full
	for li, r := range ranges {
		var line []Span[S]
		// Advance to the first span that ends after r.Start.
		for si < len(spans) && sStart+len(spans[si].Text) <= r.Start {
			sStart += len(spans[si].Text)
			si++
		}
		j, jStart := si, sStart
		for j < len(spans) && jStart < r.End {
			sp := spans[j]
			lo := max(r.Start, jStart) - jStart
			hi := min(r.End, jStart+len(sp.Text)) - jStart
			if hi > lo {
				line = append(line, Span[S]{Text: sp.Text[lo:hi], Style: sp.Style})
			}
			jStart += len(sp.Text)
			j++
		}
		if line == nil {
			line = []Span[S]{}
		}
		out[li] = line
	}
	return out
}

// TruncateSpans cuts spans to at most width cells, appending ellipsis (in
// the style of the last kept span) when anything was cut.
func TruncateSpans[S any](spans []Span[S], width int, ellipsis string) []Span[S] {
	if SpansWidth(spans) <= width {
		return spans
	}
	ew := Width(ellipsis)
	if ew > width {
		ellipsis, ew = "", 0
	}
	limit := width - ew
	var out []Span[S]
	used := 0
	for _, sp := range spans {
		if used >= limit {
			break
		}
		t := Truncate(sp.Text, limit-used, "")
		if t != "" {
			out = append(out, Span[S]{Text: t, Style: sp.Style})
		}
		used += Width(t)
		if t != sp.Text {
			break
		}
	}
	if ellipsis != "" {
		st := spans[0].Style
		if len(out) > 0 {
			st = out[len(out)-1].Style
		}
		out = append(out, Span[S]{Text: ellipsis, Style: st})
	}
	return out
}
