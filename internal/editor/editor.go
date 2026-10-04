// Package editor is astrolabe's built-in modal editor (DESIGN.md §4.3): a
// deliberate Vim subset over a Markdown buffer, with Markdown assistance in
// Insert mode and a styled-source display that soft-wraps to the page
// measure.
//
// The editor is an embeddable component, not a program. The host (the TUI)
// owns files, the status bar and the event loop; the editor never touches the
// filesystem or the terminal. The host feeds it events:
//
//	ed := editor.New(editor.Config{Text: data, Theme: th, Glyphs: gl})
//	res := ed.HandleKey(ev)        // or HandlePaste / HandleMouse
//	switch res.Action { case editor.ActionSave: write ed.Text(); ed.MarkSaved() … }
//	cur := ed.Draw(screen, rect)   // draws the page into rect, places the cursor
//	if ed.CommandLineActive() { ed.DrawCommandLine(screen, statusRect) }
//
// # Command line
//
// The ":" command line and the "/" "?" search prompt are not drawn inside
// the editor's rectangle: DESIGN.md §4.2 puts them in the status bar's row,
// which the host owns. The host draws them either with DrawCommandLine (which
// also places the terminal cursor there) or from the raw state returned by
// CommandLine. While the command line is active Draw hides the text cursor.
//
// # Positions
//
// Lines are 0-based, matching internal/md block positions (frontmatter lines
// count; a BOM is not part of line 0). Columns are byte offsets into the line
// and always fall on a grapheme-cluster boundary.
//
// # Choices where DESIGN.md is silent (each also noted at its code)
//
//   - Esc in Normal mode with nothing pending returns ActionLeave; the host
//     saves first when Dirty (autosave on leave) and returns to the reader.
//   - :q on a dirty buffer refuses with E37, like Vim; ZZ and :x return
//     ActionSaveQuit and the host saves only when Dirty. What "quit" means
//     (back to the reader, or leaving astrolabe) is the host's decision.
//   - Ex commands the editor does not know (":e NOTE", ":theme x", ":w FILE",
//     …) are passed to the host as ActionCommand.
//   - j/k without a count move by display line (as gj/gk); with a count or
//     after an operator they move by logical line, like the popular
//     `v:count ? 'j' : 'gj'` mapping, so 5j still means five source lines.
//   - Y yanks to the end of the line (Neovim's default).
//   - o on a list item, task or quote continues it, like Enter at its end.
//   - Tab outside a list or table inserts spaces to the next multiple of 4
//     (or a tab when the file indents with tabs); auto-pairing is off.
//   - Ctrl-t inserts "YYYY-MM-DD HH:MM".
//   - Search patterns use Vim's "magic" syntax (\( \) \| \+ \? \= \{ \< \>
//     \c \C), translated to Go regexps; matching is per line, smart-case.
//     Back-references inside a pattern (\1) are not available in RE2; in
//     :s replacements \1…\9, & and \r work.
//   - Tab in a table re-aligns it and puts the cursor at the end of the next
//     cell's content (typing appends); Tab in the last cell adds a row.
//   - The cursor line's soft ground covers the cursor's display row, not
//     every row of a wrapped line (j/k move by display row too).
//   - Line endings are kept: CRLF when every break was CRLF; otherwise LF,
//     with any stray CR kept in the line text (shown as ^M). An empty file
//     gets a final newline once it has content.
package editor

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
)

// Mode is the editor's modal state.
type Mode int

// Modes. The command line is not a mode of its own: it is active over the
// mode it was opened from (see CommandLineActive).
const (
	ModeNormal Mode = iota
	ModeInsert
	ModeVisual
	ModeVisualLine
)

// String returns the label for the status-bar pill: NORMAL, INSERT, VISUAL
// or V-LINE.
func (m Mode) String() string {
	switch m {
	case ModeInsert:
		return "INSERT"
	case ModeVisual:
		return "VISUAL"
	case ModeVisualLine:
		return "V-LINE"
	}
	return "NORMAL"
}

// Action tells the host what an event asks it to do.
type Action int

