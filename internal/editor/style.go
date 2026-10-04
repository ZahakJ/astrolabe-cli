package editor

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ZahakJ/astrolabe-cli/internal/theme"
)

// lineState is the block context at the start of a line, carried across
// lines by a light scanner: fenced code, frontmatter, $$ math, multi-line
// comments, callouts and tables. It is the editor's own line-level
// Markdown tokenizer; it does not need the full AST.
type lineState struct {
	fence    byte // '`' or '~' inside a fenced code block
	fenceLen int
	front    bool // inside frontmatter
	math     bool
	comment  byte   // '%' or '<' inside a multi-line comment
	callout  string // kind while inside a callout's quote lines
	table    bool   // the line continues a table
}

// syncStates drops cached line states from the first edited line on.
func (e *Editor) syncStates() {
	d := e.buf.dirtyFrom
	if d < 0 {
		return
	}
	e.buf.dirtyFrom = -1
	if d <= e.fmEnd+1 {
		d = 0
	}
	if d < len(e.states) {
		e.states = e.states[:d]
	}
}

// stateAt returns the block state at the start of line l.
func (e *Editor) stateAt(l int) lineState {
	e.syncStates()
	if len(e.states) == 0 {
		e.fmEnd = -1
		e.states = append(e.states, lineState{})
	}
	for len(e.states) <= l {
		i := len(e.states) - 1
		e.states = append(e.states, e.nextState(e.states[i], i))
	}
	return e.states[l]
}

func (e *Editor) inCode(l int) bool { return e.stateAt(l).fence != 0 }

// nextState computes the state after line i.
func (e *Editor) nextState(st lineState, i int) lineState {
	b := e.buf
	s := b.line(i)
	if i == 0 && strings.TrimRight(s, " \t") == "---" {
		for j := 1; j < b.lineCount() && j < 2000; j++ {
			t := strings.TrimRight(b.line(j), " \t")
			if t == "---" || t == "..." {
				e.fmEnd = j
				return lineState{front: true}
			}
		}
	}
	if st.front {
		if i == e.fmEnd {
			return lineState{}
		}
		return st
	}
	if st.fence != 0 {
		if isFenceClose(s, st.fence, st.fenceLen) {
			return lineState{}
		}
		return st
	}
	if st.math {
		if strings.HasSuffix(strings.TrimSpace(s), "$$") {
			return lineState{}
		}
		return st
	}
	if st.comment != 0 {
		if (st.comment == '%' && strings.Contains(s, "%%")) || (st.comment == '<' && strings.Contains(s, "-->")) {
			return lineState{}
		}
		return st
	}
	var next lineState
	if c, n, ok := fenceOpen(s); ok {
		return lineState{fence: c, fenceLen: n}
	}
	t := strings.TrimSpace(s)
	if t == "$$" || (strings.HasPrefix(t, "$$") && !strings.HasSuffix(t[2:], "$$")) {
		return lineState{math: true}
	}
	if strings.HasPrefix(t, "%%") && strings.Count(t, "%%") == 1 {
		return lineState{comment: '%'}
	}
	if strings.HasPrefix(t, "<!--") && !strings.Contains(t[4:], "-->") {
		return lineState{comment: '<'}
	}
	if strings.HasPrefix(t, ">") {
		if k := calloutKind(s); k != "" {
			next.callout = k
		} else {
			next.callout = st.callout
		}
	}
	if e.isTableLine(i, st) && i+1 < b.lineCount() && isPipeRow(b.line(i+1)) {
		next.table = true
	}
	return next
}

// isTableLine reports whether line i is part of a table: a pipe row that
// continues a table or is a header followed by a delimiter row.
func (e *Editor) isTableLine(i int, st lineState) bool {
	s := e.buf.line(i)
	if !isPipeRow(s) {
		return false
	}
	return st.table || isDelimRow(s) && i > 0 && isPipeRow(e.buf.line(i-1)) ||
		(i+1 < e.buf.lineCount() && isDelimRow(e.buf.line(i+1)))
}

// stripContainers removes block-quote markers and indentation.
func stripContainers(s string) string {
	for {
		t := strings.TrimLeft(s, " \t")
		if strings.HasPrefix(t, ">") {
			s = t[1:]
			continue
		}
		return t
	}
}

