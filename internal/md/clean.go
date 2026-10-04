package md

import "strings"

// Plain-text rendering of single source lines, for lists that show one
// line of a note out of context: the TUI's search results and agenda, and
// the human (terminal) output of the CLI's find and tasks.

// CleanLine turns one raw source line (a search hit, a task) into the text
// a reader sees — list, task, quote and heading markers, emphasis markers and
// link syntax removed — and carries the match ranges (byte offsets into raw)
// across. Text kept verbatim maps exactly; a match that touches a link
// written as [[Target|label]] highlights the whole label. A line with no
// inline content (a code fence, a table delimiter) is returned unchanged.
func CleanLine(raw string, ranges [][2]int) (string, [][2]int) {
	if strings.TrimSpace(raw) == "" || strings.ContainsRune(raw, '\n') {
		return raw, ranges
	}
	if cells := TableCellSpans(strings.TrimSpace(raw)); len(cells) > 1 && !delimiterRow(raw) {
		// A table row: its cells, cleaned, separated by a middle dot.
		lead := len(raw) - len(strings.TrimLeft(raw, " "))
		var b strings.Builder
		var out [][2]int
		for _, c := range cells {
			a, z := c[0]+lead, c[1]+lead
			txt, rs := CleanLine(raw[a:z], shiftRanges(ranges, -a, z-a))
			if strings.TrimSpace(txt) == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteString(" · ")
			}
			out = append(out, shiftRanges(rs, b.Len(), b.Len()+len(txt))...)
			b.WriteString(txt)
		}
		return b.String(), out
	}
	doc := Parse(raw)
	if len(doc.Blocks) != 1 {
		return raw, ranges
	}
	var inl []Inline
	WalkBlocks(doc.Blocks, func(b Block) bool {
		if inl == nil {
			for _, l := range BlockInlines(b) {
				inl = append(inl, l...)
			}
		}
		return inl == nil
	})
	if len(inl) == 0 {
		return raw, ranges
	}
	c := cleaner{raw: raw, ranges: ranges}
	c.walk(inl)
	out := strings.TrimSpace(c.b.String())
	if out == "" {
		return raw, ranges
	}
	lead := len(c.b.String()) - len(strings.TrimLeft(c.b.String(), " "))
	return out, shiftRanges(c.out, -lead, len(out))
}

type cleaner struct {
	raw    string
	ranges [][2]int
	b      strings.Builder
	out    [][2]int
}

// add appends text that came from raw[start:end]. exact means text is
// raw[start:end] byte for byte, so matches map one to one; otherwise the
// whole text is highlighted when any match overlaps the source.
func (c *cleaner) add(text string, start, end int, exact bool) {
	at := c.b.Len()
	c.b.WriteString(text)
	for _, r := range c.ranges {
		a, b := max(r[0], start), min(r[1], end)
		if a >= b {
			continue
		}
		if exact {
			c.mark(at+a-start, at+b-start)
		} else {
			c.mark(at, at+len(text))
		}
	}
}

func (c *cleaner) mark(a, b int) {
	if n := len(c.out); n > 0 && c.out[n-1][1] >= a {
		c.out[n-1][1] = max(c.out[n-1][1], b)
		return
	}
	c.out = append(c.out, [2]int{a, b})
}

func (c *cleaner) span(n Inline) (int, int) {
	s := n.SourceSpan()
	return max(0, min(s.Start, len(c.raw))), max(0, min(s.End, len(c.raw)))
}

func (c *cleaner) verbatim(n Inline) {
	a, b := c.span(n)
	c.add(c.raw[a:b], a, b, true)
}

func (c *cleaner) walk(inl []Inline) {
	for _, n := range inl {
		a, b := c.span(n)
		switch n := n.(type) {
		case *Text:
			c.add(n.Value, a, b, c.raw[a:b] == n.Value)
		case *Code:
			if i := strings.Index(c.raw[a:b], n.Value); i >= 0 && n.Value != "" {
				c.add(n.Value, a+i, a+i+len(n.Value), true)
			} else {
				c.add(n.Value, a, b, false)
			}
		case *SoftBreak, *HardBreak:
			c.add(" ", a, b, false)
		case *Emphasis:
			c.walk(n.Children)
		case *Strong:
			c.walk(n.Children)
		case *Strikethrough:
			c.walk(n.Children)
		case *Highlight:
			c.walk(n.Children)
		case *Link:
			c.walk(n.Children)
		case *WikiLink:
			c.add(n.Display(), a, b, false)
		case *Image:
			c.add(PlainText(n.Alt), a, b, false)
		case *Tag, *Math, *FootnoteRef:
			c.verbatim(n)
		case *RawHTML, *Comment:
			// hidden in the reader
		default:
			c.verbatim(n)
		}
	}
}

// delimiterRow reports a table's |---|:--:| line.
func delimiterRow(line string) bool {
	return strings.Trim(line, "|-: \t") == "" && strings.Contains(line, "-")
}

// TableCellSpans returns the byte ranges of a table row's trimmed cells, or
// nil when line is not a table row. Pipes escaped as \| or inside [[…]] and
// code spans do not split.
func TableCellSpans(line string) [][2]int {
	if len(line) < 2 || line[0] != '|' {
		return nil
	}
	var cells [][2]int
	add := func(a, b int) {
		for a < b && line[a] == ' ' {
			a++
		}
		for b > a && line[b-1] == ' ' {
			b--
		}
		cells = append(cells, [2]int{a, b})
	}
	depth, code := 0, false
	start := 1
	for i := 1; i < len(line); i++ {
		switch c := line[i]; {
		case c == '\\':
			i++
		case c == '`':
			code = !code
		case !code && strings.HasPrefix(line[i:], "[["):
			depth++
			i++
		case !code && depth > 0 && strings.HasPrefix(line[i:], "]]"):
			depth--
			i++
		case c == '|' && depth == 0 && !code:
			add(start, i)
			start = i + 1
		}
	}
	if start < len(line) && strings.TrimSpace(line[start:]) != "" {
		add(start, len(line))
	}
	return cells
}

// shiftRanges moves byte ranges by `by` and clips them to [0, limit).
func shiftRanges(rs [][2]int, by, limit int) [][2]int {
	var out [][2]int
	for _, r := range rs {
		a, b := r[0]+by, r[1]+by
		if b <= 0 || a >= limit {
			continue
		}
		out = append(out, [2]int{max(a, 0), min(b, limit)})
	}
	return out
}