// Actions.
const (
	// ActionNone: nothing for the host to do beyond redrawing.
	ActionNone Action = iota
	// ActionSave: write Text() to the file (:w, Ctrl-s), then MarkSaved.
	ActionSave
	// ActionQuit: leave the editor; the buffer is clean (:q).
	ActionQuit
	// ActionQuitDiscard: leave the editor, discarding changes (:q!).
	ActionQuitDiscard
	// ActionSaveQuit: save if Dirty, then leave (:wq, :x, ZZ).
	ActionSaveQuit
	// ActionLeave: Esc in Normal mode with nothing pending — return to the
	// reader, saving first if Dirty (DESIGN.md autosave on leave).
	ActionLeave
	// ActionCommand: an ex command the editor does not handle itself;
	// Result.Command holds it without the leading colon.
	ActionCommand
)

// String names the action (for logs and tests).
func (a Action) String() string {
	return [...]string{"none", "save", "quit", "quit!", "save+quit", "leave", "command"}[a]
}

// Result is the outcome of one event.
type Result struct {
	Action Action
	// Command is the ex command for ActionCommand (e.g. "e Ideas").
	Command string
	// Message is a transient message for the status bar ("3 substitutions
	// on 2 lines", "search hit BOTTOM, continuing at TOP"), possibly
	// accompanying another action.
	Message string
	// Error marks Message as an error (shown in danger ink).
	Error bool
}

// Candidate is one entry of the [[ link completer.
type Candidate struct {
	// Target is inserted between the brackets: the note's link name.
	Target string
	// Label is shown in the popup (usually the title).
	Label string
	// Detail is shown muted after the label (usually the path).
	Detail string
}

// Completer returns the candidates for a [[ query, best first. The host
// ranks them (it knows titles, aliases and recency); FuzzyCompleter builds
// one from a fixed list.
type Completer func(query string) []Candidate

// Config configures New. Only Text is needed; zero values pick defaults.
type Config struct {
	// Text is the file content, exactly as read.
	Text []byte
	// Theme and Glyphs style the display (default: onyx, Unicode).
	Theme  theme.Theme
	Glyphs theme.Glyphs
	// Measure is the soft-wrap width in cells (default 78).
	Measure int
	// Bidi enables visual reordering and Arabic shaping of RTL lines (for
	// terminals without bidi of their own; see term.Caps.Bidi).
	Bidi bool
	// Number shows absolute line numbers (:set nu).
	Number bool
	// Completer feeds the [[ popup; nil disables it.
	Completer Completer
	// Clipboard receives text yanked to the "+ (or "*) register, for OSC 52.
	Clipboard func(string) error
	// Now returns the current time (Ctrl-t); nil means time.Now.
	Now func() time.Time
	// Width and Height are the initial view size, used by motions such as
	// Ctrl-d before the first Draw (default 80×24).
	Width, Height int
}

// Cursor is where the terminal cursor goes after Draw.
type Cursor struct {
	X, Y    int
	Shape   term.CursorShape
	Visible bool
}

// Status is the information the host shows in the status bar.
type Status struct {
	Mode Mode
	// Line is the 1-based cursor line; Col the 1-based character
	// (grapheme) column; VCol the 1-based display column.
	Line, Col, VCol int
	// Lines is the number of lines in the buffer.
	Lines int
	// Pending holds typed keys of an incomplete command ("2d", "\"a").
	Pending string
	// Dirty reports unsaved changes.
	Dirty bool
}

type register struct {
	text     string
	linewise bool
}

// input is one recorded event for dot-repeat and insert repetition.
type input struct {
	tok   string // key token ("a", "<CR>", "<C-w>")
	paste string // bracketed paste text, when tok == ""
	// completion acceptance: delete back bytes before the cursor, then
	// insert text
	complete bool
	back     int
	text     string
}

type change struct {
	inputs []input
	count  int
	visual *visualShape
}

type visualShape struct {
	mode  Mode
	lines int // line span - 1
	cols  int // clusters on a single line, or end byte column for multi-line
}

type pending struct {
	reg    rune
	count1 int
	count2 int
	op     string
	prefix string
	keys   []string
}

func (p *pending) empty() bool {
	return p.reg == 0 && p.count1 == 0 && p.count2 == 0 && p.op == "" && p.prefix == ""
}

func (p *pending) count() (int, bool) {
	if p.count1 == 0 && p.count2 == 0 {
		return 1, false
	}
	c1, c2 := p.count1, p.count2
	if c1 == 0 {
		c1 = 1
	}
	if c2 == 0 {
		c2 = 1
	}
	return c1 * c2, true
}

