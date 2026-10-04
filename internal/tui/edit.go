package tui

import (
	"errors"
	"path"
	"sort"
	"strings"

	"github.com/ZahakJ/folio/internal/editor"
	"github.com/ZahakJ/folio/internal/term"
	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/vault"
)

// editSession is the built-in editor open on one note.
type editSession struct {
	ed   *editor.Editor
	doc  *docView
	tok  vault.StatToken // the file version the buffer was loaded from / saved as
	kept []byte          // the text emergencySave last wrote (note or recovered)
	w, h int
}

// Ways into the built-in editor from the reader (DESIGN.md §4.3).
type editEntry int

const (
	editNormal      editEntry = iota // e: Normal mode on the block's first non-blank
	editInsertStart                  // i: Insert mode where the line's text begins
	editInsertEnd                    // a: Insert mode at the end of the line
)

// editHere opens the block under the reader's cursor in the built-in editor,
// or in $EDITOR (E, or config editor = external).
func (a *app) editHere(external bool, how editEntry) {
	d := a.needDoc()
	if d == nil {
		if a.view == viewHome {
			a.flash("open a note first (Ctrl-p finds one)")
		}
		return
	}
	if d.stdin {
		a.flash("read-only: this page came from stdin")
		return
	}
	src := d.page.SourceLine(d.cur)
	if src < 0 {
		src = 0
	}
	if external || a.cfg.External {
		a.runExternal(d.path, src+1)
		return
	}
	a.openEditorOn(d.path, src+1)
	if a.view == viewEditor && a.edit != nil && how != editNormal {
		a.edit.ed.StartInsert(how == editInsertEnd)
	}
}

// runExternal opens rel in $EDITOR at a 1-based line and reloads it after.
func (a *app) runExternal(rel string, line int) {
	cmd := a.cfg.EditorCommand(a.v.Abs(rel), line)
	err := a.host.RunExternal(cmd)
	a.scr.Invalidate()
	if a.doc != nil && a.doc.path == rel && !a.doc.stdin {
		a.reloadDoc(a.doc)
	} else if _, err := a.v.Refresh(rel); err == nil {
		a.gen++
	}
	if err != nil {
		a.flashErr("editor: %v", err)
		return
	}
	a.flash("back from the editor; reloaded %s", rel)
}

// openEditorOn opens a note in the built-in editor at a 1-based line (-1 =
// the last line).
func (a *app) openEditorOn(rel string, line int) {
	if a.cfg.External {
		if a.doc == nil || a.doc.path != rel {
			a.openNote(rel, line)
		}
		a.runExternal(rel, line)
		return
	}
	b, tok, err := a.v.ReadNote(rel)
	if err != nil {
		a.flashErr("cannot open %s: %v", rel, err)
		return
	}
	if binaryContent(b) {
		a.flashErr("%s is not a text file", rel)
		return
	}
	d := a.doc
	if d == nil || d.stdin || d.path != rel {
		nd, err := a.loadDoc(rel)
		if err != nil {
			a.flashErr("cannot open %s: %v", rel, err)
			return
		}
		d = nd
	}
	a.closePrompt()
	l := a.layout()
	ed := editor.New(editor.Config{
		Text:      b,
		Theme:     a.th,
		Glyphs:    a.gl,
		Measure:   a.measure,
		Bidi:      a.bidi,
		Number:    a.number,
		Completer: a.linkCompleter(),
		Clipboard: a.host.Copy,
		Now:       a.now,
		Width:     l.main.W,
		Height:    l.main.H - 1,
	})
	if line < 0 {
		line = ed.LineCount() // a fresh note: start at its end
	}
	ed.SetCursorLine(max(0, line-1))
	a.edit = &editSession{ed: ed, doc: d, tok: tok}
	a.doc = d
	a.view = viewEditor
	a.msg = message{} // a reader message ("/rete 3 of 9") means nothing here
	a.ctx.focus = ctxNone
	a.tree.focus = false
	a.keys.reset()
}

