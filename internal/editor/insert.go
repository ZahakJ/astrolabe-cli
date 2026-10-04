package editor

import (
	"strconv"
	"strings"
)

// startInsert enters Insert mode. count repeats the inserted text on Esc
// (3ix<Esc>); kind is the command that started it (o and O reopen a line
// for each repetition).
func (e *Editor) startInsert(kind string, count int) {
	e.mode = ModeInsert
	e.ins = insertSession{count: count, kind: kind}
	e.recording = true
	e.completer = completerState{}
}

// recordInsert appends an Insert-mode input to the dot-repeat record and to
// the session record used for count repetition.
func (e *Editor) recordInsert(in input) {
	if e.insRepeat {
		return
	}
	if e.recording {
		e.rec = append(e.rec, in)
	}
	e.ins.rec = append(e.ins.rec, in)
}

// insertInput handles one input in Insert mode.
func (e *Editor) insertInput(in input) Result {
	if in.complete {
		e.recordInsert(in)
		e.applyCompletion(in.back, in.text)
		return Result{}
	}
	if in.tok == "" {
		text := normalizeNewlines(in.paste)
		if text != "" {
			e.recordInsert(in)
			e.insertText(text)
			e.updateCompleter()
		}
		return Result{}
	}
	tok := in.tok
	if e.completer.open && e.completerKey(tok) {
		return Result{}
	}
	switch tok {
	case "<Esc>", "<C-c>":
		e.leaveInsert()
		return Result{}
	case "<C-s>":
		return Result{Action: ActionSave}
	case "<Up>", "<Down>", "<Left>", "<Right>", "<Home>", "<End>", "<PageUp>", "<PageDown>":
		e.breakInsert()
		e.insertMove(tok)
		return Result{}
	}
	e.recordInsert(in)
	switch tok {
	case "<CR>", "<C-j>", "<C-m>":
		e.insertNewline()
	case "<BS>":
		e.backspace()
	case "<Del>":
		e.deleteForward()
	case "<C-w>":
		e.deleteWordBack()
	case "<C-u>":
		e.deleteLineBack()
	case "<Tab>":
		e.insertTab(true)
	case "<S-Tab>":
		e.insertTab(false)
	case "<C-t>":
		// Ctrl-t inserts a timestamp (DESIGN.md), replacing Vim's indent.
		e.insertText(e.now().Format("2006-01-02 15:04"))
	default:
		if isChar(tok) {
			e.insertText(tok)
			if tok == "[" {
				e.maybeOpenCompleter()
			}
		}
	}
	e.updateCompleter()
	return Result{}
}

func (e *Editor) insertText(s string) {
	e.cur = e.buf.insert(e.cur, s)
}

// leaveInsert handles Esc: repeat the insertion for a count, step the
// cursor back one character and close the undo group.
func (e *Editor) leaveInsert() {
	if n := e.ins.count; n > 1 && !e.insRepeat {
		rec := e.ins.rec
		e.insRepeat = true
		for i := 1; i < n; i++ {
			if e.ins.kind == "o" || e.ins.kind == "O" {
				e.openLine(e.ins.kind == "o")
			}
			for _, in := range rec {
				e.insertInput(in)
			}
		}
		e.insRepeat = false
	}
	e.completer = completerState{}
	e.mode = ModeNormal
	if e.cur.Col > 0 {
		e.cur.Col = prevCol(e.buf.line(e.cur.Line), e.cur.Col)
	}
	e.clampCursor()
	e.setWant()
	e.buf.commit(e.cur)
	if e.recording {
		e.rec = append(e.rec, input{tok: "<Esc>"})
		if !e.replaying {
			e.saveChange(e.recCount)
			if e.recVisual != nil {
				e.lastChange.visual = e.recVisual
			}
		}
	}
	e.recVisual = nil
	e.recording = false
}

// breakInsert is called when the cursor moves in Insert mode: like Vim, it
// closes the undo group and restarts the dot record, as if a fresh "i"
// began here.
func (e *Editor) breakInsert() {
	e.buf.commit(e.cur)
	e.buf.groupCursor = e.cur
	e.ins = insertSession{count: 1, kind: "i"}
	e.rec = append(e.rec[:0], input{tok: "i"})
	e.recCount = 1
	e.recVisual = nil
	e.recording = true
	e.completer = completerState{}
}

