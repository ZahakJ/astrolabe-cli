package editor

import (
	"strings"
	"unicode/utf8"
)

// walker is a cursor over the buffer that moves by grapheme cluster with
// the semantics of Vim's inc()/dec(), including the "NUL" position after
// the last character of a line. The word motions below are ports of Vim's
// fwd_word, bck_word, end_word and bckend_word so that counts, operators
// and line ends behave exactly as muscle memory expects.
type walker struct {
	b   *buffer
	p   Pos
	big bool
}

// inc moves forward one cluster. It returns 0 when staying in the line on a
// character, 2 when it moved onto the end-of-line position, 1 when it went
// to the start of the next line and -1 at the end of the buffer.
func (w *walker) inc() int {
	l := w.b.lines[w.p.Line]
	if w.p.Col < len(l) {
		w.p.Col = nextCol(l, w.p.Col)
		if w.p.Col < len(l) {
			return 0
		}
		return 2
	}
	if w.p.Line+1 < len(w.b.lines) {
		w.p = Pos{w.p.Line + 1, 0}
		return 1
	}
	return -1
}

// dec moves back one cluster: 0 within the line, 1 when it moved to the
// end-of-line position of the previous line, -1 at the start of the buffer.
func (w *walker) dec() int {
	if w.p.Col > 0 {
		w.p.Col = prevCol(w.b.lines[w.p.Line], w.p.Col)
		return 0
	}
	if w.p.Line > 0 {
		w.p.Line--
		w.p.Col = len(w.b.lines[w.p.Line])
		return 1
	}
	return -1
}

func (w *walker) cls() int {
	return clusterClass(clusterAt(w.b.lines[w.p.Line], w.p.Col), w.big)
}

func (w *walker) lineEmpty() bool { return w.b.lines[w.p.Line] == "" }

func (w *walker) skipChars(class int, forward bool) bool {
	for w.cls() == class {
		var r int
		if forward {
			r = w.inc()
		} else {
			r = w.dec()
		}
		if r == -1 {
			return true
		}
	}
	return false
}

// fwdWord is Vim's fwd_word: eol (set for operators) stops at the end of
// the line on the last word instead of moving to the next line.
func (w *walker) fwdWord(count int, eol bool) bool {
	for count > 0 {
		count--
		sclass := w.cls()
		lastLine := w.p.Line == len(w.b.lines)-1
		i := w.inc()
		if i == -1 || (i >= 1 && lastLine) {
			return false
		}
		if i >= 1 && eol && count == 0 {
			return true
		}
		if sclass != 0 {
			for w.cls() == sclass {
				i = w.inc()
				if i == -1 || (i >= 1 && eol && count == 0) {
					return true
				}
			}
		}
		for w.cls() == 0 {
			if w.p.Col == 0 && w.lineEmpty() {
				break
			}
			i = w.inc()
			if i == -1 || (i >= 1 && lastLine) {
				return false
			}
		}
	}
	return true
}

// bckWord is Vim's bck_word.
func (w *walker) bckWord(count int, stop bool) bool {
	for count > 0 {
		count--
		sclass := w.cls()
		if w.dec() == -1 {
			return false
		}
		if !stop || sclass == w.cls() || sclass == 0 {
			finished := false
			for w.cls() == 0 {
				if w.p.Col == 0 && w.lineEmpty() {
					finished = true
					break
				}
				if w.dec() == -1 {
					return true
				}
			}
			if finished {
				stop = false
				continue
			}
			if w.skipChars(w.cls(), false) {
				return true
			}
		}
		w.inc()
		stop = false
	}
	return true
}

// endWord is Vim's end_word: stop keeps the cursor at the end of the
// current word (used by cw); empty stops at empty lines.
func (w *walker) endWord(count int, stop, empty bool) bool {
	for count > 0 {
		count--
		sclass := w.cls()
		if w.inc() == -1 {
			return false
		}
		if w.cls() == sclass && sclass != 0 {
			if w.skipChars(sclass, true) {
				return false
			}
		} else if !stop || sclass == 0 {
			finished := false
			for w.cls() == 0 {
				if w.p.Col == 0 && w.lineEmpty() && empty {
					finished = true
					break
				}
				if w.inc() == -1 {
					return false
				}
			}
			if finished {
				stop = false
				continue
			}
			if w.skipChars(w.cls(), true) {
				return false
			}
		}
		w.dec()
		stop = false
	}
	return true
}

// bckendWord is Vim's bckend_word (ge).
func (w *walker) bckendWord(count int, eol bool) bool {
	for count > 0 {
		count--
		sclass := w.cls()
		i := w.dec()
		if i == -1 {
			return false
		}
		if eol && i == 1 {
			return true
		}
		if sclass != 0 {
			for w.cls() == sclass {
				i = w.dec()
				if i == -1 || (eol && i == 1) {
					return true
				}
			}
		}
		for w.cls() == 0 {
			if w.p.Col == 0 && w.lineEmpty() {
				break
			}
			i = w.dec()
			if i == -1 || (eol && i == 1) {
				return true
			}
		}
	}
	return true
}

