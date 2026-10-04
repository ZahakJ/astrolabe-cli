package tui

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ZahakJ/folio/internal/export"
	"github.com/ZahakJ/folio/internal/term"
	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/theme"
	"github.com/ZahakJ/folio/internal/vault"
)

// cmdPrompt is the reader's one-line ":" command line or "/" search line,
// drawn in place of the status bar.
type cmdPrompt struct {
	kind rune // ':' or '/'
	in   lineInput

	// Tab completion state.
	comp     []string
	compIdx  int
	compBase string

	// The search state to restore when "/" is cancelled.
	prevSearch       string
	prevCur, prevTop int
}

func (a *app) openPrompt(kind rune) {
	p := &cmdPrompt{kind: kind}
	if kind == '/' {
		d := a.needDoc()
		if d == nil {
			return
		}
		p.prevSearch, p.prevCur, p.prevTop = d.searchStr, d.cur, d.top
	}
	a.prompt = p
}

func (a *app) closePrompt() { a.prompt = nil }

func (a *app) promptKey(k term.KeyEvent) {
	p := a.prompt
	ks := k.String()
	switch ks {
	case "esc", "ctrl+c":
		if p.kind == '/' {
			if d := a.needDoc(); d != nil {
				a.startSearch(p.prevSearch, false)
				d.cur, d.top = p.prevCur, p.prevTop
				a.clampView(d)
			}
		}
		a.closePrompt()
		return
	case "enter":
		a.closePrompt()
		if p.kind == '/' {
			if p.in.text == "" {
				// Like Vim: an empty pattern repeats the last search.
				if d := a.needDoc(); d != nil && p.prevSearch != "" {
					a.startSearch(p.prevSearch, true)
				}
				return
			}
			if d := a.needDoc(); d != nil {
				d.cur, d.top = p.prevCur, p.prevTop
			}
			a.startSearch(p.in.text, true)
			return
		}
		a.runCommand(p.in.text)
		return
	case "tab", "shift+tab":
		if p.kind == ':' {
			a.complete(ks == "tab")
		}
		return
	case "backspace":
		if p.in.text == "" {
			ks = "esc"
			a.promptKey(term.KeyEvent{Key: term.KeyEscape})
			return
		}
	}
	if handled, changed := p.in.handleKey(k); handled {
		p.comp = nil
		if changed {
			a.promptChanged()
		}
	}
}

// promptChanged runs incremental search while "/" is typed.
func (a *app) promptChanged() {
	p := a.prompt
	if p == nil || p.kind != '/' {
		return
	}
	d := a.needDoc()
	if d == nil {
		return
	}
	d.cur, d.top = p.prevCur, p.prevTop
	a.startSearch(p.in.text, false)
	if len(d.matches) > 0 {
		m := d.matches[d.mi]
		d.cur = m.line
		a.clampView(d)
	}
}

func (a *app) drawPrompt(y int) {
	s := a.scr
	p := a.prompt
	st := a.st.status
	st.FG = a.th.Text
	x := s.PutString(1, y, string(p.kind), a.st.sAccent)
	hint := ""
	if p.kind == '/' {
		if d := a.doc; d != nil && p.in.text != "" {
			if len(d.matches) == 0 {
				hint = "no match"
			} else {
				hint = itoa(d.mi+1) + "/" + itoa(len(d.matches))
			}
		}
	} else if len(p.comp) > 1 {
		hint = "Tab " + itoa(p.compIdx+1) + "/" + itoa(len(p.comp))
	}
	right := a.w - 1
	if hint != "" {
		right -= text.Width(hint) + 1
		s.PutString(right+1, y, hint, a.st.sFaint)
		right -= 1
	}
	cx := p.in.draw(s, x, y, right-x, st)
	s.SetCursor(cx, y)
	s.SetCursorShape(term.CursorBar)
	s.ShowCursor(true)
}

// --- completion -------------------------------------------------------------------

var exCommands = []string{"e", "edit", "export", "help", "new", "noh", "q", "quit", "set", "theme", "today", "w", "wq", "x"}