func fenceOpen(s string) (byte, int, bool) {
	t := stripContainers(s)
	if len(t) < 3 || (t[0] != '`' && t[0] != '~') {
		return 0, 0, false
	}
	c := t[0]
	n := 0
	for n < len(t) && t[n] == c {
		n++
	}
	if n < 3 {
		return 0, 0, false
	}
	if c == '`' && strings.ContainsRune(t[n:], '`') {
		return 0, 0, false
	}
	return c, n, true
}

func isFenceClose(s string, c byte, n int) bool {
	t := stripContainers(s)
	k := 0
	for k < len(t) && t[k] == c {
		k++
	}
	return k >= n && strings.TrimSpace(t[k:]) == ""
}

// calloutKind returns the type of a "> [!type]" line, lower-cased.
func calloutKind(s string) string {
	t := stripContainers(s)
	if !strings.HasPrefix(t, "[!") {
		return ""
	}
	j := strings.IndexByte(t, ']')
	if j < 3 {
		return ""
	}
	return strings.ToLower(t[2:j])
}

// isPipeRow reports a line containing an unescaped pipe outside code.
func isPipeRow(s string) bool {
	if !strings.Contains(s, "|") {
		return false
	}
	return pipeCount(s) > 0
}

func pipeCount(s string) int {
	n := 0
	inCode := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '`':
			inCode = !inCode
		case '|':
			if !inCode {
				n++
			}
		}
	}
	return n
}

// isDelimRow reports a table delimiter row such as "| :--- | ---: |".
func isDelimRow(s string) bool {
	t := strings.TrimSpace(stripQuote(s))
	if !strings.Contains(t, "|") || !strings.Contains(t, "-") {
		return false
	}
	t = strings.Trim(t, "|")
	for _, c := range strings.Split(t, "|") {
		c = strings.TrimSpace(c)
		c = strings.TrimPrefix(c, ":")
		c = strings.TrimSuffix(c, ":")
		if c == "" || strings.Trim(c, "-") != "" {
			return false
		}
	}
	return true
}

func stripQuote(s string) string {
	lp, ok := parseListPrefix(s)
	if ok && !lp.isList() {
		return s[lp.end:]
	}
	return s
}

// styler paints per-byte styles for one line.
type styler struct {
	th theme.Theme
	s  string
	st []theme.Style
}

func (y *styler) fg(a, b int, c theme.Color) {
	for i := max(a, 0); i < b && i < len(y.st); i++ {
		y.st[i] = y.st[i].Fg(c)
	}
}

func (y *styler) attr(a, b int, at theme.Attr) {
	for i := max(a, 0); i < b && i < len(y.st); i++ {
		y.st[i] = y.st[i].With(at)
	}
}

func (y *styler) bg(a, b int, c theme.Color) {
	for i := max(a, 0); i < b && i < len(y.st); i++ {
		y.st[i] = y.st[i].Bg(c)
	}
}

func (y *styler) over(a, b int, o theme.Style) {
	for i := max(a, 0); i < b && i < len(y.st); i++ {
		y.st[i] = o.Over(y.st[i])
	}
}

func (y *styler) faint(a, b int) { y.fg(a, b, y.th.Faint) }

// lineStyles returns the style of every byte of line l.
func (e *Editor) lineStyles(l int) []theme.Style {
	s := e.buf.line(l)
	st := e.stateAt(l)
	th := e.th
	base := th.Base()
	y := &styler{th: th, s: s, st: make([]theme.Style, len(s))}
	for i := range y.st {
		y.st[i] = base
	}
	codeBG := th.Raised
	switch {
	case l == 0 && e.stateAt(1).front && e.fmEnd > 0:
		y.faint(0, len(s))
	case st.front:
		if l == e.fmEnd {
			y.faint(0, len(s))
			break
		}
		y.fg(0, len(s), th.Muted)
		if k := strings.IndexByte(s, ':'); k > 0 && !strings.HasPrefix(strings.TrimSpace(s), "-") {
			y.fg(0, k+1, th.Faint)
		}
	case st.fence != 0:
		y.bg(0, len(s), codeBG)
		if isFenceClose(s, st.fence, st.fenceLen) {
			y.faint(0, len(s))
		} else if codeBG.IsDefault() {
			y.fg(0, len(s), th.Muted)
		}
	case st.math:
		y.fg(0, len(s), th.Math)
	case st.comment != 0:
		y.faint(0, len(s))
		y.attr(0, len(s), theme.Italic)
	default:
		if _, n, ok := fenceOpen(s); ok {
			y.bg(0, len(s), codeBG)
			k := len(s) - len(stripContainers(s))
			y.faint(0, k+n)
			y.fg(k+n, len(s), th.Muted)
			break
		}
		t := strings.TrimSpace(s)
		if t == "$$" || strings.HasPrefix(t, "$$") {
			y.fg(0, len(s), th.Math)
			break
		}
		e.styleBlock(y, l, st)
	}
	return y.st
}

