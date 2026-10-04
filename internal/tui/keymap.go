package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ZahakJ/astrolabe-cli/internal/term"
)

// The reader keymap of DESIGN.md §4.3 is one table: the dispatcher looks
// keys up in it, the help overlay prints it, and the leader palette lists its
// Space bindings. Help therefore cannot drift from what the keys do.

// binding is one reader command.
type binding struct {
	// keys are the key sequences that run it, each a space-separated list
	// of term.KeyEvent.String() names ("g g", "space f", "ctrl+d").
	keys []string
	// section groups the help screen.
	section string
	// desc is the help description; word the one-word label shown in the
	// leader palette (Space bindings only).
	desc, word string
	// run executes the command; count is the typed count (0 = none).
	run func(a *app, count int)
}

// Help sections, in display order.
const (
	secMove   = "Move"
	secLinks  = "Links and history"
	secFold   = "Folds, tasks, editing"
	secSearch = "Search"
	secLeader = "Space (leader)"
	secOther  = "Other"
)

var sections = []string{secMove, secLinks, secFold, secSearch, secLeader, secOther}

// keymap is the reader's dispatch table. It is filled in init to break the
// initialisation cycle (the help command reads the table).
var keymap []binding

// keyIndex maps a full key sequence to its binding; keyPrefix holds every
// proper prefix of a sequence.
var (
	keyIndex  map[string]*binding
	keyPrefix map[string]bool
)

