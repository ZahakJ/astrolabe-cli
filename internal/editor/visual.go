package editor

import (
	"strings"
	"unicode/utf8"
)

// visualInput handles one input in Visual or Visual-line mode.
func (e *Editor) visualInput(in input) Result {
	p := &e.pend
	if in.tok == "" {
		return Result{}
	}
	if len(p.keys) == 0 {
		e.buf.groupCursor = minPos(e.anchor, e.cur)
	}
	p.keys = append(p.keys, displayToken(in.tok))
	res, done := e.visualStep(in.tok)
	if done {
		e.pend = pending{}
		if e.mode != ModeInsert {
			e.buf.commit(e.cur)
		}
	}
	return res
}

// visualRange is the selected region.
func (e *Editor) visualRange() textRange {
	a, c := e.anchor, e.cur
	if e.mode == ModeVisualLine {
		if a.Line > c.Line {
			a, c = c, a
		}
		return textRange{start: Pos{a.Line, 0}, end: Pos{c.Line, 0}, linewise: true}
	}
	start, end := minPos(a, c), maxPos(a, c)
	l := e.buf.line(end.Line)
	if end.Col < len(l) {
		end.Col = nextCol(l, end.Col)
	} else if end.Line+1 < e.buf.lineCount() {
		end = Pos{end.Line + 1, 0} // the selection includes the line break
	}
	return textRange{start: start, end: end}
}

func (e *Editor) shapeOf() visualShape {
	a, c := minPos(e.anchor, e.cur), maxPos(e.anchor, e.cur)
	sh := visualShape{mode: e.mode, lines: c.Line - a.Line}
	if e.mode == ModeVisual {
		if sh.lines == 0 {
			l := e.buf.line(a.Line)
			sh.cols = countClusters(l, min(len(l), nextCol(l, c.Col))) - countClusters(l, a.Col)
		} else {
			sh.cols = c.Col
		}
	}
	return sh
}

// selectShape re-creates a selection of the recorded shape at the cursor
// (dot-repeat of a Visual-mode change).
func (e *Editor) selectShape(sh visualShape) {
	e.mode = sh.mode
	e.anchor = e.cur
	end := Pos{clampInt(e.cur.Line+sh.lines, 0, e.buf.lineCount()-1), e.cur.Col}
	if sh.mode == ModeVisual {
		l := e.buf.line(end.Line)
		if sh.lines == 0 {
			c := e.cur.Col
			for i := 1; i < sh.cols && nextCol(l, c) < len(l); i++ {
				c = nextCol(l, c)
			}
			end.Col = c
		} else {
			end.Col = snapCol(l, min(sh.cols, len(l)))
		}
	}
	e.cur = end
}

func (e *Editor) exitVisual() {
	e.lastVisual.anchor, e.lastVisual.cur, e.lastVisual.mode, e.lastVisual.set = e.anchor, e.cur, e.mode, true
	e.visStart = min(e.anchor.Line, e.cur.Line)
	e.visEnd = max(e.anchor.Line, e.cur.Line)
	e.mode = ModeNormal
	e.clampCursor()
}

// beginVisualChange starts recording a Visual-mode change for dot-repeat.
func (e *Editor) beginVisualChange(tokens ...string) {
	sh := e.shapeOf()
	e.recVisual = &sh
	e.rec = e.rec[:0]
	for _, t := range tokens {
		e.rec = append(e.rec, input{tok: t})
	}
}

// endVisualChange saves a Visual-mode change that did not enter Insert.
func (e *Editor) endVisualChange() {
	if !e.replaying {
		e.saveChange(1)
		e.lastChange.visual = e.recVisual
	}
	e.recVisual = nil
}

