package editor

import (
	"strings"
)

// Pos is a position in the buffer: a 0-based line index and a byte offset
// into that line. Editor code keeps Col on a grapheme-cluster boundary.
type Pos struct {
	Line, Col int
}

// Less reports whether p comes before q.
func (p Pos) Less(q Pos) bool {
	return p.Line < q.Line || (p.Line == q.Line && p.Col < q.Col)
}

// minPos and maxPos order two positions.
func minPos(a, b Pos) Pos {
	if b.Less(a) {
		return b
	}
	return a
}

func maxPos(a, b Pos) Pos {
	if a.Less(b) {
		return b
	}
	return a
}

// buffer holds the text as a slice of lines (without line terminators) and
// remembers the file's encoding details so Text reproduces them exactly:
// the byte-order mark, the line ending (LF or CRLF) and whether the file
// ended with a newline.
//
// Line endings: a file is treated as CRLF only when every line break is
// "\r\n"; the "\r" is then stripped from the lines and restored on output.
// In a mixed file the stray "\r" bytes stay in the line text (shown as ^M),
// so an untouched line always round-trips byte for byte.
type buffer struct {
	lines   []string
	crlf    bool
	bom     bool
	finalNL bool
	// wasEmpty records that the original text was empty; a final newline
	// is then added once there is content (a POSIX text file).
	wasEmpty bool

	// undo log
	undo    []*group
	redo    []*group
	cur     *group // open group receiving ops, or nil
	seq     int    // id of the newest committed state
	nextID  int
	savedID int // state id at the last save

	// dirtyFrom is the smallest line index touched since the editor last
	// reset it (for style-state invalidation); -1 when nothing changed.
	dirtyFrom int
	version   int
	// groupCursor is the cursor at the start of the current command; a
	// group opened by the first edit records it as the undo position.
	groupCursor Pos
}

// op is one primitive edit: at pos, the text removed was replaced by the
// text inserted.
type op struct {
	pos      Pos
	removed  string
	inserted string
}

// group is one undoable change: a Normal-mode command, or a whole insert
// session together with the command that started it.
type group struct {
	ops          []op
	cursorBefore Pos
	cursorAfter  Pos
	id           int // state id after this group
	prevID       int // state id before this group
}

func newBuffer(data []byte) *buffer {
	b := &buffer{dirtyFrom: -1}
	s := string(data)
	if strings.HasPrefix(s, "\ufeff") {
		b.bom = true
		s = s[len("\ufeff"):]
	}
	if s == "" {
		b.wasEmpty = true
		b.lines = []string{""}
		return b
	}
	lf := strings.Count(s, "\n")
	crlf := strings.Count(s, "\r\n")
	if lf > 0 && crlf == lf {
		b.crlf = true
		s = strings.ReplaceAll(s, "\r\n", "\n")
	}
	if strings.HasSuffix(s, "\n") {
		b.finalNL = true
		s = s[:len(s)-1]
	}
	b.lines = strings.Split(s, "\n")
	return b
}

// text returns the buffer as file bytes.
func (b *buffer) text() []byte {
	eol := "\n"
	if b.crlf {
		eol = "\r\n"
	}
	n := 0
	for _, l := range b.lines {
		n += len(l) + len(eol)
	}
	var sb strings.Builder
	sb.Grow(n + 3)
	if b.bom {
		sb.WriteString("\ufeff")
	}
	for i, l := range b.lines {
		if i > 0 {
			sb.WriteString(eol)
		}
		sb.WriteString(l)
	}
	final := b.finalNL
	if b.wasEmpty && !(len(b.lines) == 1 && b.lines[0] == "") {
		final = true
	}
	if final {
		sb.WriteString(eol)
	}
	return []byte(sb.String())
}

func (b *buffer) lineCount() int { return len(b.lines) }

func (b *buffer) line(i int) string {
	if i < 0 || i >= len(b.lines) {
		return ""
	}
	return b.lines[i]
}

// clampPos keeps p inside the buffer (Col may equal the line length).
func (b *buffer) clampPos(p Pos) Pos {
	if p.Line < 0 {
		p.Line = 0
	}
	if p.Line >= len(b.lines) {
		p.Line = len(b.lines) - 1
	}
	if p.Col < 0 {
		p.Col = 0
	}
	if l := len(b.lines[p.Line]); p.Col > l {
		p.Col = l
	}
	return p
}

// endPos returns the last position of the buffer (after its last byte).
func (b *buffer) endPos() Pos {
	n := len(b.lines) - 1
	return Pos{n, len(b.lines[n])}
}

// get returns the text between two positions, lines joined by "\n".
func (b *buffer) get(start, end Pos) string {
	start, end = b.clampPos(start), b.clampPos(end)
	if !start.Less(end) {
		return ""
	}
	if start.Line == end.Line {
		return b.lines[start.Line][start.Col:end.Col]
	}
	var sb strings.Builder
	sb.WriteString(b.lines[start.Line][start.Col:])
	for i := start.Line + 1; i < end.Line; i++ {
		sb.WriteByte('\n')
		sb.WriteString(b.lines[i])
	}
	sb.WriteByte('\n')
	sb.WriteString(b.lines[end.Line][:end.Col])
	return sb.String()
}