func init() {
	keymap = []binding{
		{keys: []string{"j", "down"}, section: secMove, desc: "cursor down (counts work: 5j)", run: func(a *app, n int) { a.cursorMove(max(n, 1)) }},
		{keys: []string{"k", "up"}, section: secMove, desc: "cursor up", run: func(a *app, n int) { a.cursorMove(-max(n, 1)) }},
		{keys: []string{"ctrl+d"}, section: secMove, desc: "half page down", run: func(a *app, n int) { a.halfPage(1) }},
		{keys: []string{"ctrl+u"}, section: secMove, desc: "half page up", run: func(a *app, n int) { a.halfPage(-1) }},
		{keys: []string{"ctrl+f", "pgdown"}, section: secMove, desc: "page down", run: func(a *app, n int) { a.fullPage(1) }},
		{keys: []string{"ctrl+b", "pgup"}, section: secMove, desc: "page up", run: func(a *app, n int) { a.fullPage(-1) }},
		{keys: []string{"g g", "home"}, section: secMove, desc: "top (or line N with a count)", run: func(a *app, n int) { a.gotoTop(n) }},
		{keys: []string{"G", "end"}, section: secMove, desc: "bottom", run: func(a *app, n int) { a.gotoBottom(n) }},
		{keys: []string{"] ]", "}"}, section: secMove, desc: "next heading", run: func(a *app, n int) { a.nextHeading(max(n, 1)) }},
		{keys: []string{"[ [", "{"}, section: secMove, desc: "previous heading", run: func(a *app, n int) { a.nextHeading(-max(n, 1)) }},
		{keys: []string{"l", "right"}, section: secMove, desc: "scroll a code block right", run: func(a *app, n int) { a.codeScroll(max(n, 1) * 4) }},
		{keys: []string{"h", "left"}, section: secMove, desc: "scroll a code block left", run: func(a *app, n int) { a.codeScroll(-max(n, 1) * 4) }},

		{keys: []string{"tab"}, section: secLinks, desc: "next link (highlights it)", run: func(a *app, n int) { a.cycleLink(1) }},
		{keys: []string{"shift+tab"}, section: secLinks, desc: "previous link", run: func(a *app, n int) { a.cycleLink(-1) }},
		{keys: []string{"enter", "g d", "ctrl+]"}, section: secLinks, desc: "follow the highlighted link (or the first on the line)", run: func(a *app, n int) { a.follow() }},
		{keys: []string{"backspace", "ctrl+o", "H"}, section: secLinks, desc: "back in history", run: func(a *app, n int) { a.historyBack() }},
		{keys: []string{"L"}, section: secLinks, desc: "forward in history", run: func(a *app, n int) { a.historyForward() }},

		{keys: []string{"z a"}, section: secFold, desc: "fold or unfold the section under the cursor", run: func(a *app, n int) { a.foldCmd(foldToggle) }},
		{keys: []string{"z c"}, section: secFold, desc: "fold the section", run: func(a *app, n int) { a.foldCmd(foldClose) }},
		{keys: []string{"z o"}, section: secFold, desc: "unfold the section", run: func(a *app, n int) { a.foldCmd(foldOpen) }},
		{keys: []string{"z M"}, section: secFold, desc: "fold every section", run: func(a *app, n int) { a.foldAll(true) }},
		{keys: []string{"z R"}, section: secFold, desc: "unfold everything", run: func(a *app, n int) { a.foldAll(false) }},
		{keys: []string{"x"}, section: secFold, desc: "toggle the task under the cursor", run: func(a *app, n int) { a.toggleTask() }},
		{keys: []string{"i"}, section: secFold, desc: "edit: Insert mode at the start of this block's line", run: func(a *app, n int) { a.editHere(false, editInsertStart) }},
		{keys: []string{"a"}, section: secFold, desc: "edit: Insert mode at the end of this block's line", run: func(a *app, n int) { a.editHere(false, editInsertEnd) }},
		{keys: []string{"e"}, section: secFold, desc: "edit: Normal mode on this block's line", run: func(a *app, n int) { a.editHere(false, editNormal) }},
		{keys: []string{"E"}, section: secFold, desc: "open $EDITOR at this line", run: func(a *app, n int) { a.editHere(true, editNormal) }},

		{keys: []string{"/"}, section: secSearch, desc: "search inside the note", run: func(a *app, n int) { a.openPrompt('/') }},
		{keys: []string{"n"}, section: secSearch, desc: "next match", run: func(a *app, n int) { a.searchNext(max(n, 1)) }},
		{keys: []string{"N"}, section: secSearch, desc: "previous match", run: func(a *app, n int) { a.searchNext(-max(n, 1)) }},
		{keys: []string{"ctrl+p"}, section: secSearch, desc: "find a note", run: func(a *app, n int) { a.openFinder(false) }},

		{keys: []string{"space f"}, section: secLeader, word: "find", desc: "find a note (title, path, alias)", run: func(a *app, n int) { a.openFinder(false) }},
		{keys: []string{"space /", "space s"}, section: secLeader, word: "search", desc: "search the vault's text", run: func(a *app, n int) { a.openVaultSearch() }},
		{keys: []string{"space d"}, section: secLeader, word: "today", desc: "today's daily note", run: func(a *app, n int) { a.openToday() }},
		{keys: []string{"space c"}, section: secLeader, word: "capture", desc: "capture a line to today's note", run: func(a *app, n int) { a.openCapture() }},
		{keys: []string{"space n"}, section: secLeader, word: "new", desc: "new note", run: func(a *app, n int) { a.openNewNote() }},
		{keys: []string{"space t"}, section: secLeader, word: "agenda", desc: "open tasks by due date", run: func(a *app, n int) { a.openAgenda() }},
		{keys: []string{"space #"}, section: secLeader, word: "tags", desc: "tags, then notes with a tag", run: func(a *app, n int) { a.openTags() }},
		{keys: []string{"space b"}, section: secLeader, word: "backlinks", desc: "focus backlinks in the context panel", run: func(a *app, n int) { a.toggleCtxFocus(ctxBacklinks) }},
		{keys: []string{"space o"}, section: secLeader, word: "outline", desc: "focus the outline in the context panel", run: func(a *app, n int) { a.toggleCtxFocus(ctxOutline) }},
		{keys: []string{"space e"}, section: secLeader, word: "tree", desc: "toggle the file tree", run: func(a *app, n int) { a.toggleTree() }},
		{keys: []string{"space r"}, section: secLeader, word: "recent", desc: "recent notes", run: func(a *app, n int) { a.openFinder(true) }},
		{keys: []string{"space T"}, section: secLeader, word: "theme", desc: "cycle the theme", run: func(a *app, n int) { a.cycleTheme() }},
		{keys: []string{"space y"}, section: secLeader, word: "yank", desc: "copy this note's wikilink", run: func(a *app, n int) { a.copyWikilink() }},
		{keys: []string{"space q"}, section: secLeader, word: "quit", desc: "quit", run: func(a *app, n int) { a.requestQuit() }},

		{keys: []string{"q", "Z Z"}, section: secOther, desc: "quit (asks if there are unsaved edits)", run: func(a *app, n int) { a.requestQuit() }},
		{keys: []string{"?"}, section: secOther, desc: "this help", run: func(a *app, n int) { a.pushOverlay(newHelp(a)) }},
		{keys: []string{":"}, section: secOther, desc: "commands: :e NOTE :new :today :theme :export :set wrap=N", run: func(a *app, n int) { a.openPrompt(':') }},
		{keys: []string{"esc"}, section: secOther, desc: "clear the highlighted link and search", run: func(a *app, n int) { a.clearTransient() }},
		{keys: []string{"ctrl+l"}, section: secOther, desc: "redraw the screen", run: func(a *app, n int) { a.scr.Invalidate() }},
		{keys: []string{"ctrl+z"}, section: secOther, desc: "suspend (fg to return)", run: func(a *app, n int) {}},
	}
	keyIndex = map[string]*binding{}
	keyPrefix = map[string]bool{}
	for i := range keymap {
		b := &keymap[i]
		for _, k := range b.keys {
			if _, dup := keyIndex[k]; dup {
				panic("tui: key bound twice: " + k)
			}
			keyIndex[k] = b
			parts := strings.Split(k, " ")
			for j := 1; j < len(parts); j++ {
				keyPrefix[strings.Join(parts[:j], " ")] = true
			}
		}
	}
	for k := range keyIndex {
		if keyPrefix[k] {
			panic("tui: key sequence is also a prefix: " + k)
		}
	}
}