func (e *Editor) visualStep(tok string) (Result, bool) {
	p := &e.pend
	count, hasCount := p.count()
	if tok == "<Esc>" || tok == "<C-c>" {
		if p.prefix == "" && p.op == "" {
			e.exitVisual()
		}
		return Result{}, true
	}
	switch p.prefix {
	case `"`:
		p.prefix = ""
		if isChar(tok) {
			p.reg, _ = utf8.DecodeRuneInString(tok)
		}
		return Result{}, false
	case "f", "F", "t", "T":
		pre := p.prefix
		p.prefix = ""
		if !isChar(tok) {
			return Result{}, true
		}
		e.lastFind = &findState{target: tok, forward: pre == "f" || pre == "t", till: pre == "t" || pre == "T"}
		m := e.findMotion(*e.lastFind, count, false)
		if !m.fail {
			e.applyMotion(m)
		}
		return Result{}, true
	case "i", "a":
		inner := p.prefix == "i"
		p.prefix = ""
		r, ok := e.textObject(tok, inner, count)
		if !ok {
			return Result{}, true
		}
		if r.linewise {
			if e.mode == ModeVisual && e.anchor == e.cur {
				e.mode = ModeVisualLine
			}
			if e.anchor == e.cur || e.mode == ModeVisualLine {
				e.anchor = Pos{r.start.Line, 0}
			}
			e.cur = Pos{r.end.Line, 0}
		} else {
			if e.anchor == e.cur {
				e.anchor = r.start
			} else {
				e.anchor = minPos(e.anchor, r.start)
			}
			end := r.end
			if end.Col > 0 {
				end.Col = prevCol(e.buf.line(end.Line), end.Col)
			} else if end.Line > r.start.Line {
				end = Pos{end.Line - 1, len(e.buf.line(end.Line - 1))}
			}
			e.cur = end
		}
		e.clampCursor()
		e.setWant()
		return Result{}, true
	case "r":
		p.prefix = ""
		if !isChar(tok) {
			return Result{}, true
		}
		e.beginVisualChange("r", tok)
		e.visualReplace(tok)
		e.endVisualChange()
		return Result{}, true
	case "g":
		p.prefix = ""
		switch tok {
		case "g", "e", "E", "_", "j", "k":
			key := "g" + tok
			if tok == "j" || tok == "k" {
				key = tok
			}
			m, _ := e.doMotion(key, "", count, hasCount, "")
			if !m.fail {
				e.applyMotion(m)
			}
		case "J":
			e.beginVisualChange("g", "J")
			e.visualJoin(false)
			e.endVisualChange()
		case "~", "u", "U":
			e.beginVisualChange("g", tok)
			e.applyOp("g"+tok, e.visualRange(), 0, 1)
			e.endVisualChange()
		case "v":
			if e.lastVisual.set {
				a, c, m := e.anchor, e.cur, e.mode
				e.anchor, e.cur, e.mode = e.buf.clampPos(e.lastVisual.anchor), e.buf.clampPos(e.lastVisual.cur), e.lastVisual.mode
				e.lastVisual.anchor, e.lastVisual.cur, e.lastVisual.mode = a, c, m
			}
		}
		return Result{}, true
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
	}

	if len(tok) == 1 && tok[0] >= '0' && tok[0] <= '9' && (tok != "0" || p.count1 > 0) {
		p.count1 = clampCount(p.count1*10 + int(tok[0]-'0'))
		return Result{}, false
	}

	switch tok {
	case `"`, "f", "F", "t", "T", "i", "a", "r", "g", "z":
		p.prefix = tok
		return Result{}, false
	case "v", "V":
		want := ModeVisual
		if tok == "V" {
			want = ModeVisualLine
		}
		if e.mode == want {
			e.exitVisual()
		} else {
			e.mode = want
		}
		return Result{}, true
	case "o", "O":
		e.anchor, e.cur = e.cur, e.anchor
		e.setWant()
		return Result{}, true
	case "d", "x", "<Del>", "X", "D":
		if tok == "X" || tok == "D" {
			e.mode = ModeVisualLine
		}
		e.beginVisualChange(tok)
		r := e.visualRange()
		e.exitVisual()
		e.applyOp("d", r, p.reg, 1)
		e.endVisualChange()
		return Result{}, true
	case "c", "s", "C", "S", "R":
		if tok == "C" || tok == "S" || tok == "R" {
			e.mode = ModeVisualLine
		}
		e.beginVisualChange(tok)
		r := e.visualRange()
		e.exitVisual()
		e.applyOp("c", r, p.reg, 1)
		e.recording = true
		e.recCount = 1
		return Result{}, true
	case "y", "Y":
		if tok == "Y" {
			e.mode = ModeVisualLine
		}
		r := e.visualRange()
		e.exitVisual()
		e.mode = ModeVisual // applyOp("y") moves to the start for selections
		e.applyOp("y", r, p.reg, 1)
		return Result{}, true
	case ">", "<":
		e.beginVisualChange(tok)
		r := e.visualRange()
		e.exitVisual()
		e.applyOp(tok, r, 0, count)
		e.endVisualChange()
		return Result{}, true
	case "~", "u", "U":
		op := map[string]string{"~": "g~", "u": "gu", "U": "gU"}[tok]
		e.beginVisualChange(tok)
		r := e.visualRange()
		e.exitVisual()
		e.applyOp(op, r, 0, 1)
		e.endVisualChange()
		return Result{}, true
	case "J":
		e.beginVisualChange(tok)
		e.visualJoin(true)
		e.endVisualChange()
		return Result{}, true
	case "p", "P":
		e.beginVisualChange(tok)
		e.visualPut(p.reg)
		e.endVisualChange()
		return Result{}, true
	case ":":
		e.exitVisual()
		e.openCmdline(':')
		e.cmd.text = "'<,'>"
		e.cmd.pos = len(e.cmd.text)
		return Result{}, true
	case "<C-s>":
		return Result{Action: ActionSave}, true
	case "<C-d>", "<C-u>":
		e.scrollHalf(tok == "<C-d>", count, hasCount)
		return Result{}, true
	case "<C-f>", "<PageDown>":
		e.scrollPage(true, count)
		return Result{}, true
	case "<C-b>", "<PageUp>":
		e.scrollPage(false, count)
		return Result{}, true
	}
	if m, ok := e.doMotion(tok, "", count, hasCount, ""); ok {
		if !m.fail {
			e.applyMotion(m)
		}
		return Result{}, true
	}
	return Result{}, true
}

