package editor

// textRange is the region an operator acts on. For a charwise range, end is
// exclusive; for a linewise range, start.Line..end.Line are included whole.
type textRange struct {
	start, end Pos
	linewise   bool
}

// wordObject implements iw, aw, iW and aW on the cursor line.
func wordObject(b *buffer, p Pos, count int, inner, big bool) (textRange, bool) {
	s := b.lines[p.Line]
	if s == "" {
		return textRange{}, false
	}
	class := func(col int) int { return clusterClass(clusterAt(s, col), big) }
	// run boundaries around col
	runStart := func(col int) int {
		c := class(col)
		for col > 0 {
			q := prevCol(s, col)
			if class(q) != c {
				break
			}
			col = q
		}
		return col
	}
	runEnd := func(col int) int {
		c := class(col)
		for col < len(s) && class(col) == c {
			col = nextCol(s, col)
		}
		return col
	}
	col := snapCol(s, p.Col)
	if col >= len(s) {
		col = lastCol(s)
	}
	start := runStart(col)
	end := runEnd(col)
	onBlank := class(col) == 0
	if inner {
		for i := 1; i < count && end < len(s); i++ {
			end = runEnd(end)
		}
		return textRange{start: Pos{p.Line, start}, end: Pos{p.Line, end}}, true
	}
	if onBlank {
		// blanks + the following word
		if end < len(s) {
			end = runEnd(end)
		}
		for i := 1; i < count && end < len(s); i++ {
			end = runEnd(end)
			if end < len(s) {
				end = runEnd(end)
			}
		}
		return textRange{start: Pos{p.Line, start}, end: Pos{p.Line, end}}, true
	}
	for i := 1; i <= count; i++ {
		if i > 1 {
			if end >= len(s) {
				break
			}
			end = runEnd(end) // the next word
		}
		if end < len(s) && class(end) == 0 {
			end = runEnd(end) // trailing blanks
		} else if i == 1 && start > 0 && class(prevCol(s, start)) == 0 {
			// no trailing blanks: take the leading ones instead
			start = runStart(prevCol(s, start))
		}
	}
	return textRange{start: Pos{p.Line, start}, end: Pos{p.Line, end}}, true
}

// quoteObject implements i" a" i' a' i` a` on the cursor line.
func quoteObject(b *buffer, p Pos, q byte, inner bool) (textRange, bool) {
	s := b.lines[p.Line]
	var quotes []int
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == q {
			quotes = append(quotes, i)
		}
	}
	col := p.Col
	open, close := -1, -1
	before := 0
	onQuote := -1
	for k, qi := range quotes {
		if qi < col {
			before++
		} else if qi == col {
			onQuote = k
		}
	}
	switch {
	case onQuote >= 0:
		if onQuote%2 == 0 && onQuote+1 < len(quotes) {
			open, close = quotes[onQuote], quotes[onQuote+1]
		} else if onQuote%2 == 1 {
			open, close = quotes[onQuote-1], quotes[onQuote]
		}
	case before%2 == 1 && before < len(quotes):
		open, close = quotes[before-1], quotes[before]
	case before%2 == 0 && before+1 < len(quotes):
		open, close = quotes[before], quotes[before+1]
	}
	if open < 0 {
		return textRange{}, false
	}
	if inner {
		return textRange{start: Pos{p.Line, open + 1}, end: Pos{p.Line, close}}, true
	}
	start, end := open, close+1
	if end < len(s) && (s[end] == ' ' || s[end] == '\t') {
		for end < len(s) && (s[end] == ' ' || s[end] == '\t') {
			end++
		}
	} else {
		for start > 0 && (s[start-1] == ' ' || s[start-1] == '\t') {
			start--
		}
	}
	return textRange{start: Pos{p.Line, start}, end: Pos{p.Line, end}}, true
}

// blockObject implements i( a( i[ a[ i{ a{ i< a< (with counts selecting
// enclosing levels).
func blockObject(b *buffer, p Pos, open, close byte, inner bool, count int) (textRange, bool) {
	s := b.lines[p.Line]
	var o Pos
	found := false
	cur := p
	if p.Col < len(s) && s[p.Col] == open {
		o, found = p, true
	} else if p.Col < len(s) && s[p.Col] == close {
		o, found = findPair(b, p, open, close, false)
	} else {
		o, found = findPair(b, cur, open, close, false)
	}
	if !found {
		return textRange{}, false
	}
	for i := 1; i < count; i++ {
		o2, ok := findPair(b, o, open, close, false)
		if !ok {
			return textRange{}, false
		}
		o = o2
	}
	c, ok := findPair(b, o, open, close, true)
	if !ok {
		return textRange{}, false
	}
	if !inner {
		return textRange{start: o, end: Pos{c.Line, c.Col + 1}}, true
	}
	start := Pos{o.Line, o.Col + 1}
	end := c
	openAtEOL := start.Col >= len(b.lines[o.Line])
	closeAtBOL := isBlank(b.lines[c.Line][:c.Col])
	if openAtEOL && closeAtBOL && c.Line-o.Line >= 2 {
		return textRange{start: Pos{o.Line + 1, 0}, end: Pos{c.Line - 1, 0}, linewise: true}, true
	}
	if openAtEOL && c.Line > o.Line {
		start = Pos{o.Line + 1, 0}
	}
	if closeAtBOL && c.Line > start.Line {
		end = Pos{c.Line - 1, len(b.lines[c.Line-1])}
	}
	return textRange{start: start, end: end}, true
}

// paragraphObject implements ip and ap (linewise). Whitespace-only lines
// count as blank here, as in Vim.
func paragraphObject(b *buffer, line, count int, inner bool) (textRange, bool) {
	n := len(b.lines)
	blank := func(i int) bool { return isBlank(b.lines[i]) }
	runEnd := func(i int) int {
		k := blank(i)
		for i+1 < n && blank(i+1) == k {
			i++
		}
		return i
	}
	start := line
	k := blank(line)
	for start > 0 && blank(start-1) == k {
		start--
	}
	end := runEnd(line)
	if inner {
		for i := 1; i < count && end+1 < n; i++ {
			end = runEnd(end + 1)
		}
		return textRange{start: Pos{start, 0}, end: Pos{end, 0}, linewise: true}, true
	}
	for i := 0; i < count; i++ {
		if i > 0 {
			if end+1 >= n {
				break
			}
			end = runEnd(end + 1)
		}
		if end+1 < n {
			end = runEnd(end + 1)
		} else if i == 0 && !k {
			// no blank lines after: include the ones before
			for start > 0 && blank(start-1) {
				start--
			}
		}
	}
	return textRange{start: Pos{start, 0}, end: Pos{end, 0}, linewise: true}, true
}
