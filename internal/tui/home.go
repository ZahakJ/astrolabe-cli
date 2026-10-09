package tui

import (
	"os"

	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
)

// homeState is the selection on the home screen's recent-notes list.
type homeState struct {
	sel   int
	items []string // vault-relative paths shown, most recent first
	rows  []int    // screen row of each item at the last draw
	x0    int
}

// homeRecent lists up to n recent notes: visited ones from the recent file
// first, then the most recently modified.
func (a *app) homeRecent(n int) []string {
	var out []string
	seen := map[string]bool{}
	if a.recent != nil {
		for _, e := range a.recent.Entries() {
			rel, err := a.v.Rel(e.Path)
			if err != nil || seen[rel] {
				continue
			}
			if fi, err := os.Stat(e.Path); err != nil || !fi.Mode().IsRegular() {
				continue
			}
			seen[rel] = true
			out = append(out, rel)
			if len(out) == n {
				return out
			}
		}
	}
	for _, note := range a.v.RecentNotes() {
		if seen[note.Path] {
			continue
		}
		seen[note.Path] = true
		out = append(out, note.Path)
		if len(out) == n {
			break
		}
	}
	return out
}

// homeLegend is the two-column key legend of the home screen.
var homeLegend = [][2]string{
	{"ctrl+p", "find a note"}, {"space /", "search text"},
	{"space d", "today"}, {"space c", "capture"},
	{"space n", "new note"}, {"space t", "agenda"},
	{"space e", "file tree"}, {"?", "all keys"},
	{":", "commands"}, {"q", "quit"},
}

func (a *app) drawHome(r term.Rect) {
	s := a.scr
	h := &a.home
	maxRecent := 6
	if r.H < 24 {
		maxRecent = max(0, r.H-18)
	}
	h.items = a.homeRecent(maxRecent)
	if h.sel >= len(h.items) {
		h.sel = max(0, len(h.items)-1)
	}
	ascii := a.gl.Name == "ascii"
	legendRows := (len(homeLegend) + 1) / 2
	colW := 26
	twoCols := r.W >= colW*2+4
	if !twoCols {
		legendRows = len(homeLegend)
	}
	// Height of the whole block, to centre it vertically.
	blockH := 4 + 2 + legendRows
	if len(h.items) > 0 {
		blockH += 2 + len(h.items)
	}
	showLegend := true
	if blockH > r.H-1 {
		showLegend = false
		blockH -= legendRows + 1
	}
	// The Astrolabe mark replaces the lone ✦ when there is room for it
	// after the recent list and the legend.
	logo := logoArt.rows
	logoCls := logoArt.classes
	if ascii {
		logo, logoCls = logoASCII.rows, logoASCII.classes
	}
	showLogo := blockH+len(logo)+1 <= r.H-2 && r.W >= 24
	if showLogo {
		blockH += len(logo) // replaces the one-row brand, plus its gap
	}
	y := r.Y + max(1, (r.H-blockH)/2)
	center := func(str string, st theme.Style) {
		w := text.Width(str)
		s.PutStringClip(r.X+max(0, (r.W-w)/2), y, str, st, r.X+r.W)
		y++
	}
	if showLogo {
		lw := text.Width(logo[0])
		x0 := r.X + max(0, (r.W-lw)/2)
		ring := a.st.accent
		pin := theme.Style{FG: a.th.Link, BG: ring.BG}
		for i, row := range logo {
			x := x0
			cls := logoCls[i]
			k := 0
			for _, ch := range row {
				st := ring
				if k < len(cls) && cls[k] == 'c' {
					st = pin
				}
				if ch != ' ' {
					s.SetCell(x, y, string(ch), st)
				}
				x++
				k++
			}
			y++
		}
	} else {
		center(a.gl.Brand, a.st.accent.With(boldAttr))
	}
	y++
	center("The vault is open.", a.st.heading)
	count := plural(a.v.Len(), "note", "notes")
	if !a.v.Scanned() {
		count += " so far " + a.gl.Ellipsis
	}
	center(count+"  "+a.gl.Dot+"  "+a.dispS(a.vaultPath()), a.st.muted)
	y++

	blockW := colW * 2
	if !twoCols {
		blockW = colW
	}
	blockW = min(blockW, r.W-4)
	x0 := r.X + max(2, (r.W-blockW)/2)
	h.x0 = x0
	h.rows = h.rows[:0]
	if len(h.items) > 0 {
		s.PutStringClip(x0, y, "Recent", a.st.muted.With(boldAttr), r.X+r.W)
		y++
		for i, rel := range h.items {
			title := noteName(rel)
			if n, ok := a.v.Note(rel); ok && n.Title != "" {
				title = n.Title
			}
			st := a.st.base
			if i == h.sel {
				s.SetCell(x0-2, y, a.gl.Bar, a.st.cursor)
				st = a.st.accent
			}
			dir := pathDir(rel)
			tw := blockW
			if dir != "" {
				tw = blockW - min(text.Width(dir), blockW/2) - 2
			}
			s.PutStringClip(x0, y, a.dispFit(title, tw), st, x0+tw)
			if dir != "" {
				dw := min(text.Width(dir), blockW/2)
				s.PutStringClip(x0+blockW-dw, y, text.TruncateLeft(a.dispPath(dir), dw, a.gl.Ellipsis), a.st.faint, x0+blockW)
			}
			h.rows = append(h.rows, y)
			y++
		}
		y++
	}
	if !showLegend {
		return
	}
	for i, kv := range homeLegend {
		col, row := 0, i
		if twoCols {
			col, row = i%2, i/2
		}
		x := x0 + col*colW
		yy := y + row
		k := keyDisplay(kv[0], ascii)
		kx := s.PutStringClip(x, yy, k, a.st.accent, r.X+r.W)
		s.PutStringClip(max(kx+1, x+9), yy, kv[1], a.st.muted, min(r.X+r.W, x+colW))
	}
}

func pathDir(rel string) string {
	for i := len(rel) - 1; i >= 0; i-- {
		if rel[i] == '/' {
			return rel[:i]
		}
	}
	return ""
}

// homeKey handles the home screen's own keys: moving over and opening the
// recent notes.
func (a *app) homeKey(k term.KeyEvent) bool {
	h := &a.home
	switch k.String() {
	case "j", "down":
		if h.sel < len(h.items)-1 {
			h.sel++
		}
	case "k", "up":
		if h.sel > 0 {
			h.sel--
		}
	case "enter", "l":
		if h.sel < len(h.items) {
			a.openNote(h.items[h.sel], 0)
		}
	default:
		return false
	}
	return true
}

func (a *app) homeMouse(m term.MouseEvent) {
	if m.Button != term.MouseLeft || m.Action != term.MousePress {
		return
	}
	for i, y := range a.home.rows {
		if y == m.Y && m.X >= a.home.x0-2 {
			a.home.sel = i
			a.openNote(a.home.items[i], 0)
			return
		}
	}
}