func (e *Editor) insertMove(tok string) {
	line := e.buf.line(e.cur.Line)
	switch tok {
	case "<Left>":
		if e.cur.Col > 0 {
			e.cur.Col = prevCol(line, e.cur.Col)
		}
		e.setWant()
	case "<Right>":
		if e.cur.Col < len(line) {
			e.cur.Col = nextCol(line, e.cur.Col)
		}
		e.setWant()
	case "<Home>":
		e.cur.Col = 0
		e.setWant()
	case "<End>":
		e.cur.Col = len(line)
		e.setWant()
	case "<Up>", "<Down>":
		e.displayMove(tok == "<Down>")
	case "<PageUp>":
		e.scrollPage(false, 1)
	case "<PageDown>":
		e.scrollPage(true, 1)
	}
	e.buf.groupCursor = e.cur
}

func (e *Editor) backspace() {
	if e.cur.Col > 0 {
		line := e.buf.line(e.cur.Line)
		p := prevCol(line, e.cur.Col)
		e.buf.delete(Pos{e.cur.Line, p}, e.cur)
		e.cur.Col = p
		return
	}
	if e.cur.Line > 0 {
		prev := len(e.buf.line(e.cur.Line - 1))
		e.buf.delete(Pos{e.cur.Line - 1, prev}, e.cur)
		e.cur = Pos{e.cur.Line - 1, prev}
	}
}

func (e *Editor) deleteForward() {
	line := e.buf.line(e.cur.Line)
	if e.cur.Col < len(line) {
		e.buf.delete(e.cur, Pos{e.cur.Line, nextCol(line, e.cur.Col)})
		return
	}
	if e.cur.Line+1 < e.buf.lineCount() {
		e.buf.delete(e.cur, Pos{e.cur.Line + 1, 0})
	}
}

// deleteWordBack is Ctrl-w: delete the blanks and then the word before the
// cursor; at the start of a line, join with the previous one.
func (e *Editor) deleteWordBack() {
	if e.cur.Col == 0 {
		e.backspace()
		return
	}
	line := e.buf.line(e.cur.Line)
	c := e.cur.Col
	cls := func(col int) int { return clusterClass(clusterAt(line, col), false) }
	for c > 0 && cls(prevCol(line, c)) == 0 {
		c = prevCol(line, c)
	}
	if c > 0 {
		k := cls(prevCol(line, c))
		for c > 0 && cls(prevCol(line, c)) == k {
			c = prevCol(line, c)
		}
	}
	e.buf.delete(Pos{e.cur.Line, c}, e.cur)
	e.cur.Col = c
}

// deleteLineBack is Ctrl-u: delete back to the indentation (or to the line
// start when already there).
func (e *Editor) deleteLineBack() {
	if e.cur.Col == 0 {
		e.backspace()
		return
	}
	line := e.buf.line(e.cur.Line)
	start := firstNonBlankInsert(line)
	if start >= e.cur.Col {
		start = 0
	}
	e.buf.delete(Pos{e.cur.Line, start}, e.cur)
	e.cur.Col = start
}

// listPrefix describes the Markdown container prefix of a line: block quote
// markers, then optionally a list marker and a task box.
type listPrefix struct {
	quote   string // "> " markers, verbatim
	indent  string // indentation after the quote markers
	marker  string // "-", "*", "+", "3." or "3)"; "" for a plain quote
	ordered bool
	number  int
	delim   byte
	space   string // blanks after the marker
	task    bool
	end     int // byte offset where the item's content starts
}

func (lp listPrefix) isList() bool { return lp.marker != "" }