// styleBlock styles an ordinary Markdown line: quote markers, headings,
// rules, list markers, task boxes, tables, then inline markup.
func (e *Editor) styleBlock(y *styler, l int, st lineState) {
	th := y.th
	s := y.s
	i := 0
	// block quote / callout markers
	quoted := false
	for {
		j := i
		for j < len(s) && j-i < 3 && s[j] == ' ' {
			j++
		}
		if j < len(s) && s[j] == '>' {
			y.faint(j, j+1)
			i = j + 1
			if i < len(s) && s[i] == ' ' {
				i++
			}
			quoted = true
			continue
		}
		break
	}
	callout := ""
	if quoted {
		callout = st.callout
		if k := calloutKind(s); k != "" {
			callout = k
			t := strings.Index(s[i:], "]") + i + 1
			col := th.CalloutColor(k)
			y.fg(i, t, col)
			y.attr(i, t, theme.Bold)
			end := t
			if end < len(s) && (s[end] == '+' || s[end] == '-') {
				y.faint(end, end+1)
				end++
			}
			y.attr(end, len(s), theme.Bold)
			y.inline(end, len(s))
			return
		}
		if callout == "" {
			y.fg(i, len(s), th.Muted)
			y.attr(i, len(s), theme.Italic)
		}
	}
	rest := s[i:]
	tr := strings.TrimLeft(rest, " \t")
	ind := i + len(rest) - len(tr)
	// heading
	if lvl := headingLevel(tr); lvl > 0 {
		y.fg(ind, len(s), th.Heading)
		y.attr(ind, len(s), theme.Bold)
		y.faint(ind, ind+lvl)
		// closing hashes
		t := strings.TrimRight(s, " \t")
		k := len(t)
		for k > ind+lvl && t[k-1] == '#' {
			k--
		}
		if k < len(t) && k > ind+lvl && (t[k-1] == ' ' || t[k-1] == '\t') {
			y.faint(k, len(t))
			y.inline(ind+lvl, k)
		} else {
			y.inline(ind+lvl, len(s))
		}
		return
	}
	if isThematicBreak(rest) || (strings.Trim(tr, "=") == "" && tr != "" && l > 0) {
		y.faint(0, len(s))
		return
	}
	// setext heading: a paragraph line followed by === or ---
	if tr != "" && l+1 < e.buf.lineCount() && !quoted {
		nx := strings.TrimSpace(e.buf.line(l + 1))
		if nx != "" && (strings.Trim(nx, "=") == "" || strings.Trim(nx, "-") == "") {
			if _, isList := parseListPrefix(s); !isList && !isPipeRow(s) {
				if strings.Trim(nx, "=") == "" || len(nx) >= 2 {
					y.fg(ind, len(s), th.Heading)
					y.attr(ind, len(s), theme.Bold)
					y.inline(ind, len(s))
					return
				}
			}
		}
	}
	// footnote / link reference definitions
	if strings.HasPrefix(tr, "[") {
		if j := strings.Index(tr, "]:"); j > 1 {
			y.fg(ind, ind+j+2, th.Accent)
			y.faint(ind, ind+1)
			y.faint(ind+j, ind+j+2)
			if !strings.HasPrefix(tr, "[^") {
				y.faint(ind+j+2, len(s))
				return
			}
			y.inline(ind+j+2, len(s))
			return
		}
	}
	// table rows
	if e.isTableLine(l, st) {
		if isDelimRow(s) {
			y.faint(i, len(s))
			return
		}
		header := l+1 < e.buf.lineCount() && isDelimRow(e.buf.line(l+1))
		if header {
			y.attr(i, len(s), theme.Bold)
		}
		y.inline(i, len(s))
		inCode := false
		for k := i; k < len(s); k++ {
			switch s[k] {
			case '\\':
				k++
			case '`':
				inCode = !inCode
			case '|':
				if !inCode {
					y.st[k] = th.Base().Fg(th.Faint)
				}
			}
		}
		return
	}
	// list item and task box
	if lp, ok := parseListPrefix(s); ok && lp.isList() {
		ms := len(lp.quote) + len(lp.indent)
		y.faint(ms, ms+len(lp.marker))
		if lp.ordered {
			y.fg(ms, ms+len(lp.marker)-1, th.Muted)
		}
		start := ms + len(lp.marker) + len(lp.space)
		if lp.task {
			box := start
			stEnd := nextCol(s, box+1)
			y.faint(box, box+1)
			y.faint(stEnd, stEnd+1)
			state := s[box+1 : stEnd]
			switch state {
			case "x", "X":
				y.fg(box+1, stEnd, th.Ok)
				y.fg(lp.end, len(s), th.Faint)
			case "-":
				y.faint(box+1, stEnd)
				y.fg(lp.end, len(s), th.Faint)
				y.attr(lp.end, len(s), theme.Strike)
			case " ":
			default:
				y.fg(box+1, stEnd, th.Accent)
			}
		}
		y.inline(lp.end, len(s))
		return
	}
	y.inline(i, len(s))
}