func (a *app) complete(forward bool) {
	p := a.prompt
	if p.comp == nil {
		t := p.in.text
		cut := strings.LastIndex(t, " ") + 1
		base, arg := t[:cut], t[cut:]
		cmd := strings.Fields(t)
		switch {
		case cut == 0:
			for _, c := range exCommands {
				if strings.HasPrefix(c, arg) {
					p.comp = append(p.comp, c)
				}
			}
		case len(cmd) >= 1 && (cmd[0] == "e" || cmd[0] == "edit"):
			p.comp = a.noteCompletions(strings.TrimSpace(t[len(cmd[0]):]))
			base = cmd[0] + " "
		case len(cmd) >= 1 && cmd[0] == "theme":
			for _, n := range theme.Names() {
				if strings.HasPrefix(n, arg) {
					p.comp = append(p.comp, n)
				}
			}
		case len(cmd) >= 1 && cmd[0] == "set":
			for _, o := range []string{"wrap="} {
				if strings.HasPrefix(o, arg) {
					p.comp = append(p.comp, o)
				}
			}
		}
		if len(p.comp) == 0 {
			p.comp = nil
			a.flash("no completions")
			return
		}
		p.compBase = base
		p.compIdx = 0
		if !forward {
			p.compIdx = len(p.comp) - 1
		}
	} else {
		d := 1
		if !forward {
			d = -1
		}
		p.compIdx = (p.compIdx + d + len(p.comp)) % len(p.comp)
	}
	p.in.set(p.compBase + p.comp[p.compIdx])
}

// noteCompletions lists note names (paths without .md) starting with
// prefix, or whose base name does, shortest first.
func (a *app) noteCompletions(prefix string) []string {
	lp := strings.ToLower(prefix)
	type cand struct {
		name string
		rank int
	}
	var cs []cand
	for _, n := range a.v.Notes() {
		name := strings.TrimSuffix(n.Path, ".md")
		ln := strings.ToLower(name)
		switch {
		case strings.HasPrefix(ln, lp):
			cs = append(cs, cand{name, 0})
		case strings.HasPrefix(strings.ToLower(path.Base(name)), lp):
			cs = append(cs, cand{name, 1})
		case lp != "" && strings.Contains(ln, lp):
			cs = append(cs, cand{name, 2})
		}
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].rank != cs[j].rank {
			return cs[i].rank < cs[j].rank
		}
		if len(cs[i].name) != len(cs[j].name) {
			return len(cs[i].name) < len(cs[j].name)
		}
		return cs[i].name < cs[j].name
	})
	var out []string
	for _, c := range cs {
		out = append(out, c.name)
		if len(out) == 100 {
			break
		}
	}
	return out
}

// --- ex commands --------------------------------------------------------------------

// runCommand executes a ":" command typed in the reader, or passed on by the
// editor (fromEditor reports the latter).
func (a *app) runCommand(cmdline string) { a.exCommand(cmdline, false) }

func (a *app) exCommand(cmdline string, fromEditor bool) {
	cmdline = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cmdline), ":"))
	if cmdline == "" {
		return
	}
	cmd, arg := cmdline, ""
	if i := strings.IndexAny(cmdline, " \t"); i >= 0 {
		cmd, arg = cmdline[:i], strings.TrimSpace(cmdline[i+1:])
	}
	if n, err := strconv.Atoi(cmd); err == nil && !fromEditor {
		if a.needDoc() != nil {
			a.pushHistory()
			a.gotoSource(n - 1)
		}
		return
	}
	switch cmd {
	case "q", "quit", "q!", "quit!", "qa", "qa!", "wq", "x", "xit", "wqa", "xa":
		if fromEditor {
			a.flashErr("unknown command: %s", cmdline)
			return
		}
		a.requestQuit()
	case "w", "write", "w!":
		if fromEditor {
			a.flashErr(":w FILE is not supported; folio saves the note in place")
			return
		}
		a.flash("nothing to write: the reader never changes the file (i edits)")
	case "e", "edit", "e!", "o", "open":
		if arg == "" {
			if !fromEditor && a.doc != nil && !a.doc.stdin {
				a.reloadDoc(a.doc)
				a.flash("reloaded %s", a.doc.path)
			}
			return
		}
		rel, err := a.resolveTyped(arg)
		if err != nil {
			a.flashErr("%v", err)
			return
		}
		a.leaveEditorThen(func() { a.openNote(rel, 0) })
	case "new", "enew":
		if arg == "" {
			a.openNewNote()
			return
		}
		a.leaveEditorThen(func() { a.createAndEdit(arg) })
	case "today", "daily":
		a.leaveEditorThen(a.openToday)
	case "theme", "colorscheme", "colo":
		if arg == "" {
			a.flash("theme %s (one of %s)", a.base.Name, strings.Join(theme.Names(), ", "))
			return
		}
		t, ok := theme.Lookup(arg)
		if !ok {
			a.flashErr("no theme %q (one of %s)", arg, strings.Join(theme.Names(), ", "))
			return
		}
		a.setTheme(t)
	case "export":
		a.exportCurrent(arg)
	case "set", "se":
		a.setOption(arg)
	case "help", "h":
		a.pushOverlay(newHelp(a))
	case "noh", "nohlsearch":
		if a.doc != nil {
			a.doc.search, a.doc.searchStr, a.doc.matches = nil, "", nil
		}
	case "tags":
		a.openTags()
	case "agenda", "tasks":
		a.openAgenda()
	default:
		a.flashErr("unknown command: %s", cmd)
	}
}

