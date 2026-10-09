package tui

import (
	"path"
	"strings"

	"github.com/ZahakJ/astrolabe-cli/internal/editor"
	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
	"github.com/ZahakJ/astrolabe-cli/internal/vault"
)

// wordsPerMinute sets the reading-time estimate.
const wordsPerMinute = 230

// drawStatus draws the status bar (DESIGN.md §4.2): breadcrumb on the left,
// words · reading time or line:col and the mode pill on the right; a
// transient message replaces the left side; the ":" and "/" lines replace
// the whole bar while typing.
func (a *app) drawStatus(y int) {
	s := a.scr
	s.Fill(term.Rect{X: 0, Y: y, W: a.w, H: 1}, " ", a.st.status)
	if a.view == viewEditor && a.edit != nil && a.edit.ed.CommandLineActive() && len(a.overlays) == 0 {
		cur := a.edit.ed.DrawCommandLine(s, term.Rect{X: 0, Y: y, W: a.w, H: 1})
		a.placeCursor(cur)
		return
	}
	if a.prompt != nil {
		a.drawPrompt(y)
		return
	}
	narrow := a.w < 60

	// Right side, drawn first so the left knows how much room it has.
	pillText, pillStyle := a.pill()
	pill := " " + pillText + " "
	x := a.w - text.Width(pill)
	s.PutString(x, y, pill, pillStyle)
	right := x
	if !narrow {
		info := a.statusInfo()
		// A message that would be cut by the info gets its room instead
		// (it lasts a few seconds; a path in it must stay readable).
		msgW := 0
		if a.msg.text != "" {
			msgW = text.Width(oneLine(a.msg.text)) + 3
		}
		if info != "" {
			iw := text.Width(info)
			if right-iw-2 > 20 && right-iw-2 >= msgW {
				right -= iw + 2
				s.PutString(right, y, info, a.st.sMuted)
			}
		}
		if !a.v.Scanned() && a.view != viewPick {
			ind := "indexing" + a.gl.Ellipsis
			iw := text.Width(ind)
			if right-iw-2 > 24 {
				right -= iw + 2
				s.PutString(right, y, ind, a.st.sFaint)
			}
		}
	}
	limit := right - 2

	// Left side: a transient message, or the breadcrumb.
	if a.msg.text != "" {
		st := a.st.status
		if a.msg.err {
			st = a.st.sDanger
		}
		// Messages often quote a note title or path, which may be Arabic.
		s.PutStringClip(1, y, a.dispMsg(oneLine(a.msg.text), limit-1), st, limit)
		return
	}
	a.drawCrumb(y, limit, narrow)
}

// oneLine flattens control characters out of a message.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// pill returns the mode pill's label and style.
func (a *app) pill() (string, theme.Style) {
	if a.view == viewEditor && a.edit != nil {
		switch a.edit.ed.Mode() {
		case editor.ModeInsert:
			return "INSERT", a.th.PillInsert
		case editor.ModeVisual:
			return "VISUAL", a.th.PillVisual
		case editor.ModeVisualLine:
			return "V-LINE", a.th.PillVisual
		}
		return "NORMAL", a.th.PillNormal
	}
	return "READ", a.th.PillRead
}

// statusInfo is the right-hand information: words · reading time in the
// reader, line:col in the editor.
func (a *app) statusInfo() string {
	dot := " " + a.gl.Dot + " "
	switch a.view {
	case viewEditor:
		st := a.edit.ed.Status()
		info := itoa(st.Line) + ":" + itoa(st.VCol)
		if st.Pending != "" {
			info = st.Pending + "   " + info
		}
		return info
	case viewReader:
		d := a.doc
		if d == nil {
			return ""
		}
		info := plural(d.words, "word", "words") + dot + readingTime(d.words)
		if pct := a.scrollPercent(); pct != "" {
			info += dot + pct
		}
		if d.stdin {
			info = "read-only" + dot + info
		}
		if a.keys.count > 0 || len(a.keys.pending) > 0 {
			p := ""
			if a.keys.count > 0 {
				p = itoa(a.keys.count)
			}
			for _, k := range a.keys.pending {
				p += keyName(k, a.gl.Name == "ascii")
			}
			info = p + "   " + info
		}
		return info
	case viewHome:
		return plural(a.v.Len(), "note", "notes")
	}
	return ""
}