func headingLevel(t string) int {
	n := 0
	for n < len(t) && n < 7 && t[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0
	}
	if n < len(t) && t[n] != ' ' && t[n] != '\t' {
		return 0
	}
	return n
}

// delimRun is a run of emphasis delimiters (* _ ~~ ==). Openers are
// consumed from their right end, closers from their left.
type delimRun struct {
	pos, n int
	c      byte
	open   bool
	close  bool
	origN  int
}

// inline styles inline Markdown in s[from:to].
func (y *styler) inline(from, to int) {
	s := y.s[:to]
	th := y.th
	var runs []*delimRun
	type jump struct{ at, to int }
	var jumps []jump
	linkFG := th.Link
	i := from
	for i < to {
		for len(jumps) > 0 && jumps[0].at == i {
			// skip the "](url)" part of a link whose label was just styled
			i = jumps[0].to
			jumps = jumps[1:]
		}
		if i >= to {
			break
		}
		c := s[i]
		switch {
		case c == '\\' && i+1 < to && isASCIIPunct(s[i+1]):
			y.faint(i, i+1)
			i += 2
			continue
		case c == '`':
			n := 0
			for i+n < to && s[i+n] == '`' {
				n++
			}
			if end := findBackticks(s, i+n, to, n); end >= 0 {
				y.code(i, end+n, n)
				i = end + n
				continue
			}
			i += n
			continue
		case c == '$' && i+1 < to && s[i+1] != ' ' && s[i+1] != '$':
			if end := findMathEnd(s, i+1, to); end > 0 {
				y.fg(i, end+1, th.Math)
				y.faint(i, i+1)
				y.faint(end, end+1)
				i = end + 1
				continue
			}
		case c == '%' && i+1 < to && s[i+1] == '%':
			end := strings.Index(s[i+2:to], "%%")
			e := to
			if end >= 0 {
				e = i + 2 + end + 2
			}
			y.faint(i, e)
			y.attr(i, e, theme.Italic)
			i = e
			continue
		case c == '<':
			if strings.HasPrefix(s[i:to], "<!--") {
				end := strings.Index(s[i+4:to], "-->")
				e := to
				if end >= 0 {
					e = i + 4 + end + 3
				}
				y.faint(i, e)
				i = e
				continue
			}
			if j := strings.IndexByte(s[i:to], '>'); j > 1 {
				inner := s[i+1 : i+j]
				if isAutolink(inner) {
					y.faint(i, i+1)
					y.fg(i+1, i+j, linkFG)
					y.faint(i+j, i+j+1)
					i += j + 1
					continue
				}
				if isHTMLTag(inner) {
					y.faint(i, i+j+1)
					i += j + 1
					continue
				}
			}
		case c == '!' && i+2 < to && s[i+1] == '[' && s[i+2] == '[':
			if end := strings.Index(s[i+3:to], "]]"); end >= 0 {
				y.faint(i, i+1)
				y.wikilink(i+1, i+3+end+2)
				i = i + 3 + end + 2
				continue
			}
		case c == '[' && i+1 < to && s[i+1] == '[':
			if end := strings.Index(s[i+2:to], "]]"); end >= 0 {
				y.wikilink(i, i+2+end+2)
				i = i + 2 + end + 2
				continue
			}
		case c == '[' || (c == '!' && i+1 < to && s[i+1] == '['):
			open := i
			if c == '!' {
				open = i + 1
			}
			if open+1 < to && s[open+1] == '^' {
				if j := strings.IndexByte(s[open:to], ']'); j > 0 {
					y.fg(i, open+j+1, th.Accent)
					y.faint(open, open+1)
					y.faint(open+j, open+j+1)
					i = open + j + 1
					continue
				}
			}
			cl := matchBracket(s, open, to)
			if cl > 0 && cl+1 < to && (s[cl+1] == '(' || s[cl+1] == '[') {
				var pe int
				if s[cl+1] == '(' {
					pe = matchParen(s, cl+1, to)
				} else {
					pe = strings.IndexByte(s[cl+1:to], ']')
					if pe >= 0 {
						pe += cl + 1
					}
				}
				if pe > 0 {
					if c == '!' {
						y.faint(i, i+1)
					}
					y.faint(open, open+1)
					y.fg(open+1, cl, linkFG)
					y.faint(cl, pe+1)
					jumps = append(jumps, jump{cl, pe + 1})
					i = open + 1
					continue
				}
			}
		case c == '#' && (i == 0 || isTagBoundary(s[i-1])) && i+1 < to:
			if e := tagEnd(s, i+1, to); e > i+1 {
				y.fg(i, e, th.Accent)
				i = e
				continue
			}
		case (c == 'h' || c == 'w') && (i == 0 || !isWordByte(s[i-1])):
			if e := urlEnd(s, i, to); e > i {
				y.fg(i, e, linkFG)
				y.attr(i, e, th.LinkAttrs)
				i = e
				continue
			}
		case c == '^' && i > 0 && s[i-1] == ' ' && isBlockID(s[i+1:to]):
			y.faint(i, to)
			i = to
			continue
		case c == '*' || c == '_' || c == '~' || c == '=':
			n := 0
			for i+n < to && s[i+n] == c {
				n++
			}
			if (c == '~' || c == '=') && n != 2 {
				i += n
				continue
			}
			prev, next := ' ', ' '
			if i > 0 {
				prev, _ = utf8.DecodeLastRuneInString(s[:i])
			}
			if i+n < to {
				next, _ = utf8.DecodeRuneInString(s[i+n:])
			}
			left := !unicode.IsSpace(next) && (!isPunctRune(next) || unicode.IsSpace(prev) || isPunctRune(prev))
			right := !unicode.IsSpace(prev) && (!isPunctRune(prev) || unicode.IsSpace(next) || isPunctRune(next))
			if i == from {
				right = false
			}
			if i+n == to {
				left = false
			}
			op, cl := left, right
			if c == '_' {
				op = left && (!right || isPunctRune(prev))
				cl = right && (!left || isPunctRune(next))
			}
			runs = append(runs, &delimRun{pos: i, n: n, c: c, open: op, close: cl, origN: n})
			i += n
			continue
		}
		_, sz := utf8.DecodeRuneInString(s[i:])
		i += sz
	}
	y.emphasis(runs)
}