// visualReplace replaces every selected character with ch (Visual r).
func (e *Editor) visualReplace(ch string) {
	r := e.visualRange()
	e.exitVisual()
	b := e.buf
	for l := r.start.Line; l <= r.end.Line && l < b.lineCount(); l++ {
		s := b.line(l)
		from, to := 0, len(s)
		if !r.linewise {
			if l == r.start.Line {
				from = r.start.Col
			}
			if l == r.end.Line {
				to = min(r.end.Col, len(s))
			}
		}
		if from >= to {
			continue
		}
		n := countClusters(s, to) - countClusters(s, from)
		b.replace(Pos{l, from}, Pos{l, to}, strings.Repeat(ch, n))
	}
	e.cur = r.start
	e.clampCursor()
	e.setWant()
}

func (e *Editor) visualJoin(spaces bool) {
	a, c := min(e.anchor.Line, e.cur.Line), max(e.anchor.Line, e.cur.Line)
	e.exitVisual()
	e.joinLines(a, max(2, c-a+1), spaces)
}

// visualPut replaces the selection with a register; the replaced text goes
// to the unnamed register, as in Vim.
func (e *Editor) visualPut(reg rune) {
	src := e.readReg(reg)
	if src.text == "" {
		e.exitVisual()
		e.errorf("E353: Nothing in register")
		return
	}
	r := e.visualRange()
	e.exitVisual()
	b := e.buf
	deleted := e.rangeText(r)
	if r.linewise {
		last := r.end.Line == b.lineCount()-1
		e.deleteLines(r.start.Line, r.end.Line)
		text := src.text
		if b.lineCount() == 1 && b.line(0) == "" && r.start.Line == 0 {
			b.replace(Pos{0, 0}, Pos{0, 0}, text)
		} else if last && r.start.Line > 0 {
			b.insert(Pos{r.start.Line - 1, len(b.line(r.start.Line - 1))}, "\n"+text)
		} else {
			b.insert(Pos{r.start.Line, 0}, text+"\n")
		}
		e.cur = Pos{r.start.Line, firstNonBlank(b.line(r.start.Line))}
	} else {
		b.delete(r.start, r.end)
		if src.linewise {
			b.insert(r.start, "\n"+src.text+"\n")
			e.cur = Pos{r.start.Line + 1, firstNonBlank(b.line(r.start.Line + 1))}
		} else {
			end := b.insert(r.start, src.text)
			e.cur = Pos{end.Line, prevCol(b.line(end.Line), end.Col)}
		}
	}
	e.setReg(0, deleted, r.linewise, false)
	e.clampCursor()
	e.setWant()
}