// startWriting puts the editor of a note that was just created in Insert
// mode on a fresh line after its content, one blank line below the title: a
// new note exists to be written in. The buffer is marked clean afterwards,
// so leaving without typing rewrites nothing.
func (a *app) startWriting() {
	if a.view != viewEditor || a.edit == nil {
		return
	}
	ed := a.edit.ed
	ed.SetCursor(ed.LineCount()-1, 0)
	// o Esc opens the line as its own undo step, which then counts as the
	// saved state; A resumes Insert mode on it.
	ed.HandleKey(term.KeyEvent{Key: term.KeyRune, Rune: 'o'})
	ed.HandleKey(term.KeyEvent{Key: term.KeyEscape})
	ed.MarkSaved()
	ed.HandleKey(term.KeyEvent{Key: term.KeyRune, Rune: 'A'})
}

func (a *app) editorRect() term.Rect {
	l := a.layout()
	return term.Rect{X: l.main.X, Y: l.main.Y + 1, W: l.main.W, H: l.main.H - 1}
}

func (a *app) drawEditor(term.Rect) {
	es := a.edit
	if es == nil {
		return
	}
	r := a.editorRect()
	if es.w != r.W || es.h != r.H {
		es.ed.Resize(r.W, r.H)
		es.w, es.h = r.W, r.H
	}
	cur := es.ed.Draw(a.scr, r)
	if len(a.overlays) == 0 && a.prompt == nil {
		a.placeCursor(cur)
	}
}

func (a *app) editorKey(k term.KeyEvent) {
	es := a.edit
	if es == nil {
		a.view = viewReader
		return
	}
	a.editorResult(es.ed.HandleKey(k))
}

// editorResult acts on what the editor asked for.
func (a *app) editorResult(res editor.Result) {
	es := a.edit
	if es == nil {
		return
	}
	if res.Message != "" {
		if res.Error {
			a.flashErr("%s", res.Message)
		} else {
			a.flash("%s", res.Message)
		}
	}
	quitFolio := a.firstEdit != "" && a.firstEdit == es.doc.path
	switch res.Action {
	case editor.ActionSave:
		a.saveEdit(func() {})
	case editor.ActionQuit:
		a.leaveEditor(false)
		if quitFolio {
			a.quit = true
		}
	case editor.ActionQuitDiscard:
		a.leaveEditor(true)
		if quitFolio {
			a.quit = true
		}
	case editor.ActionSaveQuit:
		a.saveIfDirty(func() {
			a.leaveEditor(false)
			if quitFolio {
				a.quit = true
			}
		})
	case editor.ActionLeave:
		// Autosave on leave (DESIGN.md §4.3): Esc in Normal mode saves a
		// dirty buffer, then returns to the reader.
		a.saveIfDirty(func() { a.leaveEditor(false) })
	case editor.ActionCommand:
		a.exCommand(res.Command, true)
	}
}

func (a *app) saveIfDirty(then func()) {
	if a.edit != nil && a.edit.ed.Dirty() {
		a.saveEdit(then)
		return
	}
	then()
}

// saveEdit writes the buffer, refusing to clobber a file that changed on
// disk since it was read: the user picks reload, overwrite or cancel.
func (a *app) saveEdit(then func()) {
	es := a.edit
	if es == nil {
		then()
		return
	}
	data := es.ed.Text()
	tok, err := a.v.WriteNote(es.doc.path, data, &es.tok)
	if err != nil {
		if errors.Is(err, vault.ErrConflict) {
			gone := false
			var ce *vault.ConflictError
			if errors.As(err, &ce) {
				gone = !ce.Actual.Exists
			}
			a.conflictPrompt(then, gone)
			return
		}
		a.flashErr("could not save %s: %v", es.doc.path, err)
		return
	}
	a.saved(tok, data)
	then()
}

func (a *app) saved(tok vault.StatToken, data []byte) {
	es := a.edit
	es.tok = tok
	es.ed.MarkSaved()
	es.doc.tok = tok
	es.doc.setSource(data)
	if n, ok := a.v.Note(es.doc.path); ok {
		es.doc.note = n
	}
	a.gen++ // titles, links and backlinks may have changed
	a.flash("saved %s", es.doc.path)
}