// findChar finds the count-th occurrence of target on line s after (or
// before) col, for f/F/t/T. till stops one cluster short. When repeating a
// till search (; ,), a match adjacent to the cursor is skipped, as in Vim
// without cpo-;.
func findChar(s string, col int, target string, forward, till, repeat bool, count int) (int, bool) {
	match := func(g string) bool {
		if g == target {
			return true
		}
		// Match a base letter regardless of combining marks on it.
		r1, _ := utf8.DecodeRuneInString(g)
		r2, n := utf8.DecodeRuneInString(target)
		return n == len(target) && r1 == r2
	}
	pos := col
	if repeat && till && count == 1 {
		// Skip the adjacent match that the previous t/T stopped before.
		if forward {
			n := nextCol(s, pos)
			if n < len(s) && match(clusterAt(s, n)) {
				pos = n
			}
		} else if pos > 0 {
			p := prevCol(s, pos)
			if match(clusterAt(s, p)) {
				pos = p
			}
		}
	}
	for count > 0 {
		if forward {
			n := nextCol(s, pos)
			for n < len(s) && !match(clusterAt(s, n)) {
				n = nextCol(s, n)
			}
			if n >= len(s) {
				return col, false
			}
			pos = n
		} else {
			if pos == 0 {
				return col, false
			}
			p := prevCol(s, pos)
			for p > 0 && !match(clusterAt(s, p)) {
				p = prevCol(s, p)
			}
			if !match(clusterAt(s, p)) {
				return col, false
			}
			pos = p
		}
		count--
	}
	if till {
		if forward {
			pos = prevCol(s, pos)
		} else {
			pos = nextCol(s, pos)
		}
	}
	return pos, true
}

// matchPair implements %: from the first bracket at or after the cursor on
// its line, find the matching bracket (nesting counted across lines).
func matchPair(b *buffer, p Pos) (Pos, bool) {
	s := b.lines[p.Line]
	i := p.Col
	for i < len(s) && !strings.ContainsRune("(){}[]", rune(s[i])) {
		i++
	}
	if i >= len(s) {
		return p, false
	}
	c := s[i]
	var open, close byte
	forward := true
	switch c {
	case '(', ')':
		open, close = '(', ')'
	case '[', ']':
		open, close = '[', ']'
	default:
		open, close = '{', '}'
	}
	if c == close {
		forward = false
	}
	return findPair(b, Pos{p.Line, i}, open, close, forward)
}

// findPair scans from the bracket at p (exclusive) for its partner.
func findPair(b *buffer, p Pos, open, close byte, forward bool) (Pos, bool) {
	depth := 0
	line, col := p.Line, p.Col
	for {
		s := b.lines[line]
		if forward {
			col++
			for col >= len(s) {
				line++
				if line >= len(b.lines) {
					return p, false
				}
				s = b.lines[line]
				col = 0
				if len(s) > 0 {
					break
				}
			}
		} else {
			col--
			for col < 0 {
				line--
				if line < 0 {
					return p, false
				}
				s = b.lines[line]
				col = len(s) - 1
			}
		}
		ch := s[col]
		if ch == '\\' {
			continue
		}
		if col > 0 && s[col-1] == '\\' {
			continue
		}
		if forward {
			if ch == open {
				depth++
			} else if ch == close {
				if depth == 0 {
					return Pos{line, col}, true
				}
				depth--
			}
		} else {
			if ch == close {
				depth++
			} else if ch == open {
				if depth == 0 {
					return Pos{line, col}, true
				}
				depth--
			}
		}
	}
}

// paragraphFwd implements }: the count-th empty line after p (Vim treats
// only truly empty lines as boundaries). It reports atEnd when the buffer
// ended first; the cursor then sits on the last character.
func paragraphFwd(b *buffer, line, count int) (Pos, bool) {
	n := len(b.lines)
	for count > 0 {
		// skip empty lines, then non-empty, stop at the next empty line
		for line < n-1 && b.lines[line] == "" {
			line++
		}
		for line < n-1 && b.lines[line] != "" {
			line++
		}
		count--
		if line >= n-1 {
			if b.lines[n-1] == "" && line == n-1 && count == 0 {
				return Pos{n - 1, 0}, false
			}
			return Pos{n - 1, lastCol(b.lines[n-1])}, true
		}
	}
	return Pos{line, 0}, false
}

// paragraphBack implements {.
func paragraphBack(b *buffer, line, count int) Pos {
	for count > 0 {
		for line > 0 && b.lines[line] == "" {
			line--
		}
		for line > 0 && b.lines[line] != "" {
			line--
		}
		count--
		if line == 0 {
			return Pos{0, 0}
		}
	}
	return Pos{line, 0}
}