type insertSession struct {
	count int
	kind  string
	rec   []input
}

type findState struct {
	target  string
	forward bool
	till    bool
}

type viewPos struct{ line, row int }

// Editor is the modal editor. It is not safe for concurrent use.
type Editor struct {
	buf    *buffer
	cur    Pos
	mode   Mode
	want   int // desired display column for j/k on logical lines
	wantX  int // desired x offset for display-line j/k
	anchor Pos // visual mode anchor

	pend      pending
	regs      map[rune]register
	lastFind  *findState
	search    searchState
	lastSub   *substitution
	cmd       cmdline
	completer completerState

	rec        []input
	recCount   int
	changed    bool
	insRepeat  bool         // replaying an insertion for its count
	recVisual  *visualShape // set while recording a Visual-mode change
	recording  bool
	lastChange *change
	replaying  bool
	ins        insertSession

	lastVisual struct {
		anchor, cur Pos
		mode        Mode
		set         bool
	}
	visStart, visEnd int // '< and '> lines

	// display
	th        theme.Theme
	gl        theme.Glyphs
	measure   int
	bidi      bool
	number    bool
	width     int
	height    int
	top       viewPos
	wraps     wrapCache
	states    []lineState
	fmEnd     int // closing line of the frontmatter, -1 if none
	layout    []layoutRow
	rect      term.Rect
	pageX     int
	pageW     int
	cfgCompl  Completer
	clipboard func(string) error
	now       func() time.Time
	indent    string // indent unit for >> << and list Tab

	mouseDown    bool
	mouseAnchor  Pos
	pendingMsg   string
	pendingError bool
}

// New creates an editor over cfg.Text in Normal mode with the cursor on the
// first line.
func New(cfg Config) *Editor {
	e := &Editor{
		buf:       newBuffer(cfg.Text),
		regs:      map[rune]register{},
		th:        cfg.Theme,
		gl:        cfg.Glyphs,
		measure:   cfg.Measure,
		bidi:      cfg.Bidi,
		number:    cfg.Number,
		width:     cfg.Width,
		height:    cfg.Height,
		cfgCompl:  cfg.Completer,
		clipboard: cfg.Clipboard,
		now:       cfg.Now,
	}
	if e.th.Name == "" {
		e.th = theme.DefaultTheme()
	}
	if e.gl.Name == "" {
		e.gl = theme.UnicodeGlyphs
	}
	if e.measure <= 0 {
		e.measure = 78
	}
	if e.width <= 0 {
		e.width = 80
	}
	if e.height <= 0 {
		e.height = 24
	}
	if e.now == nil {
		e.now = time.Now
	}
	e.fmEnd = -1
	e.search.smartcase = true
	e.search.ignorecase = true
	e.search.hl = true
	e.indent = detectIndent(e.buf.lines)
	return e
}

// Text returns the buffer as file bytes, with the original BOM, line
// endings and final-newline state.
func (e *Editor) Text() []byte { return e.buf.text() }

// Dirty reports whether the buffer differs from the text it was created
// with or last marked saved. Undoing back to that state makes it clean.
func (e *Editor) Dirty() bool { return e.buf.dirty() }

// MarkSaved records that the current text was written to disk.
func (e *Editor) MarkSaved() { e.buf.markSaved() }

// Mode returns the current mode.
func (e *Editor) Mode() Mode { return e.mode }

// LineCount returns the number of lines in the buffer.
func (e *Editor) LineCount() int { return e.buf.lineCount() }

// CursorLine returns the cursor's 0-based line.
func (e *Editor) CursorLine() int { return e.cur.Line }

// CursorPos returns the cursor position (0-based line, byte column).
func (e *Editor) CursorPos() Pos { return e.cur }

// SetCursor moves the cursor to a 0-based line and byte column (clamped
// and snapped to a grapheme boundary) and scrolls it into view. Use it when
// opening the editor on the reader's block.
func (e *Editor) SetCursor(line, col int) {
	p := e.buf.clampPos(Pos{line, col})
	s := e.buf.line(p.Line)
	p.Col = snapCol(s, p.Col)
	e.cur = p
	e.clampCursor()
	e.setWant()
	e.ensureVisible()
}

