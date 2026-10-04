package editor

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// cmdline is the state of the ":" / "/" / "?" prompt.
type cmdline struct {
	active  bool
	kind    byte
	text    string
	pos     int
	hist    map[byte][]string
	hidx    int
	saveCur Pos
	saveTop viewPos
	// incsearch preview
	preview *searchRE
	prevPat string
}

type searchState struct {
	pattern    string
	forward    bool
	re         *searchRE
	hl         bool
	ignorecase bool
	smartcase  bool
	noSmart    bool // pattern came from * or #: ignorecase without smartcase
}

type substitution struct {
	pattern string
	repl    string
	global  bool
	icase   int // 0 default, 1 ignore (i), 2 match (I)
}

// CommandLineActive reports whether the ":" or search prompt is open.
func (e *Editor) CommandLineActive() bool { return e.cmd.active }

// CommandLine returns the prompt state for a host that draws it itself:
// the prompt character (':', '/' or '?'), the typed text and the cursor's
// byte offset in it.
func (e *Editor) CommandLine() (prompt rune, text string, cursor int, active bool) {
	return rune(e.cmd.kind), e.cmd.text, e.cmd.pos, e.cmd.active
}

func (e *Editor) openCmdline(kind byte) {
	h := e.cmd.hist
	if h == nil {
		h = map[byte][]string{}
	}
	e.cmd = cmdline{active: true, kind: kind, hist: h, saveCur: e.cur, saveTop: e.top}
	e.cmd.hidx = len(h[histKey(kind)])
}

func histKey(k byte) byte {
	if k == '?' {
		return '/'
	}
	return k
}

// cmdInput edits the prompt.
func (e *Editor) cmdInput(in input) Result {
	c := &e.cmd
	opPending := e.pend.op != ""
	if opPending && !e.replaying {
		e.rec = append(e.rec, in)
	}
	if in.tok == "" {
		t := normalizeNewlines(in.paste)
		if i := strings.IndexByte(t, '\n'); i >= 0 {
			t = t[:i]
		}
		c.text = c.text[:c.pos] + t + c.text[c.pos:]
		c.pos += len(t)
		e.incsearch()
		return Result{}
	}
	switch in.tok {
	case "<Esc>", "<C-c>":
		e.cmdCancel()
		return Result{}
	case "<CR>", "<C-j>", "<C-m>":
		return e.cmdExecute()
	case "<BS>":
		if c.text == "" {
			e.cmdCancel()
			return Result{}
		}
		if c.pos > 0 {
			p := prevCol(c.text, c.pos)
			c.text = c.text[:p] + c.text[c.pos:]
			c.pos = p
		}
	case "<Del>":
		if c.pos < len(c.text) {
			c.text = c.text[:c.pos] + c.text[nextCol(c.text, c.pos):]
		}
	case "<C-w>":
		p := c.pos
		for p > 0 && c.text[p-1] == ' ' {
			p--
		}
		if p > 0 {
			k := clusterClass(clusterAt(c.text, prevCol(c.text, p)), false)
			for p > 0 && clusterClass(clusterAt(c.text, prevCol(c.text, p)), false) == k {
				p = prevCol(c.text, p)
			}
		}
		c.text = c.text[:p] + c.text[c.pos:]
		c.pos = p
	case "<C-u>":
		c.text = c.text[c.pos:]
		c.pos = 0
	case "<Left>":
		c.pos = prevCol(c.text, c.pos)
	case "<Right>":
		c.pos = nextCol(c.text, c.pos)
	case "<Home>", "<C-b>":
		c.pos = 0
	case "<End>", "<C-e>":
		c.pos = len(c.text)
	case "<Up>", "<Down>":
		h := c.hist[histKey(c.kind)]
		if in.tok == "<Up>" && c.hidx > 0 {
			c.hidx--
		} else if in.tok == "<Down>" && c.hidx < len(h) {
			c.hidx++
		}
		if c.hidx < len(h) {
			c.text = h[c.hidx]
		} else {
			c.text = ""
		}
		c.pos = len(c.text)
	case "<Tab>":
		c.text = c.text[:c.pos] + "\t" + c.text[c.pos:]
		c.pos++
	default:
		if isChar(in.tok) {
			c.text = c.text[:c.pos] + in.tok + c.text[c.pos:]
			c.pos += len(in.tok)
		}
	}
	e.incsearch()
	return Result{}
}

