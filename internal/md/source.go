package md

import "sort"

// splitLines records where each line's content starts and ends in src.
// Lines end at "\n", "\r\n" or a lone "\r". A leading UTF-8 BOM is not part
// of line 0's content. A final line terminator does not start a new line
// (so "a\n" has one line, "" has zero lines).
func splitLines(src string) (starts, ends []int, bom, crlf bool) {
	i := 0
	if len(src) >= 3 && src[0] == 0xEF && src[1] == 0xBB && src[2] == 0xBF {
		bom = true
		i = 3
	}
	n := len(src)
	// Estimate the line count to avoid regrowth on large files.
	est := 1
	for j := i; j < n; j++ {
		if src[j] == '\n' {
			est++
		}
	}
	starts = make([]int, 0, est)
	ends = make([]int, 0, est)
	sawEOL := false
	for i < n {
		start := i
		for i < n && src[i] != '\n' && src[i] != '\r' {
			i++
		}
		starts = append(starts, start)
		ends = append(ends, i)
		if i < n {
			if src[i] == '\r' {
				if i+1 < n && src[i+1] == '\n' {
					if !sawEOL {
						crlf = true
					}
					i++
				}
			}
			sawEOL = true
			i++
		}
	}
	return starts, ends, bom, crlf
}

// LineCount returns the number of source lines.
func (d *Document) LineCount() int { return len(d.lineStarts) }

// Line returns the content of 0-based source line n without its line
// terminator, or "" when n is out of range.
func (d *Document) Line(n int) string {
	if n < 0 || n >= len(d.lineStarts) {
		return ""
	}
	return d.Source[d.lineStarts[n]:d.lineEnds[n]]
}

// LineStart returns the byte offset in Source where line n's content
// begins (after the BOM for line 0). Out-of-range n is clamped.
func (d *Document) LineStart(n int) int {
	if len(d.lineStarts) == 0 {
		return len(d.Source)
	}
	if n < 0 {
		n = 0
	}
	if n >= len(d.lineStarts) {
		return len(d.Source)
	}
	return d.lineStarts[n]
}

// LineCol converts a byte offset in Source to a 0-based line and a 0-based
// byte column within that line. Offsets inside a line terminator map to
// the end of that line; out-of-range offsets are clamped.
func (d *Document) LineCol(offset int) (line, col int) {
	if len(d.lineStarts) == 0 {
		return 0, 0
	}
	if offset < d.lineStarts[0] {
		return 0, 0
	}
	line = sort.Search(len(d.lineStarts), func(i int) bool { return d.lineStarts[i] > offset }) - 1
	if line < 0 {
		line = 0
	}
	col = offset - d.lineStarts[line]
	if lim := d.lineEnds[line] - d.lineStarts[line]; col > lim {
		col = lim
	}
	return line, col
}