// emphasis pairs delimiter runs (a simplified CommonMark algorithm) and
// styles the spans between them.
func (y *styler) emphasis(runs []*delimRun) {
	th := y.th
	var stack []*delimRun
	for _, d := range runs {
		if d.close {
			for d.n > 0 {
				k := -1
				for j := len(stack) - 1; j >= 0; j-- {
					if stack[j].c == d.c && stack[j].n > 0 {
						k = j
						break
					}
				}
				if k < 0 {
					break
				}
				o := stack[k]
				use := 1
				if d.c == '~' || d.c == '=' {
					use = 2
				} else if o.n >= 2 && d.n >= 2 {
					use = 2
				}
				oEnd := o.pos + o.n
				cStart := d.pos + (d.origN - d.n)
				y.faint(oEnd-use, oEnd)
				y.faint(cStart, cStart+use)
				switch {
				case d.c == '~':
					y.fg(oEnd, cStart, th.Faint)
					y.attr(oEnd, cStart, theme.Strike)
				case d.c == '=':
					y.over(oEnd, cStart, th.Highlight)
				case use == 2:
					y.attr(oEnd, cStart, theme.Bold)
				default:
					y.attr(oEnd, cStart, theme.Italic)
				}
				o.n -= use
				d.n -= use
				stack = stack[:k+1]
				if o.n == 0 {
					stack = stack[:k]
				}
			}
		}
		if d.open && d.n > 0 {
			stack = append(stack, d)
		}
	}
}

func (y *styler) code(a, b, n int) {
	th := y.th
	if th.Raised.IsDefault() {
		y.fg(a, b, th.Accent)
		return
	}
	y.bg(a, b, th.Raised)
	y.fg(a, b, th.Text)
	y.faint(a, a+n)
	y.faint(b-n, b)
}

