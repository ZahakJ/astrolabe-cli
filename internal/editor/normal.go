package editor

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type mkind int

const (
	exclusive mkind = iota
	inclusive
	linewise
)

// motion is the result of a cursor motion.
type motion struct {
	pos      Pos
	kind     mkind
	vertical bool // keep the remembered column
	wantEnd  bool // $: stick to the end of lines
	fail     bool
}

// normalInput handles one input in Normal mode.
func (e *Editor) normalInput(in input) Result {
	p := &e.pend
	if in.tok == "" {
		if in.paste != "" && p.empty() {
			e.pasteNormal(in.paste)
		}
		return Result{}
	}
	if len(p.keys) == 0 {
		e.rec = e.rec[:0]
		e.changed = false
		e.buf.groupCursor = e.cur
	}
	p.keys = append(p.keys, displayToken(in.tok))
	res, done := e.normalStep(in.tok)
	if done {
		count, _ := p.count()
		e.pend = pending{}
		if e.mode != ModeInsert && !e.cmd.active {
			e.buf.commit(e.cur)
			if e.changed && !e.replaying {
				e.saveChange(count)
			}
		}
		if e.mode == ModeInsert {
			e.recCount = count
		}
	}
	return res
}

func (e *Editor) saveChange(count int) {
	in := make([]input, len(e.rec))
	copy(in, e.rec)
	e.lastChange = &change{inputs: in, count: count}
}

func displayToken(tok string) string {
	if tok == " " {
		return "␣"
	}
	return tok
}

func (e *Editor) recTok(tok string) { e.rec = append(e.rec, input{tok: tok}) }

// normalStep advances the pending command by one token. done reports that
// the command finished (executed, or cancelled).
func (e *Editor) normalStep(tok string) (Result, bool) {
	p := &e.pend
	if tok == "<Esc>" || tok == "<C-c>" {
		if p.empty() && len(p.keys) == 1 {
			return Result{Action: ActionLeave}, true
		}
		return Result{}, true
	}
	count, hasCount := p.count()

	switch p.prefix {
	case `"`:
		p.prefix = ""
		if !isChar(tok) {
			return Result{}, true
		}
		e.recTok(tok)
		p.reg, _ = utf8.DecodeRuneInString(tok)
		return Result{}, false
	case "f", "F", "t", "T":
		pre := p.prefix
		p.prefix = ""
		if !isChar(tok) {
			return Result{}, true
		}
		e.recTok(tok)
		e.lastFind = &findState{target: tok, forward: pre == "f" || pre == "t", till: pre == "t" || pre == "T"}
		m := e.findMotion(*e.lastFind, count, false)
		return e.finishMotion(m, count), true
	case "r":
		p.prefix = ""
		if !isChar(tok) && tok != "<CR>" && tok != "<Tab>" {
			return Result{}, true
		}
		e.recTok(tok)
		e.replaceChars(tok, count)
		return Result{}, true
	case "g":
		p.prefix = ""
		e.recTok(tok)
		return e.gCommand(tok, count, hasCount)
	case "z":
		p.prefix = ""
		switch tok {
		case "z", ".":
			e.scrollCursorTo(e.height / 2)
		case "t", "<CR>":
			e.scrollCursorTo(0)
		case "b", "-":
			e.scrollCursorTo(e.height - 1)
		}
		return Result{}, true
	case "Z":
		p.prefix = ""
		switch tok {
		case "Z":
			return Result{Action: ActionSaveQuit}, true
		case "Q":
			return Result{Action: ActionQuitDiscard}, true
		}
		return Result{}, true
	case "i", "a":
		inner := p.prefix == "i"
		p.prefix = ""
		e.recTok(tok)
		r, ok := e.textObject(tok, inner, count)
		if !ok {
			return Result{}, true
		}
		e.applyOp(p.op, r, p.reg, 1)
		return Result{}, true
	}

	// counts
	if len(tok) == 1 && tok[0] >= '0' && tok[0] <= '9' {
		d := int(tok[0] - '0')
		if p.op == "" {
			if d != 0 || p.count1 > 0 {
				p.count1 = clampCount(p.count1*10 + d)
				return Result{}, false
			}
		} else if d != 0 || p.count2 > 0 {
			p.count2 = clampCount(p.count2*10 + d)
			return Result{}, false
		}
	}

	if p.op != "" {
		return e.operatorPending(tok, count, hasCount)
	}

	e.recTok(tok)
	switch tok {
	case `"`:
		p.prefix = `"`
		return Result{}, false
	case "d", "c", "y", ">", "<":
		p.op = tok
		return Result{}, false
	case "f", "F", "t", "T", "r", "g", "z", "Z":
		p.prefix = tok
		return Result{}, false
	}
	if m, ok := e.doMotion(tok, "", count, hasCount, ""); ok {
		if m.fail {
			return Result{}, true
		}
		e.applyMotion(m)
		return Result{}, true
	}
	return e.normalAction(tok, count, hasCount)
}