func (e *Editor) cmdCancel() {
	if e.cmd.kind == '/' || e.cmd.kind == '?' {
		e.cur, e.top = e.cmd.saveCur, e.cmd.saveTop
	}
	e.cmd.active = false
	e.cmd.preview = nil
	e.pend = pending{}
}

// incsearch previews the match of the pattern being typed.
func (e *Editor) incsearch() {
	c := &e.cmd
	if c.kind != '/' && c.kind != '?' {
		return
	}
	e.cur, e.top = c.saveCur, c.saveTop
	c.preview = nil
	if c.text == "" {
		return
	}
	pat := searchPattern(c.text, c.kind)
	if pat == "" {
		return
	}
	re, err := compileVimPattern(pat, e.search.ignorecase, e.search.smartcase)
	if err != nil {
		return
	}
	c.preview = re
	if p, _, ok := e.searchFrom(re, c.saveCur, c.kind == '/', 1); ok {
		e.cur = p
	}
}

func (e *Editor) cmdExecute() Result {
	c := &e.cmd
	text := c.text
	kind := c.kind
	c.active = false
	c.preview = nil
	if text != "" {
		k := histKey(kind)
		h := c.hist[k]
		if len(h) == 0 || h[len(h)-1] != text {
			c.hist[k] = append(h, text)
		}
	}
	if kind == ':' {
		r := e.ex(text)
		return r
	}
	e.cur, e.top = c.saveCur, c.saveTop
	pat := searchPattern(text, kind)
	if pat == "" {
		pat = e.search.pattern
		if pat == "" {
			e.errorf("E35: No previous regular expression")
			e.pend = pending{}
			return Result{}
		}
	}
	e.search.pattern = pat
	e.search.forward = kind == '/'
	e.search.noSmart = false
	re, err := compileVimPattern(pat, e.search.ignorecase, e.search.smartcase)
	if err != nil {
		e.errorf("E383: Invalid search string: %s", pat)
		e.pend = pending{}
		return Result{}
	}
	e.search.re = re
	e.search.hl = true
	count, _ := e.pend.count()
	p, wrapped, ok := e.searchFrom(re, e.cur, e.search.forward, count)
	if !ok {
		e.errorf("E486: Pattern not found: %s", pat)
		e.pend = pending{}
		return Result{}
	}
	if wrapped {
		e.wrapMessage(e.search.forward)
	}
	if e.pend.op != "" {
		e.opWithMotion(motion{pos: p, kind: exclusive}, count)
		e.finishNormal(count)
		return Result{}
	}
	e.pend = pending{}
	if e.mode == ModeVisual || e.mode == ModeVisualLine {
		e.cur = p
	} else {
		e.cur = p
		e.clampCursor()
	}
	e.setWant()
	return Result{}
}

// searchPattern returns the pattern part of a search line: an unescaped
// "/" (or "?" for backward searches) ends it, and what follows is a Vim
// search offset, which is accepted and ignored.
func searchPattern(text string, kind byte) string {
	for i := 0; i < len(text); i++ {
		if text[i] == '\\' {
			i++
			continue
		}
		if text[i] == kind {
			return text[:i]
		}
	}
	return text
}

// finishNormal completes a Normal-mode command that ended outside
// normalInput (an operator whose motion was a search).
func (e *Editor) finishNormal(count int) {
	e.pend = pending{}
	if e.mode != ModeInsert {
		e.buf.commit(e.cur)
		if e.changed && !e.replaying {
			e.saveChange(count)
		}
	} else {
		e.recCount = count
	}
}