func (y *styler) wikilink(a, b int) {
	th := y.th
	y.faint(a, a+2)
	y.faint(b-2, b)
	y.fg(a+2, b-2, th.Link)
	y.attr(a+2, b-2, th.LinkAttrs)
	inner := y.s[a+2 : b-2]
	if k := strings.IndexByte(inner, '|'); k >= 0 {
		y.fg(a+2, a+2+k, th.Muted)
		y.faint(a+2+k, a+3+k)
	}
	if k := strings.IndexByte(inner, '#'); k >= 0 {
		y.faint(a+2+k, a+3+k)
	}
}

func findBackticks(s string, from, to, n int) int {
	for i := from; i < to; {
		if s[i] != '`' {
			i++
			continue
		}
		k := 0
		for i+k < to && s[i+k] == '`' {
			k++
		}
		if k == n {
			return i
		}
		i += k
	}
	return -1
}

// findMathEnd finds the closing $ of inline math (Pandoc rules: no blank
// before it, no digit after it).
func findMathEnd(s string, from, to int) int {
	for i := from; i < to; i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '$' {
			if s[i-1] == ' ' {
				return -1
			}
			if i+1 < to && s[i+1] >= '0' && s[i+1] <= '9' {
				return -1
			}
			return i
		}
	}
	return -1
}

func matchBracket(s string, open, to int) int {
	depth := 0
	for i := open; i < to; i++ {
		switch s[i] {
		case '\\':
			i++
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func matchParen(s string, open, to int) int {
	depth := 0
	for i := open; i < to; i++ {
		switch s[i] {
		case '\\':
			i++
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		case ' ':
			// a title may follow the URL; spaces are allowed
		}
	}
	return -1
}

func isAutolink(s string) bool {
	if strings.ContainsAny(s, " <") {
		return false
	}
	if i := strings.Index(s, ":"); i > 1 {
		return strings.HasPrefix(s, "http") || strings.HasPrefix(s, "mailto") || strings.HasPrefix(s, "ftp") || strings.HasPrefix(s, "obsidian")
	}
	return strings.Contains(s, "@") && strings.Contains(s, ".")
}

func isHTMLTag(s string) bool {
	if s == "" {
		return false
	}
	t := strings.TrimPrefix(s, "/")
	if t == "" {
		return false
	}
	c := t[0]
	if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
		return false
	}
	for i := 0; i < len(t); i++ {
		ch := t[i]
		if ch == ' ' || ch == '/' {
			break
		}
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-') {
			return false
		}
	}
	return true
}

func isTagBoundary(c byte) bool {
	return c == ' ' || c == '\t' || c == '(' || c == '*' || c == '_' || c == '~' || c == '=' || c == '['
}

// tagEnd returns the end of a #tag body starting at i (or i if none). A
// tag needs one character that is not a digit.
func tagEnd(s string, i, to int) int {
	j := i
	nonDigit := false
	for j < to {
		r, n := utf8.DecodeRuneInString(s[j:])
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '_' || r == '-' || r == '/') {
			break
		}
		if !unicode.IsDigit(r) {
			nonDigit = true
		}
		j += n
	}
	for j > i && s[j-1] == '/' {
		j--
	}
	if !nonDigit {
		return i
	}
	return j
}

// urlEnd returns the end of a bare URL starting at i, or i.
func urlEnd(s string, i, to int) int {
	t := s[i:to]
	if !(strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") || strings.HasPrefix(t, "www.")) {
		return i
	}
	j := i
	for j < to && s[j] != ' ' && s[j] != '\t' && s[j] != '<' {
		j++
	}
	// trailing punctuation is not part of the URL; keep balanced parens
	for j > i {
		c := s[j-1]
		if strings.IndexByte(".,;:!?*_~'\"", c) >= 0 {
			j--
			continue
		}
		if c == ')' && strings.Count(s[i:j], "(") < strings.Count(s[i:j], ")") {
			j--
			continue
		}
		break
	}
	if j-i <= len("https://") {
		return i
	}
	return j
}

func isBlockID(s string) bool {
	s = strings.TrimRight(s, " \t")
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c >= 0x80
}

func isASCIIPunct(c byte) bool {
	return c >= '!' && c <= '/' || c >= ':' && c <= '@' || c >= '[' && c <= '`' || c >= '{' && c <= '~'
}

func isPunctRune(r rune) bool {
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}