func clampCount(n int) int {
	if n > 99999 {
		return 99999
	}
	return n
}

// operatorPending handles the token after an operator.
func (e *Editor) operatorPending(tok string, count int, hasCount bool) (Result, bool) {
	p := &e.pend
	e.recTok(tok)
	switch {
	case tok == p.op || (len(p.op) == 2 && tok == p.op[1:]):
		// dd cc yy >> << g~~ guu gUU: count lines
		last := e.cur.Line + count - 1
		if last >= e.buf.lineCount() {
			last = e.buf.lineCount() - 1
		}
		e.applyOp(p.op, textRange{start: Pos{e.cur.Line, 0}, end: Pos{last, 0}, linewise: true}, p.reg, 1)
		return Result{}, true
	case tok == "i" || tok == "a":
		p.prefix = tok
		return Result{}, false
	case tok == "f" || tok == "F" || tok == "t" || tok == "T":
		p.prefix = tok
		return Result{}, false
	case tok == "g":
		p.prefix = "g"
		return Result{}, false
	case tok == "/" || tok == "?":
		e.openCmdline(tok[0])
		return Result{}, false
	}
	m, ok := e.doMotion(tok, "", count, hasCount, p.op)
	if !ok || m.fail {
		return Result{}, true
	}
	e.opWithMotion(m, count)
	return Result{}, true
}

// opWithMotion applies the pending operator over the motion from the
// cursor.
func (e *Editor) opWithMotion(m motion, count int) {
	r, ok := e.motionRange(e.cur, m, e.pend.op)
	if !ok {
		return
	}
	// The count belonged to the motion; operators run once.
	e.applyOp(e.pend.op, r, e.pend.reg, 1)
}

// finishMotion is used by prefixed motions (f, t, gg …): move or operate.
func (e *Editor) finishMotion(m motion, count int) Result {
	if m.fail {
		return Result{}
	}
	if e.pend.op != "" {
		e.opWithMotion(m, count)
		return Result{}
	}
	e.applyMotion(m)
	return Result{}
}

// applyMotion moves the cursor.
func (e *Editor) applyMotion(m motion) {
	e.cur = m.pos
	if m.wantEnd {
		e.want = math.MaxInt32
	}
	e.clampCursor()
	switch {
	case m.wantEnd:
		e.want, e.wantX = math.MaxInt32, math.MaxInt32
	case !m.vertical:
		e.setWant()
	}
}

// motionRange turns a motion from p into the range an operator acts on,
// applying Vim's rules for exclusive motions that end in column 0 and the
// d{motion} linewise exception.
func (e *Editor) motionRange(from Pos, m motion, op string) (textRange, bool) {
	if m.kind == linewise {
		a, b := from.Line, m.pos.Line
		if a > b {
			a, b = b, a
		}
		return textRange{start: Pos{a, 0}, end: Pos{b, 0}, linewise: true}, true
	}
	start, end := minPos(from, m.pos), maxPos(from, m.pos)
	if m.kind == inclusive {
		l := e.buf.line(end.Line)
		if end.Col < len(l) {
			end.Col = nextCol(l, end.Col)
		}
	} else if end.Col == 0 && end.Line > start.Line {
		end.Line--
		if isBlank(e.buf.line(start.Line)[:start.Col]) {
			return textRange{start: Pos{start.Line, 0}, end: Pos{end.Line, 0}, linewise: true}, true
		}
		end.Col = len(e.buf.line(end.Line))
	}
	if !start.Less(end) && m.kind != inclusive {
		return textRange{}, false
	}
	if op == "d" && start.Line != end.Line &&
		isBlank(e.buf.line(start.Line)[:start.Col]) && isBlank(e.buf.line(end.Line)[end.Col:]) {
		return textRange{start: Pos{start.Line, 0}, end: Pos{end.Line, 0}, linewise: true}, true
	}
	return textRange{start: start, end: end}, true
}