// resolveTyped resolves a note name typed by the user: a vault path or name
// resolved like a wikilink, else a unique fuzzy match.
func (a *app) resolveTyped(name string) (string, error) {
	if rel, ok := a.v.ResolveName(name); ok {
		return rel, nil
	}
	notes := a.v.Notes()
	cands := make([]string, len(notes))
	for i, n := range notes {
		cands[i] = n.Title + " " + n.Path
	}
	r := text.Rank(name, cands)
	switch {
	case len(r) == 1:
		return notes[r[0].Index].Path, nil
	case len(r) > 1:
		// A clear winner is accepted; otherwise list a few.
		if r[0].Score > r[1].Score*2 {
			return notes[r[0].Index].Path, nil
		}
		var names []string
		for _, x := range r[:min(3, len(r))] {
			names = append(names, noteName(notes[x.Index].Path))
		}
		return "", errorf("%q is ambiguous: %s%s", name, strings.Join(names, ", "), a.gl.Ellipsis)
	}
	return "", errorf("no note matches %q", name)
}

// setOption handles ":set".
func (a *app) setOption(arg string) {
	key, val, _ := strings.Cut(arg, "=")
	switch strings.TrimSpace(key) {
	case "wrap", "measure", "tw", "textwidth":
		n, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil || n < 20 || n > 400 {
			a.flashErr("wrap needs a width of 20 or more")
			return
		}
		a.measure = n
		a.gen++
		if a.edit != nil {
			a.edit.ed.SetMeasure(n)
		}
		a.flash("measure %d", n)
	case "nu", "number", "nonu", "nonumber", "nu!", "number!":
		k := strings.TrimSpace(key)
		switch {
		case strings.HasSuffix(k, "!"):
			a.number = !a.number
		default:
			a.number = !strings.HasPrefix(k, "no")
		}
		if a.edit != nil {
			a.edit.ed.SetNumber(a.number)
		}
		if a.number {
			a.flash("line numbers on in the editor")
		} else {
			a.flash("line numbers off in the editor")
		}
	case "":
		a.flash("measure %d, theme %s", a.measure, a.base.Name)
	default:
		a.flashErr("unknown option: %s", key)
	}
}

// setTheme switches the theme and persists it in the config file.
func (a *app) setTheme(t theme.Theme) {
	a.base = t
	a.applyTheme()
	a.scr.Invalidate()
	if a.cfg.ConfigFile == "" {
		a.flash("theme %s", t.Name)
		return
	}
	if err := vault.SetConfigValue(a.cfg.ConfigFile, "theme", t.Name); err != nil {
		a.flashErr("theme %s (not saved: %v)", t.Name, err)
		return
	}
	a.flash("theme %s", t.Name)
}

func (a *app) cycleTheme() {
	t, ok := theme.Lookup(theme.Next(a.base.Name))
	if !ok {
		t = theme.DefaultTheme()
	}
	a.setTheme(t)
}

// exportCurrent writes the current note as HTML: to FILE when given, else
// to "<name>.html" in the working directory (never silently into the vault
// folder of the note).
func (a *app) exportCurrent(file string) {
	d := a.doc
	if a.view == viewEditor && a.edit != nil {
		d = a.edit.doc
	}
	if d == nil {
		a.flash("no note open")
		return
	}
	if file == "" {
		name := d.name
		if d.stdin {
			name = "stdin"
		}
		file = name + ".html"
	}
	if strings.HasPrefix(file, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			file = filepath.Join(home, file[2:])
		}
	}
	if !filepath.IsAbs(file) {
		dir := a.cfg.Cwd
		if dir == "" {
			dir, _ = os.Getwd()
		}
		file = filepath.Join(dir, file)
	}
	src := d.src
	if a.view == viewEditor && a.edit != nil {
		src = a.edit.ed.Text()
	}
	opt := export.Options{Path: d.path, Title: d.name, Resolver: &resolver{v: a.v, from: d.path}, Today: a.now(), Generator: "folio"}
	if !d.stdin {
		opt.Backlinks = a.v.BacklinkCount(d.path)
	}
	html := export.HTML(parseBytes(src), opt)
	if _, err := vault.WriteFile(file, []byte(html), nil); err != nil {
		a.flashErr("export: %v", err)
		return
	}
	a.flash("exported %s", file)
}