func (e *Editor) wrapMessage(forward bool) {
	if forward {
		e.message("search hit BOTTOM, continuing at TOP")
	} else {
		e.message("search hit TOP, continuing at BOTTOM")
	}
}

// searchMotion implements n N * #.
func (e *Editor) searchMotion(tok string, count int) (Pos, bool) {
	switch tok {
	case "*", "#":
		line := e.buf.line(e.cur.Line)
		start, end := wordUnderCursor(line, e.cur.Col)
		if start == end {
			e.errorf("E348: No string under cursor")
			return e.cur, false
		}
		w := line[start:end]
		pat := regexpEscapeVim(w)
		r, _ := utf8.DecodeRuneInString(w)
		if isWordRune(r) {
			pat = `\<` + pat + `\>`
		}
		e.search.pattern = pat
		e.search.forward = tok == "*"
		e.search.noSmart = true
		re, err := compileVimPattern(pat, e.search.ignorecase, false)
		if err != nil {
			return e.cur, false
		}
		e.search.re = re
		e.search.hl = true
		// start from the word's beginning so the current word is skipped
		from := Pos{e.cur.Line, start}
		p, wrapped, ok := e.searchFrom(re, from, e.search.forward, count)
		if wrapped {
			e.wrapMessage(e.search.forward)
		}
		return p, ok
	}
	if e.search.pattern == "" {
		e.errorf("E35: No previous regular expression")
		return e.cur, false
	}
	if e.search.re == nil {
		re, err := compileVimPattern(e.search.pattern, e.search.ignorecase, e.search.smartcase && !e.search.noSmart)
		if err != nil {
			return e.cur, false
		}
		e.search.re = re
	}
	e.search.hl = true
	fwd := e.search.forward
	if tok == "N" {
		fwd = !fwd
	}
	p, wrapped, ok := e.searchFrom(e.search.re, e.cur, fwd, count)
	if !ok {
		e.errorf("E486: Pattern not found: %s", e.search.pattern)
		return e.cur, false
	}
	if wrapped {
		e.wrapMessage(fwd)
	}
	return p, true
}

// wordUnderCursor returns the keyword at or after col on the line (as *
// finds it), or a run of non-blank characters when there is no keyword.
func wordUnderCursor(s string, col int) (int, int) {
	isW := func(c int) bool {
		r, _ := utf8.DecodeRuneInString(s[c:])
		return isWordRune(r)
	}
	c := col
	for c < len(s) && !isW(c) {
		c = nextCol(s, c)
	}
	if c >= len(s) {
		// no keyword: use the non-blank run under the cursor
		c = col
		for c < len(s) && (s[c] == ' ' || s[c] == '\t') {
			c++
		}
		if c >= len(s) {
			return 0, 0
		}
		st := c
		for st > 0 && s[st-1] != ' ' && s[st-1] != '\t' {
			st = prevCol(s, st)
		}
		en := c
		for en < len(s) && s[en] != ' ' && s[en] != '\t' {
			en = nextCol(s, en)
		}
		return st, en
	}
	st := c
	for st > 0 && isW(prevCol(s, st)) {
		st = prevCol(s, st)
	}
	en := c
	for en < len(s) && isW(en) {
		en = nextCol(s, en)
	}
	return st, en
}