// doMotion computes a motion. ok is false when tok is not a motion.
func (e *Editor) doMotion(tok, arg string, count int, hasCount bool, op string) (motion, bool) {
	b := e.buf
	cur := e.cur
	line := b.line(cur.Line)
	visual := e.mode == ModeVisual || e.mode == ModeVisualLine
	m := motion{pos: cur, kind: exclusive}
	switch tok {
	case "h", "<Left>":
		if cur.Col == 0 {
			m.fail = true
			break
		}
		c := cur.Col
		for i := 0; i < count && c > 0; i++ {
			c = prevCol(line, c)
		}
		m.pos.Col = c
	case "l", "<Right>":
		c := cur.Col
		limit := lastCol(line)
		if op != "" || e.mode == ModeInsert {
			limit = len(line)
		}
		if c >= limit {
			m.fail = op == "" || line == ""
			break
		}
		for i := 0; i < count && c < limit; i++ {
			c = nextCol(line, c)
		}
		m.pos.Col = c
	case "<BS>":
		w := walker{b: b, p: cur}
		for i := 0; i < count; i++ {
			if w.dec() == -1 {
				break
			}
			if w.p.Col == len(b.line(w.p.Line)) && w.p.Col > 0 && op == "" {
				w.p.Col = lastCol(b.line(w.p.Line))
			}
		}
		m.pos = w.p
	case " ":
		w := walker{b: b, p: cur}
		for i := 0; i < count; i++ {
			r := w.inc()
			if r == -1 {
				break
			}
			if r == 2 && op == "" {
				if w.inc() == -1 {
					w.p.Col = lastCol(b.line(w.p.Line))
					break
				}
			}
		}
		m.pos = w.p
	case "j", "<Down>", "<C-n>", "<C-j>", "k", "<Up>", "<C-p>":
		down := tok == "j" || tok == "<Down>" || tok == "<C-n>" || tok == "<C-j>"
		if op == "" && !hasCount && e.mode != ModeVisualLine {
			if !e.displayMove(down) {
				m.fail = true
			}
			// displayMove already placed the cursor
			return motion{pos: e.cur, kind: linewise, vertical: true, fail: m.fail}, true
		}
		l := cur.Line
		if down {
			l += count
		} else {
			l -= count
		}
		if l < 0 || l >= b.lineCount() {
			if (down && cur.Line == b.lineCount()-1) || (!down && cur.Line == 0) {
				m.fail = true
				break
			}
			l = clampInt(l, 0, b.lineCount()-1)
		}
		m.pos = Pos{l, colAtVcol(b.line(l), e.want, false)}
		m.kind = linewise
		m.vertical = true
	case "w", "W", "e", "E", "b", "B":
		big := tok == "W" || tok == "E" || tok == "B"
		w := walker{b: b, p: cur, big: big}
		switch tok {
		case "w", "W":
			if op == "c" {
				// cw on a word is ce (stopping at the end of the current
				// word); on blanks it is dw (Vim without cpo-w).
				if c := w.cls(); line != "" && cur.Col < len(line) && c != 0 {
					w.endWord(count, true, false)
					m.pos = w.p
					m.kind = inclusive
					break
				}
			}
			w.fwdWord(count, op != "")
			m.pos = w.p
			if w.p.Col >= len(b.line(w.p.Line)) && w.p.Col > 0 && cur.Less(w.p) {
				// Do not leave the cursor on the end-of-line position.
				m.pos.Col = lastCol(b.line(w.p.Line))
				if op != "" {
					m.kind = inclusive
				}
			}
		case "e", "E":
			w.endWord(count, false, false)
			m.pos = w.p
			m.kind = inclusive
		case "b", "B":
			if !w.bckWord(count, false) && w.p == cur {
				m.fail = true
			}
			m.pos = w.p
		}
	case "ge", "gE":
		w := walker{b: b, p: cur, big: tok == "gE"}
		w.bckendWord(count, false)
		m.pos = w.p
		m.kind = inclusive
	case "0", "<Home>":
		m.pos.Col = 0
	case "^":
		m.pos.Col = firstNonBlank(line)
	case "g_":
		l := clampInt(cur.Line+count-1, 0, b.lineCount()-1)
		s := b.line(l)
		c := len(strings.TrimRight(s, " \t"))
		m.pos = Pos{l, prevCol(s, c)}
		m.kind = inclusive
	case "$", "<End>":
		l := clampInt(cur.Line+count-1, 0, b.lineCount()-1)
		s := b.line(l)
		// In Visual mode $ selects up to and including the line break.
		m.pos = Pos{l, len(s)}
		if op == "" && !visual {
			m.pos.Col = lastCol(s)
		}
		m.kind = inclusive
		m.wantEnd = true
	case "|":
		m.pos.Col = colAtVcol(line, count-1, false)
	case "G", "gg":
		l := b.lineCount() - 1
		if tok == "gg" {
			l = 0
		}
		if hasCount {
			l = clampInt(count-1, 0, b.lineCount()-1)
		}
		m.pos = Pos{l, firstNonBlank(b.line(l))}
		m.kind = linewise
	case "<CR>", "+", "<C-m>", "-", "_":
		l := cur.Line
		switch tok {
		case "-":
			l -= count
		case "_":
			l += count - 1
		default:
			l += count
		}
		if l < 0 || l >= b.lineCount() {
			m.fail = true
			break
		}
		m.pos = Pos{l, firstNonBlank(b.line(l))}
		m.kind = linewise
	case "%":
		if hasCount {
			if count > 100 {
				m.fail = true
				break
			}
			l := (count*b.lineCount() + 99) / 100
			l = clampInt(l-1, 0, b.lineCount()-1)
			m.pos = Pos{l, firstNonBlank(b.line(l))}
			m.kind = linewise
			break
		}
		p, ok := matchPair(b, cur)
		if !ok {
			m.fail = true
			break
		}
		m.pos = p
		m.kind = inclusive
	case "}":
		p, atEnd := paragraphFwd(b, cur.Line, count)
		m.pos = p
		if atEnd {
			if op != "" {
				m.pos.Col = len(b.line(p.Line))
			}
			m.kind = inclusive
			if op != "" {
				m.kind = exclusive
			}
		}
	case "{":
		m.pos = paragraphBack(b, cur.Line, count)
	case ";", ",":
		if e.lastFind == nil {
			m.fail = true
			break
		}
		f := *e.lastFind
		if tok == "," {
			f.forward = !f.forward
		}
		m = e.findMotion(f, count, true)
	case "H", "M", "L":
		m.pos = e.screenLine(tok, count, hasCount)
		m.kind = linewise
	case "n", "N", "*", "#":
		p, ok := e.searchMotion(tok, count)
		if !ok {
			m.fail = true
			break
		}
		m.pos = p
	default:
		return m, false
	}
	return m, true
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (e *Editor) findMotion(f findState, count int, repeat bool) motion {
	line := e.buf.line(e.cur.Line)
	col, ok := findChar(line, e.cur.Col, f.target, f.forward, f.till, repeat, count)
	m := motion{pos: Pos{e.cur.Line, col}, kind: exclusive, fail: !ok}
	if f.forward {
		m.kind = inclusive
	}
	return m
}

// gCommand handles the key after g.
func (e *Editor) gCommand(tok string, count int, hasCount bool) (Result, bool) {
	p := &e.pend
	switch tok {
	case "~", "u", "U":
		if p.op == "" {
			p.op = "g" + tok
			return Result{}, false
		}
		if p.op == "g"+tok {
			return e.operatorPending(tok, count, hasCount)
		}
		return Result{}, true
	case "g", "e", "E", "_":
		m, _ := e.doMotion("g"+tok, "", count, hasCount, p.op)
		if tok == "g" {
			m, _ = e.doMotion("gg", "", count, hasCount, p.op)
		}
		return e.finishMotion(m, count), true
	case "j", "k":
		if p.op == "" {
			for i := 0; i < count; i++ {
				e.displayMove(tok == "j")
			}
			return Result{}, true
		}
		m, _ := e.doMotion(tok, "", count, true, p.op)
		return e.finishMotion(m, count), true
	case "J":
		if p.op != "" {
			return Result{}, true
		}
		e.changed = true
		e.joinLines(e.cur.Line, max(count, 2), false)
		return Result{}, true
	case "v":
		if p.op != "" || !e.lastVisual.set {
			return Result{}, true
		}
		e.anchor = e.buf.clampPos(e.lastVisual.anchor)
		e.cur = e.buf.clampPos(e.lastVisual.cur)
		e.mode = e.lastVisual.mode
		e.clampCursor()
		return Result{}, true
	case "I":
		if p.op != "" {
			return Result{}, true
		}
		e.changed = true
		e.cur.Col = 0
		e.startInsert("I", count)
		return Result{}, true
	}
	return Result{}, true
}

// normalAction runs a Normal-mode command that is not a motion.
func (e *Editor) normalAction(tok string, count int, hasCount bool) (Result, bool) {
	b := e.buf
	line := b.line(e.cur.Line)
	reg := e.pend.reg
	switch tok {
	case "i", "<Insert>":
		e.changed = true
		e.startInsert("i", count)
	case "a":
		e.changed = true
		if line != "" {
			e.cur.Col = nextCol(line, e.cur.Col)
		}
		e.startInsert("a", count)
	case "A":
		e.changed = true
		e.cur.Col = len(line)
		e.startInsert("A", count)
	case "I":
		e.changed = true
		e.cur.Col = firstNonBlankInsert(line)
		e.startInsert("I", count)
	case "o", "O":
		e.changed = true
		e.openLine(tok == "o")
		e.startInsert(tok, count)
	case "x", "<Del>":
		if line == "" {
			return Result{}, true
		}
		e.changed = true
		end := e.cur.Col
		for i := 0; i < count && end < len(line); i++ {
			end = nextCol(line, end)
		}
		e.applyOp("d", textRange{start: e.cur, end: Pos{e.cur.Line, end}}, reg, 1)
	case "X":
		if e.cur.Col == 0 {
			return Result{}, true
		}
		e.changed = true
		start := e.cur.Col
		for i := 0; i < count && start > 0; i++ {
			start = prevCol(line, start)
		}
		e.applyOp("d", textRange{start: Pos{e.cur.Line, start}, end: e.cur}, reg, 1)
	case "s":
		e.changed = true
		end := e.cur.Col
		for i := 0; i < count && end < len(line); i++ {
			end = nextCol(line, end)
		}
		e.applyOp("c", textRange{start: e.cur, end: Pos{e.cur.Line, end}}, reg, 1)
		if e.mode != ModeInsert {
			e.startInsert("s", 1)
		}
	case "S":
		e.changed = true
		last := clampInt(e.cur.Line+count-1, 0, b.lineCount()-1)
		e.applyOp("c", textRange{start: Pos{e.cur.Line, 0}, end: Pos{last, 0}, linewise: true}, reg, 1)
	case "C", "D", "Y":
		last := clampInt(e.cur.Line+count-1, 0, b.lineCount()-1)
		end := Pos{last, len(b.line(last))}
		op := map[string]string{"C": "c", "D": "d", "Y": "y"}[tok]
		if tok != "Y" {
			e.changed = true
		}
		if !e.cur.Less(end) {
			if tok == "C" {
				e.cur.Col = len(line)
				e.startInsert("C", 1)
			}
			return Result{}, true
		}
		e.applyOp(op, textRange{start: e.cur, end: end}, reg, 1)
	case "J":
		e.changed = true
		e.joinLines(e.cur.Line, max(count, 2), true)
	case "~":
		if line == "" {
			return Result{}, true
		}
		e.changed = true
		end := e.cur.Col
		for i := 0; i < count && end < len(line); i++ {
			end = nextCol(line, end)
		}
		e.buf.replace(e.cur, Pos{e.cur.Line, end}, toggleCase(line[e.cur.Col:end]))
		e.cur.Col = end
		e.clampCursor()
		e.setWant()
	case "p", "P":
		e.changed = true
		e.put(e.readReg(reg), tok == "p", count)
	case "u":
		for i := 0; i < count; i++ {
			p, ok := b.undoOne()
			if !ok {
				if i == 0 {
					e.message("Already at oldest change")
				}
				break
			}
			e.cur = p
		}
		e.clampCursor()
		e.setWant()
	case "<C-r>":
		for i := 0; i < count; i++ {
			p, ok := b.redoOne()
			if !ok {
				if i == 0 {
					e.message("Already at newest change")
				}
				break
			}
			e.cur = p
		}
		e.clampCursor()
		e.setWant()
	case ".":
		c := 0
		if hasCount {
			c = count
		}
		e.pend = pending{}
		e.repeat(c)
		e.changed = false
	case "v", "V":
		e.anchor = e.cur
		if tok == "v" {
			e.mode = ModeVisual
		} else {
			e.mode = ModeVisualLine
		}
	case ":":
		e.openCmdline(':')
		if hasCount {
			e.cmd.text = ".,.+" + strconv.Itoa(count-1)
			e.cmd.pos = len(e.cmd.text)
		}
		return Result{}, true
	case "/", "?":
		e.openCmdline(tok[0])
		return Result{}, true
	case "&":
		e.changed = true
		if e.lastSub == nil {
			e.errorf("E35: No previous regular expression")
			break
		}
		s := *e.lastSub
		s.global = false
		e.substitute(e.cur.Line, e.cur.Line, s)
	case "<C-s>":
		return Result{Action: ActionSave}, true
	case "<C-d>", "<C-u>":
		e.scrollHalf(tok == "<C-d>", count, hasCount)
	case "<C-f>", "<PageDown>":
		e.scrollPage(true, count)
	case "<C-b>", "<PageUp>":
		e.scrollPage(false, count)
	case "<C-e>":
		e.scrollLines(count)
	case "<C-y>":
		e.scrollLines(-count)
	case "<C-g>":
		e.message("line %d of %d --%d%%--", e.cur.Line+1, b.lineCount(), (e.cur.Line+1)*100/b.lineCount())
	}
	return Result{}, true
}

// firstNonBlankInsert is like firstNonBlank but returns the line length for
// an all-blank line (where I appends).
func firstNonBlankInsert(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' {
			return i
		}
	}
	return len(s)
}