// SetCursorLine moves the cursor to the first non-blank of a 0-based line
// and places that line about a third of the way down the view, as the
// reader → editor switch wants.
func (e *Editor) SetCursorLine(line int) {
	p := e.buf.clampPos(Pos{line, 0})
	p.Col = firstNonBlank(e.buf.line(p.Line))
	e.cur = p
	e.setWant()
	e.top = e.back(viewPos{p.Line, 0}, e.height/3)
	e.ensureVisible()
}

// StartInsert enters Insert mode on the cursor line, as the reader's `i`
// and `a` do (DESIGN.md §4.3: the loop is `i` … type … `Esc Esc`): at the
// start of the line's text, after any quote, list, task or heading marker,
// or (atEnd) at the end of the line, as `A` does. It is one ordinary insert
// session, so `u` undoes it and `.` repeats it.
func (e *Editor) StartInsert(atEnd bool) {
	if e.mode != ModeNormal || e.cmd.active {
		return
	}
	line := e.buf.line(e.cur.Line)
	col := contentStart(line)
	if atEnd || col >= len(line) {
		e.feed(input{tok: "A"})
		return
	}
	e.cur.Col = col
	e.setWant()
	e.feed(input{tok: "i"})
}

// contentStart is the byte offset where a source line's text begins: after
// block-quote markers, a list marker and task box, or a heading's #s.
func contentStart(s string) int {
	if lp, ok := parseListPrefix(s); ok {
		return lp.end
	}
	i := firstNonBlankInsert(s)
	j := i
	for j < len(s) && j-i < 6 && s[j] == '#' {
		j++
	}
	if j > i && (j == len(s) || s[j] == ' ' || s[j] == '\t') {
		for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
			j++
		}
		return j
	}
	return i
}

// SetTheme changes the theme (and glyphs) used by Draw.
func (e *Editor) SetTheme(t theme.Theme, g theme.Glyphs) {
	e.th = t
	if g.Name != "" {
		e.gl = g
	}
}

// SetBidi turns visual reordering of RTL lines on or off.
func (e *Editor) SetBidi(on bool) { e.bidi = on }

// SetMeasure sets the soft-wrap width (:set wrap=N does the same).
func (e *Editor) SetMeasure(n int) {
	if n < 10 {
		n = 10
	}
	e.measure = n
	e.wraps.reset()
	e.ensureVisible()
}

// Measure returns the soft-wrap width (changed by :set wrap=N).
func (e *Editor) Measure() int { return e.measure }

// Number reports whether line numbers are shown (changed by :set nu).
func (e *Editor) Number() bool { return e.number }

// SetNumber turns absolute line numbers on or off (:set nu).
func (e *Editor) SetNumber(on bool) {
	e.number = on
	e.wraps.reset()
}

// SetCompleter replaces the [[ completer.
func (e *Editor) SetCompleter(c Completer) { e.cfgCompl = c }

// Resize tells the editor the size of the rectangle it will be drawn in,
// so that page motions and scrolling are right before the next Draw.
func (e *Editor) Resize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if w != e.width {
		e.wraps.reset()
	}
	e.width, e.height = w, h
	e.ensureVisible()
}

// Pending returns the keys of an incomplete Normal-mode command.
func (e *Editor) Pending() string { return strings.Join(e.pend.keys, "") }

// Status returns the status-bar information.
func (e *Editor) Status() Status {
	s := e.buf.line(e.cur.Line)
	col := e.cur.Col
	if col > len(s) {
		col = len(s)
	}
	return Status{
		Mode:    e.mode,
		Line:    e.cur.Line + 1,
		Col:     countClusters(s, col) + 1,
		VCol:    vcolOf(s, col) + 1,
		Lines:   e.buf.lineCount(),
		Pending: e.Pending(),
		Dirty:   e.Dirty(),
	}
}

// Register returns the content of a register ('"' for the unnamed one)
// and whether it holds whole lines.
func (e *Editor) Register(r rune) (text string, linewise bool) {
	g := e.regs[r]
	return g.text, g.linewise
}

// HandleKey processes one key press.
func (e *Editor) HandleKey(k term.KeyEvent) Result {
	return e.feed(input{tok: keyToken(k)})
}

// HandlePaste inserts bracketed-paste text: into the command line when it
// is active, otherwise as typed text at the cursor (entering no mode; in
// Normal mode the text goes before the cursor, as Vim does).
func (e *Editor) HandlePaste(p term.PasteEvent) Result {
	return e.feed(input{paste: p.Text})
}