// keyState is the reader's pending input: a count and the keys of an
// incomplete sequence.
type keyState struct {
	count    int
	pending  []string
	leaderAt time.Time // when to show the leader palette (zero = never)
}

func (k *keyState) reset() {
	k.count = 0
	k.pending = nil
	k.leaderAt = time.Time{}
}

func (k *keyState) leaderPending() bool {
	return len(k.pending) == 1 && k.pending[0] == "space"
}

// leaderDelay is the pause after Space before the palette appears.
const leaderDelay = 300 * time.Millisecond

// dispatch feeds one key to the keymap. It returns false when the key
// completed no binding (so a caller may try it elsewhere).
func (a *app) dispatch(k term.KeyEvent) bool {
	ks := k.String()
	ksT := &a.keys
	// Counts: digits before a command (0 only after another digit).
	if len(ksT.pending) == 0 && k.Key == term.KeyRune && k.Mod == 0 &&
		k.Rune >= '0' && k.Rune <= '9' && (k.Rune != '0' || ksT.count > 0) {
		if ksT.count < 100000 {
			ksT.count = ksT.count*10 + int(k.Rune-'0')
		}
		return true
	}
	seq := strings.Join(append(append([]string(nil), ksT.pending...), ks), " ")
	if keyPrefix[seq] {
		ksT.pending = append(ksT.pending, ks)
		if seq == "space" {
			ksT.leaderAt = a.now().Add(leaderDelay)
		}
		return true
	}
	b := keyIndex[seq]
	count := ksT.count
	hadPending := len(ksT.pending) > 0
	ksT.reset()
	if hadPending {
		// The palette (if it was shown for this leader) has done its job.
		a.closePalette()
	}
	if b == nil {
		if hadPending && ks != "esc" {
			a.flash("%s is not bound (? for help)", keyDisplay(seq, a.gl.Name == "ascii"))
		}
		return false
	}
	b.run(a, count)
	return true
}

// keyDisplay renders a key sequence for people: "space f" → "Space f",
// "ctrl+d" → "Ctrl-d", "g g" → "gg".
func keyDisplay(seq string, ascii bool) string {
	parts := strings.Split(seq, " ")
	var out []string
	allChars := true
	for _, p := range parts {
		d := keyName(p, ascii)
		if len([]rune(p)) != 1 {
			allChars = false
		}
		out = append(out, d)
	}
	if allChars {
		return strings.Join(out, "")
	}
	return strings.Join(out, " ")
}

func keyName(k string, ascii bool) string {
	names := map[string][2]string{
		"space": {"Space", "Space"}, "enter": {"Enter", "Enter"}, "esc": {"Esc", "Esc"},
		"tab": {"Tab", "Tab"}, "shift+tab": {"Shift-Tab", "Shift-Tab"},
		"backspace": {"Backspace", "Backspace"}, "down": {"↓", "Down"}, "up": {"↑", "Up"},
		"left": {"←", "Left"}, "right": {"→", "Right"}, "pgdown": {"PgDn", "PgDn"},
		"pgup": {"PgUp", "PgUp"}, "home": {"Home", "Home"}, "end": {"End", "End"},
	}
	if n, ok := names[k]; ok {
		if ascii {
			return n[1]
		}
		return n[0]
	}
	if strings.HasPrefix(k, "ctrl+") {
		return "Ctrl-" + strings.TrimPrefix(k, "ctrl+")
	}
	return k
}

// bindingKeys renders all of a binding's sequences ("j ↓").
func bindingKeys(b *binding, ascii bool) string {
	var out []string
	for _, k := range b.keys {
		out = append(out, keyDisplay(k, ascii))
	}
	return strings.Join(out, "  ")
}

// leaderBindings lists the Space bindings in table order.
func leaderBindings() []*binding {
	var out []*binding
	for i := range keymap {
		if keymap[i].section == secLeader {
			out = append(out, &keymap[i])
		}
	}
	return out
}

func sprintf(format string, args ...any) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}