// advance returns the position after text inserted at p.
func advance(p Pos, text string) Pos {
	nl := strings.Count(text, "\n")
	if nl == 0 {
		return Pos{p.Line, p.Col + len(text)}
	}
	return Pos{p.Line + nl, len(text) - strings.LastIndexByte(text, '\n') - 1}
}

// rawReplace replaces [start, end) with text without touching the undo log
// and returns the end of the inserted text.
func (b *buffer) rawReplace(start, end Pos, text string) Pos {
	first := b.lines[start.Line][:start.Col]
	last := b.lines[end.Line][end.Col:]
	parts := strings.Split(text, "\n")
	parts[0] = first + parts[0]
	endPos := Pos{start.Line + len(parts) - 1, len(parts[len(parts)-1])}
	if len(parts) == 1 {
		endPos.Col = len(parts[0])
	}
	parts[len(parts)-1] += last
	oldN := end.Line - start.Line + 1
	newN := len(parts)
	switch {
	case newN == oldN:
		copy(b.lines[start.Line:], parts)
	case newN < oldN:
		copy(b.lines[start.Line:], parts)
		b.lines = append(b.lines[:start.Line+newN], b.lines[end.Line+1:]...)
	default:
		grow := newN - oldN
		b.lines = append(b.lines, make([]string, grow)...)
		copy(b.lines[end.Line+1+grow:], b.lines[end.Line+1:len(b.lines)-grow])
		copy(b.lines[start.Line:], parts)
	}
	if b.dirtyFrom < 0 || start.Line < b.dirtyFrom {
		b.dirtyFrom = start.Line
	}
	b.version++
	return endPos
}

// replace replaces [start, end) with text, logging the change in the open
// undo group (a group is opened if none is), and returns the end of the
// inserted text.
func (b *buffer) replace(start, end Pos, text string) Pos {
	start, end = b.clampPos(start), b.clampPos(end)
	if end.Less(start) {
		start, end = end, start
	}
	removed := b.get(start, end)
	if removed == "" && text == "" {
		return start
	}
	if b.cur == nil {
		b.begin(b.groupCursor)
	}
	b.cur.ops = append(b.cur.ops, op{pos: start, removed: removed, inserted: text})
	b.redo = nil
	return b.rawReplace(start, end, text)
}

func (b *buffer) insert(p Pos, text string) Pos { return b.replace(p, p, text) }

func (b *buffer) delete(start, end Pos) string {
	start, end = b.clampPos(start), b.clampPos(end)
	if end.Less(start) {
		start, end = end, start
	}
	s := b.get(start, end)
	b.replace(start, end, "")
	return s
}

// setLine replaces the whole text of line i.
func (b *buffer) setLine(i int, s string) {
	if b.lines[i] == s {
		return
	}
	// Replace only the differing middle so undo records stay small and
	// cursor restoration lands near the change.
	old := b.lines[i]
	p := 0
	for p < len(old) && p < len(s) && old[p] == s[p] {
		p++
	}
	q := 0
	for q < len(old)-p && q < len(s)-p && old[len(old)-1-q] == s[len(s)-1-q] {
		q++
	}
	b.replace(Pos{i, p}, Pos{i, len(old) - q}, s[p:len(s)-q])
}

// begin opens an undo group if none is open.
func (b *buffer) begin(cursor Pos) {
	if b.cur != nil {
		return
	}
	b.cur = &group{cursorBefore: cursor}
}

// commit closes the open group, recording cursor as the position after it.
// Empty groups are discarded.
func (b *buffer) commit(cursor Pos) {
	g := b.cur
	b.cur = nil
	if g == nil || len(g.ops) == 0 {
		return
	}
	g.cursorAfter = cursor
	g.prevID = b.seq
	b.nextID++
	g.id = b.nextID
	b.seq = g.id
	b.undo = append(b.undo, g)
	b.redo = nil
}

// undoOne reverts the newest group; it returns the cursor to restore.
func (b *buffer) undoOne() (Pos, bool) {
	if len(b.undo) == 0 {
		return Pos{}, false
	}
	g := b.undo[len(b.undo)-1]
	b.undo = b.undo[:len(b.undo)-1]
	for i := len(g.ops) - 1; i >= 0; i-- {
		o := g.ops[i]
		b.rawReplace(o.pos, advance(o.pos, o.inserted), o.removed)
	}
	b.redo = append(b.redo, g)
	b.seq = g.prevID
	return g.cursorBefore, true
}

// redoOne re-applies the newest undone group; it returns the cursor: the
// start of the first change, as Vim does.
func (b *buffer) redoOne() (Pos, bool) {
	if len(b.redo) == 0 {
		return Pos{}, false
	}
	g := b.redo[len(b.redo)-1]
	b.redo = b.redo[:len(b.redo)-1]
	for _, o := range g.ops {
		b.rawReplace(o.pos, advance(o.pos, o.removed), o.inserted)
	}
	b.undo = append(b.undo, g)
	b.seq = g.id
	p := g.cursorBefore
	if len(g.ops) > 0 {
		p = g.ops[0].pos
		for _, o := range g.ops {
			p = minPos(p, o.pos)
		}
	}
	return p, true
}

// dirty reports whether the text differs from the last saved state (undo
// back to the saved state makes the buffer clean again, as in Vim).
func (b *buffer) dirty() bool {
	return b.seq != b.savedID || (b.cur != nil && len(b.cur.ops) > 0)
}

func (b *buffer) markSaved() { b.savedID = b.seq }