// feed dispatches one input to the current mode.
func (e *Editor) feed(in input) Result {
	var r Result
	switch {
	case e.cmd.active:
		r = e.cmdInput(in)
	case e.mode == ModeInsert:
		r = e.insertInput(in)
	case e.mode == ModeVisual || e.mode == ModeVisualLine:
		r = e.visualInput(in)
	default:
		r = e.normalInput(in)
	}
	if e.pendingMsg != "" && r.Message == "" {
		r.Message, r.Error = e.pendingMsg, e.pendingError
	}
	e.pendingMsg, e.pendingError = "", false
	e.syncStates()
	e.ensureVisible()
	return r
}

func (e *Editor) message(format string, args ...any) {
	e.pendingMsg = fmt.Sprintf(format, args...)
	e.pendingError = false
}

func (e *Editor) errorf(format string, args ...any) {
	e.pendingMsg = fmt.Sprintf(format, args...)
	e.pendingError = true
}

// keyToken renders a key in Vim notation: printable characters as
// themselves, others as <Esc>, <CR>, <C-r>, <S-Tab>, <Up> …
func keyToken(k term.KeyEvent) string {
	switch k.Key {
	case term.KeyRune:
		r := k.Rune
		if k.Mod&term.ModCtrl != 0 {
			if r >= 'A' && r <= 'Z' {
				r += 'a' - 'A'
			}
			return "<C-" + string(r) + ">"
		}
		if k.Mod&term.ModAlt != 0 {
			return "<A-" + string(r) + ">"
		}
		return string(r)
	case term.KeyEnter:
		return "<CR>"
	case term.KeyTab:
		if k.Mod&term.ModShift != 0 {
			return "<S-Tab>"
		}
		return "<Tab>"
	case term.KeyBackspace:
		return "<BS>"
	case term.KeyEscape:
		return "<Esc>"
	case term.KeyUp:
		return "<Up>"
	case term.KeyDown:
		return "<Down>"
	case term.KeyLeft:
		return "<Left>"
	case term.KeyRight:
		return "<Right>"
	case term.KeyHome:
		return "<Home>"
	case term.KeyEnd:
		return "<End>"
	case term.KeyPageUp:
		return "<PageUp>"
	case term.KeyPageDown:
		return "<PageDown>"
	case term.KeyInsert:
		return "<Insert>"
	case term.KeyDelete:
		return "<Del>"
	}
	if k.Key >= term.KeyF1 && k.Key <= term.KeyF12 {
		return fmt.Sprintf("<F%d>", int(k.Key-term.KeyF1)+1)
	}
	return ""
}

// isChar reports whether tok is a single printable character token.
func isChar(tok string) bool {
	return tok != "" && !(tok[0] == '<' && len(tok) > 1)
}

// clampCursor keeps the cursor on a valid position for the mode: in Normal
// and Visual mode it cannot sit after the last character.
func (e *Editor) clampCursor() {
	e.cur = e.buf.clampPos(e.cur)
	s := e.buf.line(e.cur.Line)
	e.cur.Col = snapCol(s, e.cur.Col)
	if e.mode != ModeInsert && e.cur.Col >= len(s) {
		// Visual mode after $ may sit on the line break, selecting it.
		visualEnd := (e.mode == ModeVisual || e.mode == ModeVisualLine) && e.want == math.MaxInt32 && s != ""
		if !visualEnd {
			e.cur.Col = lastCol(s)
		}
	}
}

// setWant remembers the cursor's display column for vertical motions.
func (e *Editor) setWant() {
	s := e.buf.line(e.cur.Line)
	e.want = vcolOf(s, e.cur.Col)
	e.wantX = e.cursorX()
}

// detectIndent picks the indent unit: a tab when tab-indented lines
// dominate, else two spaces (or four when the file's indents are all
// multiples of four).
func detectIndent(lines []string) string {
	tabs, spaces, four := 0, 0, 0
	for i, l := range lines {
		if i > 2000 {
			break
		}
		if strings.HasPrefix(l, "\t") {
			tabs++
		} else if strings.HasPrefix(l, "  ") {
			n := len(leadingWS(l))
			spaces++
			if n%4 == 0 {
				four++
			}
		}
	}
	if tabs > spaces {
		return "\t"
	}
	if spaces >= 3 && four == spaces {
		return "    "
	}
	return "  "
}