// conflictPrompt asks what to do when the file changed on disk.
func (a *app) conflictPrompt(then func(), gone bool) {
	es := a.edit
	what := "changed"
	if gone {
		what = "deleted"
	}
	a.pushOverlay(&choice{
		title: "Changed on disk",
		lines: []string{
			es.doc.path + " was " + what + " by another program",
			"since you opened it here.",
		},
		options: []choiceOption{
			{key: "r", label: "reload theirs", run: func(a *app) {
				b, tok, err := a.v.ReadNote(es.doc.path)
				if err != nil {
					a.flashErr("cannot read %s: %v", es.doc.path, err)
					return
				}
				line := es.ed.CursorLine()
				a.edit = nil
				a.doc = es.doc
				es.doc.tok = tok
				es.doc.setSource(b)
				a.openEditorOn(es.doc.path, line+1)
				a.flash("reloaded %s; your edits were discarded", es.doc.path)
			}},
			{key: "o", label: "overwrite with mine", run: func(a *app) {
				data := es.ed.Text()
				tok, err := a.v.WriteNote(es.doc.path, data, nil)
				if err != nil {
					a.flashErr("could not save %s: %v", es.doc.path, err)
					return
				}
				a.saved(tok, data)
				then()
			}},
			{key: "c", label: "cancel", run: func(a *app) {
				a.flash("not saved; still editing")
			}},
		},
	})
}

// leaveEditor returns to the reader on the block the editor cursor is in.
func (a *app) leaveEditor(discard bool) {
	es := a.edit
	if es == nil {
		return
	}
	line := es.ed.CursorLine()
	a.number = es.ed.Number()
	if m := es.ed.Measure(); m != a.measure && m >= 20 {
		a.measure = m // :set wrap=N in the editor applies to the page too
		a.gen++
	}
	a.edit = nil
	d := es.doc
	if discard {
		if b, tok, err := a.v.ReadNote(d.path); err == nil {
			d.tok = tok
			d.setSource(b)
		}
	} else if !es.ed.Dirty() {
		d.setSource(es.ed.Text())
	}
	a.doc = d
	a.view = viewReader
	d.page = nil
	d.wantSet, d.want, d.wantOff = true, line, -1
	a.ensurePage(d)
	a.remember()
	a.scr.SetCursorShape(term.CursorDefault)
}

// leaveEditorThen runs fn after leaving the editor (saving first when the
// buffer is dirty), or right away in the reader.
func (a *app) leaveEditorThen(fn func()) {
	if a.view != viewEditor || a.edit == nil {
		fn()
		return
	}
	a.saveIfDirty(func() {
		a.leaveEditor(false)
		fn()
	})
}

// --- [[ completer ------------------------------------------------------------------

// linkCompleter feeds the editor's [[ popup from the index: matched on
// title, path and aliases; an empty query lists recent notes.
func (a *app) linkCompleter() editor.Completer {
	type cand struct {
		c   editor.Candidate
		key string
		rk  int
	}
	var cands []cand
	built := -1
	build := func() {
		notes := a.v.Notes()
		base := map[string]int{}
		for _, n := range notes {
			base[strings.ToLower(noteName(n.Path))]++
		}
		rank := map[string]int{}
		for i, n := range a.v.RecentNotes() {
			rank[n.Path] = i
		}
		cands = cands[:0]
		for _, n := range notes {
			target := noteName(n.Path)
			if base[strings.ToLower(target)] > 1 {
				target = strings.TrimSuffix(n.Path, ".md")
			}
			title := n.Title
			if title == "" {
				title = noteName(n.Path)
			}
			key := title + "  " + n.Path + "  " + strings.Join(n.Aliases, "  ")
			cands = append(cands, cand{c: editor.Candidate{Target: target, Label: title, Detail: path.Dir(n.Path)}, key: key, rk: rank[n.Path]})
		}
		for i := range cands {
			if cands[i].c.Detail == "." {
				cands[i].c.Detail = ""
			}
		}
		built = a.v.Len()
	}
	return func(q string) []editor.Candidate {
		if built != a.v.Len() {
			build()
		}
		type scored struct {
			c     *cand
			score int
		}
		var ss []scored
		if q == "" {
			for i := range cands {
				ss = append(ss, scored{&cands[i], -cands[i].rk})
			}
		} else {
			m := text.NewMatcher(q)
			for i := range cands {
				if r, ok := m.Match(cands[i].key); ok {
					ss = append(ss, scored{&cands[i], r.Score})
				}
			}
		}
		sort.SliceStable(ss, func(i, j int) bool {
			if ss[i].score != ss[j].score {
				return ss[i].score > ss[j].score
			}
			return ss[i].c.rk < ss[j].c.rk
		})
		var out []editor.Candidate
		for _, s := range ss {
			out = append(out, s.c.c)
			if len(out) == 50 {
				break
			}
		}
		return out
	}
}