// regexpEscapeVim escapes a literal for a Vim magic pattern.
func regexpEscapeVim(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '.', '*', '[', ']', '^', '$', '~', '/':
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// searchFrom finds the count-th match after (or before) p, wrapping around
// the buffer. wrapped reports passing the end.
func (e *Editor) searchFrom(re *searchRE, p Pos, forward bool, count int) (Pos, bool, bool) {
	b := e.buf
	n := b.lineCount()
	wrapped := false
	cur := p
	for k := 0; k < count; k++ {
		found := false
		if forward {
			start := nextCol(b.line(cur.Line), cur.Col)
			if b.line(cur.Line) == "" {
				start = 1
			}
			for i := 0; i <= n; i++ {
				l := (cur.Line + i) % n
				if cur.Line+i >= n {
					wrapped = true
				}
				ms := re.findAll(b.line(l))
				for _, m := range ms {
					if i == 0 && m[0] < start {
						continue
					}
					if i == n && m[0] >= start {
						continue
					}
					cur = Pos{l, m[0]}
					found = true
					break
				}
				if found {
					break
				}
			}
		} else {
			for i := 0; i <= n; i++ {
				l := ((cur.Line-i)%n + n) % n
				if cur.Line-i < 0 {
					wrapped = true
				}
				ms := re.findAll(b.line(l))
				for j := len(ms) - 1; j >= 0; j-- {
					m := ms[j]
					if i == 0 && m[0] >= cur.Col {
						continue
					}
					if i == n && m[0] < cur.Col {
						continue
					}
					cur = Pos{l, m[0]}
					found = true
					break
				}
				if found {
					break
				}
			}
		}
		if !found {
			return p, false, false
		}
	}
	return cur, wrapped, true
}

// searchRE is a compiled Vim pattern. \< and \> at the ends of the pattern
// are checked against Unicode word characters after matching, since RE2's
// \b only knows ASCII.
type searchRE struct {
	re        *regexp.Regexp
	wordStart bool
	wordEnd   bool
}

func (s *searchRE) findAll(line string) [][]int {
	ms := s.re.FindAllStringIndex(line, -1)
	if !s.wordStart && !s.wordEnd {
		return ms
	}
	out := ms[:0]
	for _, m := range ms {
		if s.ok(line, m[0], m[1]) {
			out = append(out, m)
		}
	}
	return out
}

// ok checks the Unicode word boundaries required by \< and \>.
func (s *searchRE) ok(line string, a, b int) bool {
	if s.wordStart {
		if a >= len(line) {
			return false
		}
		r, _ := utf8.DecodeRuneInString(line[a:])
		if !isWordRune(r) {
			return false
		}
		if a > 0 {
			pr, _ := utf8.DecodeLastRuneInString(line[:a])
			if isWordRune(pr) {
				return false
			}
		}
	}
	if s.wordEnd {
		if b == 0 {
			return false
		}
		pr, _ := utf8.DecodeLastRuneInString(line[:b])
		if !isWordRune(pr) {
			return false
		}
		if b < len(line) {
			r, _ := utf8.DecodeRuneInString(line[b:])
			if isWordRune(r) {
				return false
			}
		}
	}
	return true
}

// compileVimPattern translates a Vim "magic" pattern into a Go regexp.
// Smart-case: an upper-case letter in the pattern makes it case-sensitive;
// \c and \C force either way.
func compileVimPattern(pat string, ignorecase, smartcase bool) (*searchRE, error) {
	expr, caseFlag, ws, we := translatePattern(pat)
	icase := ignorecase
	if icase && smartcase && hasUpper(pat) {
		icase = false
	}
	switch caseFlag {
	case 1:
		icase = true
	case 2:
		icase = false
	}
	if icase {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, err
	}
	return &searchRE{re: re, wordStart: ws, wordEnd: we}, nil
}

// hasUpper reports an upper-case letter outside escape sequences.
func hasUpper(p string) bool {
	for i := 0; i < len(p); i++ {
		if p[i] == '\\' {
			i++
			continue
		}
		r, n := utf8.DecodeRuneInString(p[i:])
		if unicode.IsUpper(r) {
			return true
		}
		i += n - 1
	}
	return false
}

func translatePattern(p string) (expr string, caseFlag int, ws, we bool) {
	var sb strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '\\' && i+1 < len(p) {
			i++
			n := p[i]
			switch n {
			case '(':
				sb.WriteString("(")
			case ')':
				sb.WriteString(")")
			case '|':
				sb.WriteString("|")
			case '+':
				sb.WriteString("+")
			case '?', '=':
				sb.WriteString("?")
			case '{':
				j := strings.IndexByte(p[i:], '}')
				if j < 0 {
					sb.WriteString(`\{`)
					continue
				}
				body := p[i+1 : i+j]
				body = strings.TrimSuffix(body, `\`)
				i += j
				lazy := strings.HasPrefix(body, "-")
				body = strings.TrimPrefix(body, "-")
				if body == "" {
					sb.WriteString("*")
				} else {
					sb.WriteString("{" + body + "}")
				}
				if lazy {
					sb.WriteString("?")
				}
			case '<':
				if sb.Len() == 0 {
					ws = true
				} else {
					sb.WriteString(`\b`)
				}
			case '>':
				if i == len(p)-1 {
					we = true
				} else {
					sb.WriteString(`\b`)
				}
			case 's', 'S', 'd', 'D', 'w', 'W', 't':
				sb.WriteByte('\\')
				sb.WriteByte(n)
			case 'a':
				sb.WriteString(`[A-Za-z]`)
			case 'l':
				sb.WriteString(`[a-z]`)
			case 'u':
				sb.WriteString(`[A-Z]`)
			case 'x':
				sb.WriteString(`[0-9A-Fa-f]`)
			case 'h':
				sb.WriteString(`[A-Za-z_]`)
			case 'c':
				caseFlag = 1
			case 'C':
				caseFlag = 2
			case 'v', 'V', 'm', 'M':
			case 'n':
				sb.WriteString(`$`)
			default:
				sb.WriteString(regexp.QuoteMeta(string(n)))
			}
			continue
		}
		switch c {
		case '(', ')', '|', '+', '?', '{', '}':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case '[':
			j := closingBracket(p, i)
			if j < 0 {
				sb.WriteString(`\[`)
				continue
			}
			sb.WriteString(p[i : j+1])
			i = j
		case '*':
			if sb.Len() == 0 {
				sb.WriteString(`\*`)
			} else {
				sb.WriteByte('*')
			}
		case '^':
			if i == 0 {
				sb.WriteByte('^')
			} else {
				sb.WriteString(`\^`)
			}
		case '$':
			if i == len(p)-1 {
				sb.WriteByte('$')
			} else {
				sb.WriteString(`\$`)
			}
		case '.':
			sb.WriteByte('.')
		case '\\':
			sb.WriteString(`\\`)
		default:
			r, n := utf8.DecodeRuneInString(p[i:])
			sb.WriteString(regexp.QuoteMeta(string(r)))
			i += n - 1
		}
	}
	return sb.String(), caseFlag, ws, we
}

// closingBracket finds the ] ending a bracket expression starting at i.
func closingBracket(p string, i int) int {
	j := i + 1
	if j < len(p) && p[j] == '^' {
		j++
	}
	if j < len(p) && p[j] == ']' {
		j++
	}
	for ; j < len(p); j++ {
		if p[j] == '\\' {
			j++
			continue
		}
		if p[j] == ']' {
			return j
		}
	}
	return -1
}

// ex runs an ex command line.
func (e *Editor) ex(text string) Result {
	s := strings.TrimLeft(text, ": \t")
	if s == "" {
		return Result{}
	}
	l1, l2, hasRange, rest, err := e.parseRange(s)
	if err != nil {
		e.errorf("%v", err)
		return Result{}
	}
	rest = strings.TrimLeft(rest, " \t")
	if rest == "" {
		if hasRange {
			e.cur = Pos{l2, firstNonBlank(e.buf.line(l2))}
			e.setWant()
		}
		return Result{}
	}
	name := rest
	i := 0
	for i < len(rest) && (unicode.IsLetter(rune(rest[i]))) {
		i++
	}
	if i == 0 {
		i = 1 // a punctuation command such as & < >
		for i < len(rest) && (rest[i] == '<' || rest[i] == '>') && rest[i] == rest[0] {
			i++
		}
	}
	name = rest[:i]
	arg := rest[i:]
	bang := false
	if strings.HasPrefix(arg, "!") {
		bang = true
		arg = arg[1:]
	}
	arg = strings.TrimSpace(arg)
	if !hasRange {
		l1, l2 = e.cur.Line, e.cur.Line
	}
	switch name {
	case "w", "write":
		if arg != "" {
			return Result{Action: ActionCommand, Command: s}
		}
		return Result{Action: ActionSave}
	case "wq", "x", "xit", "exit", "wqa", "wqall", "xa", "xall":
		if arg != "" {
			return Result{Action: ActionCommand, Command: s}
		}
		return Result{Action: ActionSaveQuit}
	case "q", "quit", "clo", "close":
		if bang {
			return Result{Action: ActionQuitDiscard}
		}
		if e.Dirty() {
			return Result{Message: "E37: No write since last change (add ! to override)", Error: true}
		}
		return Result{Action: ActionQuit}
	case "s", "substitute", "&", "&&":
		if name == "s" || name == "substitute" {
			arg = rest[i:] // the delimiter may be any punctuation, even "!"
		}
		e.changed = true
		e.exSubstitute(name, arg, l1, l2)
		e.buf.commit(e.cur)
		return Result{}
	case "noh", "nohl", "nohlsearch":
		e.search.hl = false
		return Result{}
	case "set", "se":
		e.exSet(arg)
		return Result{}
	case "d", "delete":
		e.buf.groupCursor = e.cur
		e.applyOp("d", textRange{start: Pos{l1, 0}, end: Pos{l2, 0}, linewise: true}, 0, 1)
		e.buf.commit(e.cur)
		return Result{}
	case "y", "yank":
		save := e.cur
		e.applyOp("y", textRange{start: Pos{l1, 0}, end: Pos{l2, 0}, linewise: true}, 0, 1)
		e.cur = save
		return Result{}
	case "j", "join":
		e.buf.groupCursor = e.cur
		n := l2 - l1 + 1
		if !hasRange || n < 2 {
			n = 2
		}
		e.joinLines(l1, n, !bang)
		e.buf.commit(e.cur)
		return Result{}
	case ">", ">>", ">>>", "<", "<<", "<<<":
		e.buf.groupCursor = e.cur
		for k := 0; k < len(name); k++ {
			e.shiftLines(l1, l2, name[0] == '>')
		}
		e.cur = Pos{l2, firstNonBlank(e.buf.line(l2))}
		e.buf.commit(e.cur)
		return Result{}
	case "u", "un", "undo":
		e.normalAction("u", 1, false)
		return Result{}
	case "red", "redo":
		e.normalAction("<C-r>", 1, false)
		return Result{}
	}
	return Result{Action: ActionCommand, Command: s}
}

// parseRange parses an ex line range: N, ., $, %, '< '>, with +N/-N
// offsets, separated by , or ;. Lines are returned 0-based.
func (e *Editor) parseRange(s string) (l1, l2 int, has bool, rest string, err error) {
	n := e.buf.lineCount()
	if strings.HasPrefix(s, "%") {
		return 0, n - 1, true, s[1:], nil
	}
	addr := func(s string) (int, string, bool, error) {
		line := e.cur.Line
		ok := false
		switch {
		case s == "":
			return line, s, false, nil
		case s[0] == '.':
			ok = true
			s = s[1:]
		case s[0] == '$':
			line, ok = n-1, true
			s = s[1:]
		case strings.HasPrefix(s, "'<"):
			line, ok = e.visStart, true
			s = s[2:]
		case strings.HasPrefix(s, "'>"):
			line, ok = e.visEnd, true
			s = s[2:]
		case s[0] >= '0' && s[0] <= '9':
			j := 0
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			v, _ := strconv.Atoi(s[:j])
			line, ok = v-1, true
			s = s[j:]
		}
		for len(s) > 0 && (s[0] == '+' || s[0] == '-') {
			sign := 1
			if s[0] == '-' {
				sign = -1
			}
			j := 1
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			v := 1
			if j > 1 {
				v, _ = strconv.Atoi(s[1:j])
			}
			line += sign * v
			ok = true
			s = s[j:]
		}
		return line, s, ok, nil
	}
	a, s2, okA, _ := addr(s)
	if !okA {
		return e.cur.Line, e.cur.Line, false, s, nil
	}
	b := a
	if len(s2) > 0 && (s2[0] == ',' || s2[0] == ';') {
		var okB bool
		b, s2, okB, _ = addr(s2[1:])
		if !okB {
			b = e.cur.Line
		}
	}
	if a > b {
		a, b = b, a
	}
	if a < 0 || b >= n {
		if a < 0 {
			a = 0
		}
		if b >= n {
			if strings.TrimSpace(s2) == "" {
				b = n - 1 // :999 goes to the last line, as in Vim
			} else {
				return 0, 0, false, "", fmt.Errorf("E16: Invalid range")
			}
		}
	}
	return a, b, true, s2, nil
}

// exSubstitute parses and runs :s/pat/rep/flags (and :& / :&&).
func (e *Editor) exSubstitute(name, arg string, l1, l2 int) {
	var sub substitution
	flags := ""
	switch {
	case name == "&" || name == "&&" || strings.TrimSpace(arg) == "" || isAlnumStart(arg):
		if e.lastSub == nil {
			e.errorf("E35: No previous regular expression")
			return
		}
		sub = *e.lastSub
		sub.global = false
		flags = strings.TrimSpace(arg)
		if name == "&&" {
			sub.global = e.lastSub.global
		}
	default:
		d := arg[0]
		parts := splitDelim(arg[1:], d)
		sub.pattern = parts[0]
		if len(parts) > 1 {
			sub.repl = parts[1]
		}
		if len(parts) > 2 {
			flags = parts[2]
		}
	}
	for _, f := range strings.TrimSpace(flags) {
		switch f {
		case 'g':
			sub.global = !sub.global
		case 'i':
			sub.icase = 1
		case 'I':
			sub.icase = 2
		case '&':
			if e.lastSub != nil {
				sub.global = e.lastSub.global
			}
		case 'c':
			e.message("confirm (c) is not supported; substituting all")
		}
	}
	if sub.pattern == "" {
		sub.pattern = e.search.pattern
		if sub.pattern == "" {
			e.errorf("E35: No previous regular expression")
			return
		}
	}
	e.lastSub = &sub
	e.search.pattern = sub.pattern
	e.search.re = nil
	e.search.hl = true
	e.search.noSmart = false
	e.buf.groupCursor = e.cur
	e.substitute(l1, l2, sub)
}

func isAlnumStart(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '"' || r == '|'
}

// splitDelim splits s on unescaped d (an escaped delimiter becomes the
// delimiter itself), into at most three parts.
func splitDelim(s string, d byte) []string {
	var parts []string
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			if s[i+1] == d {
				sb.WriteByte(d)
			} else {
				sb.WriteByte(c)
				sb.WriteByte(s[i+1])
			}
			i++
			continue
		}
		if c == d && len(parts) < 2 {
			parts = append(parts, sb.String())
			sb.Reset()
			continue
		}
		sb.WriteByte(c)
	}
	return append(parts, sb.String())
}

// substitute applies a substitution to lines l1..l2.
func (e *Editor) substitute(l1, l2 int, sub substitution) {
	ic := e.search.ignorecase
	sc := e.search.smartcase
	switch sub.icase {
	case 1:
		ic, sc = true, false
	case 2:
		ic = false
	}
	re, err := compileVimPattern(sub.pattern, ic, sc)
	if err != nil {
		e.errorf("E486: Invalid pattern: %s", sub.pattern)
		return
	}
	b := e.buf
	type edit struct {
		line int
		text string
	}
	var edits []edit
	total := 0
	for l := l1; l <= l2 && l < b.lineCount(); l++ {
		s := b.line(l)
		ms := re.re.FindAllStringSubmatchIndex(s, -1)
		if len(ms) == 0 {
			continue
		}
		var sb strings.Builder
		last := 0
		n := 0
		for _, m := range ms {
			if !re.ok(s, m[0], m[1]) {
				continue
			}
			sb.WriteString(s[last:m[0]])
			sb.WriteString(expandReplacement(sub.repl, s, m))
			last = m[1]
			n++
			if !sub.global {
				break
			}
		}
		if n == 0 {
			continue
		}
		sb.WriteString(s[last:])
		edits = append(edits, edit{l, sb.String()})
		total += n
	}
	if len(edits) == 0 {
		e.errorf("E486: Pattern not found: %s", sub.pattern)
		return
	}
	// apply bottom-up so line numbers stay valid when replacements add lines
	for i := len(edits) - 1; i >= 0; i-- {
		ed := edits[i]
		b.replace(Pos{ed.line, 0}, Pos{ed.line, len(b.line(ed.line))}, ed.text)
	}
	lastLine := edits[len(edits)-1].line
	for i := 0; i < len(edits)-1; i++ {
		lastLine += strings.Count(edits[i].text, "\n")
	}
	lastLine += strings.Count(edits[len(edits)-1].text, "\n")
	lastLine = clampInt(lastLine, 0, b.lineCount()-1)
	e.cur = Pos{lastLine, firstNonBlank(b.line(lastLine))}
	e.setWant()
	if total > 1 {
		lines := "line"
		if len(edits) > 1 {
			lines = "lines"
		}
		subs := "substitution"
		if total > 1 {
			subs = "substitutions"
		}
		e.message("%d %s on %d %s", total, subs, len(edits), lines)
	}
}

// expandReplacement builds the replacement for one match: & and \0 are the
// whole match, \1…\9 groups, \r and \n a line break, \t a tab.
func expandReplacement(repl, s string, m []int) string {
	var sb strings.Builder
	group := func(k int) string {
		if 2*k+1 < len(m) && m[2*k] >= 0 {
			return s[m[2*k]:m[2*k+1]]
		}
		return ""
	}
	for i := 0; i < len(repl); i++ {
		c := repl[i]
		switch {
		case c == '&':
			sb.WriteString(group(0))
		case c == '\\' && i+1 < len(repl):
			i++
			n := repl[i]
			switch {
			case n >= '0' && n <= '9':
				sb.WriteString(group(int(n - '0')))
			case n == 'r' || n == 'n':
				sb.WriteByte('\n')
			case n == 't':
				sb.WriteByte('\t')
			default:
				sb.WriteByte(n)
			}
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// exSet handles :set options.
func (e *Editor) exSet(arg string) {
	for _, opt := range strings.Fields(arg) {
		name, val, hasVal := strings.Cut(opt, "=")
		switch name {
		case "nu", "number":
			e.SetNumber(true)
		case "nonu", "nonumber":
			e.SetNumber(false)
		case "nu!", "number!", "invnu", "invnumber":
			e.SetNumber(!e.number)
		case "wrap", "measure", "tw", "textwidth":
			if !hasVal {
				if name == "wrap" {
					e.SetMeasure(78)
					continue
				}
				e.message("%s=%d", name, e.measure)
				continue
			}
			n, err := strconv.Atoi(val)
			if err != nil || n < 10 {
				e.errorf("E521: Number required after =: %s", opt)
				continue
			}
			e.SetMeasure(n)
		case "nowrap":
			e.errorf("nowrap is not supported: the editor always soft-wraps")
		case "hls", "hlsearch":
			e.search.hl = true
		case "nohls", "nohlsearch":
			e.search.hl = false
		case "ic", "ignorecase":
			e.search.ignorecase = true
			e.search.re = nil
		case "noic", "noignorecase":
			e.search.ignorecase = false
			e.search.re = nil
		case "scs", "smartcase":
			e.search.smartcase = true
			e.search.re = nil
		case "noscs", "nosmartcase":
			e.search.smartcase = false
			e.search.re = nil
		default:
			e.errorf("E518: Unknown option: %s", opt)
		}
	}
}