// drawCrumb draws "✦ vault › folder › sub › Note title ●" in [1, limit).
// Below 60 columns only the note name is shown. Folders are the first to
// ellipsise, then the vault name goes; the note name stands last.
func (a *app) drawCrumb(y, limit int, narrow bool) {
	s := a.scr
	x := 1
	name, folders, dirty := a.crumbParts()
	name = a.dispS(name)
	for i := range folders {
		folders[i] = a.dispS(folders[i])
	}
	if !narrow {
		x = s.PutString(x, y, a.gl.Brand, a.st.sAccent)
		x++
	}
	dirtyW := 0
	if dirty {
		dirtyW = 2
	}
	sep := " " + a.gl.Crumb + " "
	nameW := text.Width(name)
	avail := limit - x - dirtyW
	if narrow {
		folders = nil
	} else if vn := a.crumbVault(); vn != "" {
		vn = a.dispS(vn)
		if vw := text.Width(vn) + text.Width(sep); vw+nameW <= avail {
			x = s.PutStringClip(x, y, vn, a.st.sMuted, limit)
			x = s.PutStringClip(x, y, sep, a.st.sFaint, limit)
			avail -= vw
		}
	}
	// Folders are the first to ellipsise; the note name stands last.
	var crumb []string
	for i := range folders {
		w := 0
		for _, f := range folders[i:] {
			w += text.Width(f) + text.Width(sep)
		}
		if w+nameW <= avail {
			crumb = folders[i:]
			if i > 0 {
				crumb = append([]string{a.gl.Ellipsis}, crumb...)
			}
			break
		}
	}
	if crumb == nil && len(folders) > 0 && text.Width(a.gl.Ellipsis+sep)+nameW <= avail {
		crumb = []string{a.gl.Ellipsis}
	}
	for _, f := range crumb {
		x = s.PutStringClip(x, y, f, a.st.sMuted, limit)
		x = s.PutStringClip(x, y, sep, a.st.sFaint, limit)
	}
	nst := a.st.status
	nst.FG = a.th.Text
	if nst.FG.IsDefault() {
		nst.Attrs |= theme.Bold
	}
	x = s.PutStringClip(x, y, text.Truncate(name, max(1, limit-x-dirtyW), a.gl.Ellipsis), nst, limit-dirtyW)
	if dirty {
		ds := a.st.status
		ds.FG = a.th.Accent
		s.PutString(x+1, y, a.gl.Dirty, ds)
	}
}

// crumbParts returns the note name, its folders and the dirty flag.
func (a *app) crumbParts() (name string, folders []string, dirty bool) {
	switch a.view {
	case viewHome:
		return a.vaultPath(), nil, false
	case viewEditor:
		if a.edit == nil {
			return "", nil, false
		}
		d := a.edit.doc
		return d.title(), splitFolders(d.path), a.edit.ed.Dirty()
	}
	d := a.doc
	if d == nil {
		return "", nil, false
	}
	if d.stdin {
		return "stdin", nil, false
	}
	return d.title(), splitFolders(d.path), false
}

// vaultPath is the vault as people say it: "~/notes", or the absolute path
// outside the home directory (the home screen).
func (a *app) vaultPath() string { return vault.DisplayPath(a.v.Root(), a.cfg.Home) }

// vaultName is the vault's short name for the status bar and the tree:
// vaultPath when short, else the folder's own name.
func (a *app) vaultName() string { return vault.ShortName(a.v.Root(), a.cfg.Home) }

// crumbVault is the vault name leading the breadcrumb of a note ("" for
// the home screen, which names the vault itself, and for stdin).
func (a *app) crumbVault() string {
	switch a.view {
	case viewReader:
		if a.doc == nil || a.doc.stdin {
			return ""
		}
	case viewEditor:
		if a.edit == nil {
			return ""
		}
	default:
		return ""
	}
	return a.vaultName()
}

func splitFolders(p string) []string {
	dir := path.Dir(p)
	if dir == "." || dir == "/" || dir == "" {
		return nil
	}
	return strings.Split(dir, "/")
}

// placeCursor applies an editor cursor to the screen.
func (a *app) placeCursor(c editor.Cursor) {
	a.scr.SetCursor(c.X, c.Y)
	a.scr.SetCursorShape(c.Shape)
	a.scr.ShowCursor(c.Visible)
}

// readingTime is the reading time of n words at wordsPerMinute, rounded up:
// "1 min" … "59 min", then hours ("1 h", "54 h 49 min"), digits grouped
// like the word count.
func readingTime(words int) string {
	mins := (words + wordsPerMinute - 1) / wordsPerMinute
	if mins < 1 {
		mins = 1
	}
	if mins < 60 {
		return itoa(mins) + " min"
	}
	h, m := mins/60, mins%60
	out := fmtCount(h) + " h"
	if m > 0 {
		out += " " + itoa(m) + " min"
	}
	return out
}
