package md

import "strings"

// cellText is one raw table cell: its trimmed text and the byte index of
// that text within the row string it came from.
type cellText struct {
	text  string
	start int
}

// splitRow splits a table row into cells. A leading and a trailing pipe
// are optional. Pipes escaped with a backslash, inside code spans that
// close on the same row, or inside [[wikilinks]] do not separate cells.
// hasPipe reports whether the row contains at least one separating pipe.
func splitRow(row string) (cells []cellText, hasPipe bool) {
	lo, hi := 0, len(row)
	for lo < hi && isSpaceOrTab(row[lo]) {
		lo++
	}
	for hi > lo && isSpaceOrTab(row[hi-1]) {
		hi--
	}
	if lo < hi && row[lo] == '|' {
		lo++
		hasPipe = true
	}
	if hi > lo && row[hi-1] == '|' && !escapedAt(row, hi-1) {
		hi--
		hasPipe = true
	}
	cellStart := lo
	add := func(a, b int) {
		for a < b && isSpaceOrTab(row[a]) {
			a++
		}
		for b > a && isSpaceOrTab(row[b-1]) {
			b--
		}
		cells = append(cells, cellText{text: row[a:b], start: a})
	}
	for i := lo; i < hi; {
		switch c := row[i]; {
		case c == '\\':
			i += 2
		case c == '`':
			n := 0
			for i+n < hi && row[i+n] == '`' {
				n++
			}
			if end := findBacktickRun(row[:hi], i+n, n); end >= 0 {
				i = end + n
			} else {
				i += n
			}
		case c == '[' && i+1 < hi && row[i+1] == '[':
			if j := strings.Index(row[i+2:hi], "]]"); j >= 0 {
				i = i + 2 + j + 2
			} else {
				i += 2
			}
		case c == '|':
			add(cellStart, i)
			cellStart = i + 1
			hasPipe = true
			i++
		default:
			i++
		}
	}
	if cellStart > hi {
		cellStart = hi
	}
	add(cellStart, hi)
	return cells, hasPipe
}

// escapedAt reports whether the byte at i is preceded by an odd number of
// backslashes.
func escapedAt(s string, i int) bool {
	n := 0
	for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
		n++
	}
	return n%2 == 1
}

// findBacktickRun returns the index of the next run of exactly n
// backticks in s at or after from, or -1.
func findBacktickRun(s string, from, n int) int {
	for i := from; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(s) && s[j] == '`' {
			j++
		}
		if j-i == n {
			return i
		}
		i = j
	}
	return -1
}

// parseDelimRow parses a GFM table delimiter row such as "| :-- | --: |".
// The row must contain at least one pipe.
func parseDelimRow(s string) ([]Align, bool) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "|") {
		return nil, false
	}
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	parts := strings.Split(s, "|")
	al := make([]Align, 0, len(parts))
	for _, part := range parts {
		c := strings.TrimSpace(part)
		left := strings.HasPrefix(c, ":")
		right := strings.HasSuffix(c, ":")
		c = strings.TrimPrefix(c, ":")
		c = strings.TrimSuffix(c, ":")
		if c == "" || strings.Trim(c, "-") != "" {
			return nil, false
		}
		switch {
		case left && right:
			al = append(al, AlignCenter)
		case left:
			al = append(al, AlignLeft)
		case right:
			al = append(al, AlignRight)
		default:
			al = append(al, AlignNone)
		}
	}
	return al, true
}