// textObject resolves iw/aw/ip/i(/… at the cursor.
func (e *Editor) textObject(tok string, inner bool, count int) (textRange, bool) {
	switch tok {
	case "w", "W":
		return wordObject(e.buf, e.cur, count, inner, tok == "W")
	case "p":
		return paragraphObject(e.buf, e.cur.Line, count, inner)
	case `"`, "'", "`":
		return quoteObject(e.buf, e.cur, tok[0], inner)
	case "(", ")", "b":
		return blockObject(e.buf, e.cur, '(', ')', inner, count)
	case "[", "]":
		return blockObject(e.buf, e.cur, '[', ']', inner, count)
	case "{", "}", "B":
		return blockObject(e.buf, e.cur, '{', '}', inner, count)
	case "<", ">":
		return blockObject(e.buf, e.cur, '<', '>', inner, count)
	}
	return textRange{}, false
}

// setReg stores text in a register following Vim's rules for the unnamed,
// yank ("0), named (a–z, A–Z appends), black-hole ("_) and clipboard ("+,
// "*) registers.
func (e *Editor) setReg(reg rune, text string, lw bool, yank bool) {
	if reg == '_' {
		return
	}
	r := register{text: text, linewise: lw}
	switch {
	case reg >= 'A' && reg <= 'Z':
		low := unicode.ToLower(reg)
		old := e.regs[low]
		if old.text != "" {
			if old.linewise || lw {
				r = register{text: old.text + "\n" + text, linewise: true}
			} else {
				r = register{text: old.text + text}
			}
		}
		e.regs[low] = r
	case reg >= 'a' && reg <= 'z':
		e.regs[reg] = r
	case reg == '+' || reg == '*':
		e.regs['+'] = r
		if e.clipboard != nil {
			out := text
			if lw {
				out += "\n"
			}
			if err := e.clipboard(out); err != nil {
				e.message("copied (the terminal may not support OSC 52)")
			}
		}
	}
	e.regs['"'] = r
	if yank && (reg == 0 || reg == '"') {
		e.regs['0'] = r
	}
}