// parseListPrefix parses the container prefix of s. ok is false for lines
// that are neither list items nor quotes.
func parseListPrefix(s string) (listPrefix, bool) {
	var lp listPrefix
	i := 0
	for {
		j := i
		for j < len(s) && j-i < 3 && s[j] == ' ' {
			j++
		}
		if j < len(s) && s[j] == '>' {
			j++
			if j < len(s) && s[j] == ' ' {
				j++
			}
			i = j
			continue
		}
		break
	}
	lp.quote = s[:i]
	j := i
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	lp.indent = s[i:j]
	k := j
	switch {
	case k < len(s) && (s[k] == '-' || s[k] == '*' || s[k] == '+') && (k+1 == len(s) || s[k+1] == ' ' || s[k+1] == '\t'):
		if isThematicBreak(s[j:]) {
			break
		}
		lp.marker = s[k : k+1]
		k++
	default:
		d := k
		for d < len(s) && d-k < 9 && s[d] >= '0' && s[d] <= '9' {
			d++
		}
		if d > k && d < len(s) && (s[d] == '.' || s[d] == ')') && (d+1 == len(s) || s[d+1] == ' ' || s[d+1] == '\t') {
			lp.number, _ = strconv.Atoi(s[k:d])
			lp.ordered = true
			lp.delim = s[d]
			lp.marker = s[k : d+1]
			k = d + 1
		}
	}
	if lp.marker == "" {
		if lp.quote == "" {
			return lp, false
		}
		lp.indent = ""
		lp.end = i
		return lp, true
	}
	sp := k
	for sp < len(s) && (s[sp] == ' ' || s[sp] == '\t') {
		sp++
	}
	lp.space = s[k:sp]
	lp.end = sp
	// task box: "[x]" followed by a blank or the end of the line
	if sp < len(s) && s[sp] == '[' && sp > k {
		r := sp + 1
		if r < len(s) {
			r = nextCol(s, r)
			if r < len(s) && s[r] == ']' && (r+1 == len(s) || s[r+1] == ' ' || s[r+1] == '\t') {
				lp.task = true
				q := r + 1
				for q < len(s) && (s[q] == ' ' || s[q] == '\t') {
					q++
				}
				lp.end = q
			}
		}
	}
	return lp, true
}