func (e *Editor) readReg(reg rune) register {
	switch {
	case reg == 0:
		return e.regs['"']
	case reg == '*':
		return e.regs['+']
	case reg >= 'A' && reg <= 'Z':
		return e.regs[unicode.ToLower(reg)]
	}
	return e.regs[reg]
}

// applyOp runs an operator over a range.
func (e *Editor) applyOp(op string, r textRange, reg rune, count int) {
	b := e.buf
	switch op {
	case "y":
		text := e.rangeText(r)
		e.setReg(reg, text, r.linewise, true)
		if r.linewise {
			if r.start.Line < e.cur.Line || e.mode == ModeVisualLine || e.mode == ModeVisual {
				e.cur.Line = r.start.Line
				if e.mode != ModeNormal {
					e.cur.Col = 0
				}
			}
		} else if r.start.Less(e.cur) || e.mode != ModeNormal {
			e.cur = r.start
		}
		if e.mode != ModeNormal {
			e.mode = ModeNormal
		}
		e.clampCursor()
		e.setWant()
		if n := r.end.Line - r.start.Line + 1; r.linewise && n > 2 {
			e.message("%d lines yanked", n)
		}
	case "d":
		e.changed = true
		text := e.rangeText(r)
		e.setReg(reg, text, r.linewise, false)
		e.mode = ModeNormal
		if r.linewise {
			e.deleteLines(r.start.Line, r.end.Line)
			l := clampInt(r.start.Line, 0, b.lineCount()-1)
			e.cur = Pos{l, firstNonBlank(b.line(l))}
		} else {
			b.delete(r.start, r.end)
			e.cur = r.start
		}
		e.clampCursor()
		e.setWant()
		if n := r.end.Line - r.start.Line + 1; r.linewise && n > 2 {
			e.message("%d fewer lines", n)
		}
	case "c":
		e.changed = true
		text := e.rangeText(r)
		e.setReg(reg, text, r.linewise, false)
		e.mode = ModeNormal
		if r.linewise {
			ind := leadingWS(b.line(r.start.Line))
			b.replace(Pos{r.start.Line, 0}, Pos{r.end.Line, len(b.line(r.end.Line))}, ind)
			e.cur = Pos{r.start.Line, len(ind)}
		} else {
			b.delete(r.start, r.end)
			e.cur = r.start
		}
		e.startInsert("c", 1)
	case ">", "<":
		e.changed = true
		e.mode = ModeNormal
		for i := 0; i < count; i++ {
			e.shiftLines(r.start.Line, r.end.Line, op == ">")
		}
		e.cur = Pos{r.start.Line, firstNonBlank(b.line(r.start.Line))}
		e.setWant()
	case "g~", "gu", "gU":
		e.changed = true
		e.mode = ModeNormal
		start, end := r.start, r.end
		if r.linewise {
			start = Pos{r.start.Line, 0}
			end = Pos{r.end.Line, len(b.line(r.end.Line))}
		}
		s := b.get(start, end)
		switch op {
		case "g~":
			s = toggleCase(s)
		case "gu":
			s = strings.ToLower(s)
		default:
			s = strings.ToUpper(s)
		}
		b.replace(start, end, s)
		e.cur = start
		e.clampCursor()
		e.setWant()
	}
}