func isThematicBreak(s string) bool {
	t := strings.TrimSpace(s)
	if len(t) < 3 {
		return false
	}
	c := t[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	n := 0
	for i := 0; i < len(t); i++ {
		switch t[i] {
		case c:
			n++
		case ' ', '\t':
		default:
			return false
		}
	}
	return n >= 3
}

// continuation returns the prefix that starts the next item.
func (lp listPrefix) continuation() string {
	if !lp.isList() {
		return lp.quote
	}
	m := lp.marker
	if lp.ordered {
		m = strconv.Itoa(lp.number+1) + string(lp.delim)
	}
	sp := lp.space
	if sp == "" {
		sp = " "
	}
	out := lp.quote + lp.indent + m + sp
	if lp.task {
		out += "[ ] "
	}
	return out
}

// insertNewline is Enter in Insert mode: continue a list item, task or
// quote; end it when the item is empty; otherwise keep the indentation.
func (e *Editor) insertNewline() {
	b := e.buf
	ln := e.cur.Line
	line := b.line(ln)
	col := e.cur.Col
	if !e.inCode(ln) {
		if lp, ok := parseListPrefix(line); ok && col >= lp.end {
			if strings.TrimSpace(line[lp.end:]) == "" {
				// An empty item ends the list (or steps out one level).
				if lp.isList() && lp.indent != "" {
					e.outdentItem(ln, lp)
					return
				}
				nl := ""
				if lp.isList() {
					nl = lp.quote
				}
				b.setLine(ln, nl)
				e.cur = Pos{ln, len(nl)}
				return
			}
			rest := line[col:]
			trimmed := strings.TrimLeft(rest, " \t")
			cont := lp.continuation()
			// Splitting an item drops the blanks around the break.
			from := lp.end + len(strings.TrimRight(line[lp.end:col], " \t"))
			b.replace(Pos{ln, from}, Pos{ln, col + len(rest) - len(trimmed)}, "\n"+cont)
			e.cur = Pos{ln + 1, len(cont)}
			if lp.ordered {
				e.renumber(ln + 1)
			}
			return
		}
	}
	// At the end of a wrapped list item's continuation line ("  more of
	// the item"), Enter starts the next item, as it does on the item's
	// first line.
	if col == len(line) && !isBlank(line) && !e.inCode(ln) {
		if lp, ok := e.continuedItem(ln); ok {
			cont := lp.continuation()
			e.cur = b.insert(e.cur, "\n"+cont)
			if lp.ordered {
				e.renumber(ln + 1)
			}
			return
		}
	}
	ind := leadingWS(line)
	if len(ind) > col {
		ind = ind[:col]
	}
	if isBlank(line) {
		// Like Vim's autoindent, an indent left unused is removed.
		b.replace(Pos{ln, 0}, Pos{ln, len(line)}, "\n"+ind)
		e.cur = Pos{ln + 1, len(ind)}
		return
	}
	e.cur = b.insert(e.cur, "\n"+ind)
}

// continuedItem finds the list item that line ln continues: the nearest
// list item above it with no blank line between, where ln and every line
// between are indented past the item's marker. Items inside block quotes
// are left to the quote handling.
func (e *Editor) continuedItem(ln int) (listPrefix, bool) {
	ind := len(leadingWS(e.buf.line(ln)))
	if ind == 0 {
		return listPrefix{}, false
	}
	for l := ln - 1; l >= 0 && ln-l <= 100; l-- {
		s := e.buf.line(l)
		if isBlank(s) {
			return listPrefix{}, false
		}
		lp, ok := parseListPrefix(s)
		if ok && lp.quote != "" {
			return listPrefix{}, false
		}
		if ok && lp.isList() {
			return lp, ind > len(lp.indent)
		}
		if len(leadingWS(s)) == 0 {
			return listPrefix{}, false
		}
	}
	return listPrefix{}, false
}

// insertTab is Tab / Shift-Tab: table cell navigation, list indentation, or
// plain indentation.
func (e *Editor) insertTab(forward bool) {
	ln := e.cur.Line
	line := e.buf.line(ln)
	if !e.inCode(ln) {
		if t, ok := e.tableAt(ln); ok {
			e.tableNav(t, forward)
			return
		}
		if lp, ok := parseListPrefix(line); ok && lp.isList() {
			if forward {
				e.indentItem(ln, lp)
			} else if lp.indent != "" {
				e.outdentItem(ln, lp)
			}
			return
		}
	}
	if forward {
		if e.indent == "\t" {
			e.insertText("\t")
			return
		}
		v := vcolOf(line, e.cur.Col)
		e.insertText(strings.Repeat(" ", tabStop-v%tabStop))
		return
	}
	ws := leadingWS(line)
	if ws == "" {
		return
	}
	w := wsWidth(ws)
	unit := wsWidth(e.indent)
	nw := ((w - 1) / unit) * unit
	ni := e.makeIndent(nw)
	e.buf.replace(Pos{ln, 0}, Pos{ln, len(ws)}, ni)
	e.cur.Col = max(0, e.cur.Col+len(ni)-len(ws))
}

// listItemAt parses line l as a list item with the same quote prefix.
func (e *Editor) listItemAt(l int, quote string) (listPrefix, bool) {
	lp, ok := parseListPrefix(e.buf.line(l))
	if !ok || !lp.isList() || lp.quote != quote {
		return lp, false
	}
	return lp, true
}

// indentItem nests a list item under the item above it: its indentation
// becomes the content column of the previous sibling.
func (e *Editor) indentItem(ln int, lp listPrefix) {
	curW := wsWidth(lp.indent)
	newIndent := ""
	number := 1
	found := false
	for l := ln - 1; l >= 0 && l >= ln-200; l-- {
		s := e.buf.line(l)
		if isBlank(s) {
			continue
		}
		p, ok := e.listItemAt(l, lp.quote)
		if !ok {
			if strings.HasPrefix(s, lp.quote) && wsWidth(leadingWS(s[len(lp.quote):])) > curW {
				continue // continuation line of a deeper item
			}
			break
		}
		w := wsWidth(p.indent)
		if w > curW {
			continue
		}
		if w == curW {
			if e.indent == "\t" {
				newIndent = lp.indent + "\t"
			} else {
				newIndent = lp.indent + strings.Repeat(" ", len(p.marker)+max(1, wsWidth(p.space)))
			}
			found = true
		}
		break
	}
	if !found {
		newIndent = lp.indent + e.indent
	}
	// numbering at the new level: continue a sibling above, else start at 1
	if lp.ordered {
		newW := wsWidth(newIndent)
		for l := ln - 1; l >= 0 && l >= ln-200; l-- {
			p, ok := e.listItemAt(l, lp.quote)
			if !ok {
				if isBlank(e.buf.line(l)) {
					continue
				}
				break
			}
			w := wsWidth(p.indent)
			if w < newW {
				break
			}
			if w == newW && p.ordered {
				number = p.number + 1
				break
			}
		}
	}
	e.rewriteItem(ln, lp, newIndent, number)
}

// outdentItem moves a list item out one level, to the indentation of its
// parent item.
func (e *Editor) outdentItem(ln int, lp listPrefix) {
	curW := wsWidth(lp.indent)
	newIndent := ""
	parentFound := false
	number := 1
	for l := ln - 1; l >= 0 && l >= ln-200; l-- {
		s := e.buf.line(l)
		if isBlank(s) {
			continue
		}
		p, ok := e.listItemAt(l, lp.quote)
		if !ok {
			if strings.HasPrefix(s, lp.quote) && wsWidth(leadingWS(s[len(lp.quote):])) > 0 {
				continue
			}
			break
		}
		if wsWidth(p.indent) < curW {
			newIndent = p.indent
			parentFound = true
			if p.ordered {
				number = p.number + 1
			}
			break
		}
	}
	if !parentFound {
		w := wsWidth(lp.indent)
		unit := wsWidth(e.indent)
		newIndent = e.makeIndent(((w - 1) / unit) * unit)
	}
	e.rewriteItem(ln, lp, newIndent, number)
}

// rewriteItem replaces the indentation (and, for ordered items, the
// number) of a list item, keeping the cursor on the same text.
func (e *Editor) rewriteItem(ln int, lp listPrefix, indent string, number int) {
	start := len(lp.quote)
	oldEnd := start + len(lp.indent) + len(lp.marker)
	marker := lp.marker
	if lp.ordered {
		marker = strconv.Itoa(number) + string(lp.delim)
	}
	repl := indent + marker
	e.buf.replace(Pos{ln, start}, Pos{ln, oldEnd}, repl)
	delta := len(repl) - (oldEnd - start)
	if e.cur.Line == ln {
		if e.cur.Col >= oldEnd {
			e.cur.Col += delta
		} else {
			e.cur.Col = min(e.cur.Col, len(e.buf.line(ln)))
			if e.cur.Col > start {
				e.cur.Col = start + len(repl)
			}
		}
	}
	if lp.ordered {
		e.renumber(ln)
		// the level the item left may now have a gap
		for l := ln + 1; l < e.buf.lineCount() && l <= ln+200; l++ {
			p, ok := e.listItemAt(l, lp.quote)
			if !ok {
				break
			}
			if wsWidth(p.indent) == wsWidth(lp.indent) && p.ordered {
				e.renumberFromAbove(l)
				break
			}
		}
	}
}

// renumber makes the ordered siblings after line l count on from it.
func (e *Editor) renumber(l int) {
	lp, ok := parseListPrefix(e.buf.line(l))
	if !ok || !lp.ordered {
		return
	}
	n := lp.number
	w := wsWidth(lp.indent)
	for k := l + 1; k < e.buf.lineCount() && k <= l+500; k++ {
		s := e.buf.line(k)
		if isBlank(s) {
			return
		}
		p, ok := e.listItemAt(k, lp.quote)
		if !ok {
			if strings.HasPrefix(s, lp.quote) && wsWidth(leadingWS(s[len(lp.quote):])) > w {
				continue
			}
			return
		}
		pw := wsWidth(p.indent)
		if pw > w {
			continue
		}
		if pw < w || !p.ordered || p.delim != lp.delim {
			return
		}
		n++
		if p.number == n {
			return // already in sequence
		}
		at := len(p.quote) + len(p.indent)
		e.buf.replace(Pos{k, at}, Pos{k, at + len(p.marker) - 1}, strconv.Itoa(n))
		if e.cur.Line == k && e.cur.Col > at {
			e.cur.Col += len(strconv.Itoa(n)) - (len(p.marker) - 1)
		}
	}
}

// renumberFromAbove renumbers the ordered item at l and its followers so
// they continue the sibling above (or start at 1).
func (e *Editor) renumberFromAbove(l int) {
	lp, ok := parseListPrefix(e.buf.line(l))
	if !ok || !lp.ordered {
		return
	}
	w := wsWidth(lp.indent)
	n := 1
	for k := l - 1; k >= 0 && k >= l-500; k-- {
		p, ok := e.listItemAt(k, lp.quote)
		if !ok {
			if isBlank(e.buf.line(k)) {
				break
			}
			continue
		}
		pw := wsWidth(p.indent)
		if pw < w {
			break
		}
		if pw == w && p.ordered {
			n = p.number + 1
			break
		}
	}
	if lp.number != n {
		at := len(lp.quote) + len(lp.indent)
		e.buf.replace(Pos{l, at}, Pos{l, at + len(lp.marker) - 1}, strconv.Itoa(n))
	}
	e.renumber(l)
}