func (e *Editor) rangeText(r textRange) string {
	if r.linewise {
		return e.buf.get(Pos{r.start.Line, 0}, Pos{r.end.Line, len(e.buf.line(r.end.Line))})
	}
	return e.buf.get(r.start, r.end)
}

// deleteLines removes lines a..b (inclusive) with their line breaks.
func (e *Editor) deleteLines(a, b int) {
	buf := e.buf
	n := buf.lineCount()
	switch {
	case a == 0 && b >= n-1:
		buf.replace(Pos{0, 0}, buf.endPos(), "")
	case b >= n-1:
		buf.delete(Pos{a - 1, len(buf.line(a - 1))}, Pos{n - 1, len(buf.line(n - 1))})
	default:
		buf.delete(Pos{a, 0}, Pos{b + 1, 0})
	}
}

// shiftLines indents or outdents lines a..b by one indent unit; empty
// lines are left alone, as in Vim.
func (e *Editor) shiftLines(a, b int, right bool) {
	unitW := wsWidth(e.indent)
	for l := a; l <= b; l++ {
		s := e.buf.line(l)
		if s == "" {
			continue
		}
		ws := leadingWS(s)
		w := wsWidth(ws)
		var nw int
		if right {
			nw = (w/unitW + 1) * unitW
		} else {
			if w == 0 {
				continue
			}
			nw = ((w - 1) / unitW) * unitW
		}
		e.buf.replace(Pos{l, 0}, Pos{l, len(ws)}, e.makeIndent(nw))
	}
}

// makeIndent builds leading whitespace of display width w in the file's
// indent style.
func (e *Editor) makeIndent(w int) string {
	if e.indent == "\t" {
		return strings.Repeat("\t", w/tabStop) + strings.Repeat(" ", w%tabStop)
	}
	return strings.Repeat(" ", w)
}

// put pastes a register after or before the cursor, count times.
func (e *Editor) put(r register, after bool, count int) {
	if r.text == "" {
		e.errorf("E353: Nothing in register")
		return
	}
	b := e.buf
	if r.linewise {
		text := strings.Repeat(r.text+"\n", count)
		text = text[:len(text)-1]
		var first int
		if after {
			l := e.cur.Line
			b.insert(Pos{l, len(b.line(l))}, "\n"+text)
			first = l + 1
		} else {
			b.insert(Pos{e.cur.Line, 0}, text+"\n")
			first = e.cur.Line
		}
		e.cur = Pos{first, firstNonBlank(b.line(first))}
		e.setWant()
		return
	}
	text := strings.Repeat(r.text, count)
	at := e.cur
	if after && b.line(at.Line) != "" {
		at.Col = nextCol(b.line(at.Line), at.Col)
	}
	end := b.insert(at, text)
	if strings.Contains(text, "\n") {
		e.cur = at
	} else {
		e.cur = Pos{end.Line, prevCol(b.line(end.Line), end.Col)}
	}
	e.clampCursor()
	e.setWant()
}

// pasteNormal inserts bracketed-paste text before the cursor in Normal
// mode, leaving the cursor on its last character (as Vim does).
func (e *Editor) pasteNormal(text string) {
	text = normalizeNewlines(text)
	if text == "" {
		return
	}
	e.buf.groupCursor = e.cur
	end := e.buf.insert(e.cur, text)
	e.buf.commit(end)
	e.cur = Pos{end.Line, prevCol(e.buf.line(end.Line), end.Col)}
	e.clampCursor()
	e.setWant()
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// joinLines joins count lines starting at line (J / gJ). With spaces, the
// leading blanks of each joined line are removed and one space inserted,
// except before ")" or after a line ending in a blank, or when the joined
// line is empty.
func (e *Editor) joinLines(line, count int, spaces bool) {
	b := e.buf
	if line+1 >= b.lineCount() {
		return
	}
	last := min(line+count-1, b.lineCount()-1)
	col := 0
	for i := line; i < last; i++ {
		cur := b.line(line)
		next := b.line(line + 1)
		if !spaces {
			col = len(cur)
			b.replace(Pos{line, len(cur)}, Pos{line + 1, 0}, "")
			continue
		}
		trimmed := strings.TrimLeft(next, " \t")
		sep := " "
		if trimmed == "" || strings.HasPrefix(trimmed, ")") || cur == "" ||
			strings.HasSuffix(cur, " ") || strings.HasSuffix(cur, "\t") {
			sep = ""
		}
		col = len(cur)
		if sep == "" && trimmed != "" && col > 0 && (strings.HasSuffix(cur, " ") || strings.HasSuffix(cur, "\t")) {
			col = prevCol(cur, col)
		}
		b.replace(Pos{line, len(cur)}, Pos{line + 1, len(next) - len(trimmed)}, sep)
		if sep == "" && trimmed == "" {
			col = lastCol(b.line(line))
		}
	}
	e.cur = Pos{line, col}
	e.clampCursor()
	e.setWant()
}

func toggleCase(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		switch {
		case unicode.IsUpper(r):
			sb.WriteRune(unicode.ToLower(r))
		case unicode.IsLower(r):
			sb.WriteRune(unicode.ToUpper(r))
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// replaceChars implements r{char}.
func (e *Editor) replaceChars(tok string, count int) {
	b := e.buf
	line := b.line(e.cur.Line)
	end := e.cur.Col
	n := 0
	for n < count && end < len(line) {
		end = nextCol(line, end)
		n++
	}
	if n < count || line == "" {
		return
	}
	e.changed = true
	if tok == "<CR>" {
		b.replace(e.cur, Pos{e.cur.Line, end}, "\n")
		e.cur = Pos{e.cur.Line + 1, 0}
		e.setWant()
		return
	}
	ch := tok
	if tok == "<Tab>" {
		ch = "\t"
	}
	b.replace(e.cur, Pos{e.cur.Line, end}, strings.Repeat(ch, count))
	e.cur.Col += len(ch) * (count - 1)
	e.clampCursor()
	e.setWant()
}

// openLine opens a new line below (o) or above (O) with the indentation of
// the current line; o on a list item, task or quote continues it.
func (e *Editor) openLine(below bool) {
	b := e.buf
	line := b.line(e.cur.Line)
	if below {
		prefix := leadingWS(line)
		if lp, ok := parseListPrefix(line); ok && !e.inCode(e.cur.Line) {
			prefix = lp.continuation()
		}
		end := b.insert(Pos{e.cur.Line, len(line)}, "\n"+prefix)
		e.cur = end
		if lp, ok := parseListPrefix(line); ok && lp.ordered && !e.inCode(e.cur.Line) {
			e.renumber(e.cur.Line)
		}
		return
	}
	prefix := leadingWS(line)
	b.insert(Pos{e.cur.Line, 0}, prefix+"\n")
	e.cur = Pos{e.cur.Line, len(prefix)}
}

// repeat replays the last change (dot), with a new count if non-zero.
func (e *Editor) repeat(count int) {
	ch := e.lastChange
	if ch == nil {
		return
	}
	e.replaying = true
	defer func() { e.replaying = false }()
	if ch.visual != nil {
		e.selectShape(*ch.visual)
		for _, in := range ch.inputs {
			e.feedInner(in)
		}
	} else {
		c := ch.count
		if count > 0 {
			c = count
		}
		var ins []input
		if c > 1 || count > 0 {
			for _, r := range strconv.Itoa(c) {
				ins = append(ins, input{tok: string(r)})
			}
		}
		ins = append(ins, ch.inputs...)
		for _, in := range ins {
			e.feedInner(in)
		}
		if count > 0 {
			ch2 := *ch
			ch2.count = c
			e.lastChange = &ch2
		}
	}
	if e.mode == ModeInsert {
		// The recorded change always ends in <Esc>; be safe anyway.
		e.feedInner(input{tok: "<Esc>"})
	}
}

// feedInner dispatches an input without the outer bookkeeping of feed.
func (e *Editor) feedInner(in input) {
	switch {
	case e.cmd.active:
		e.cmdInput(in)
	case e.mode == ModeInsert:
		e.insertInput(in)
	case e.mode == ModeVisual || e.mode == ModeVisualLine:
		e.visualInput(in)
	default:
		e.normalInput(in)
	}
}
