package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ZahakJ/astrolabe-cli/internal/md"
	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/vault"
)

// writeJSON prints v as indented JSON on stdout.
func (a *app) writeJSON(v any) error {
	enc := json.NewEncoder(a.env.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// nothing is the "nothing found" outcome: exit 1, with the message shown
// only when stderr is a terminal so that scripts stay quiet.
func (a *app) nothing(format string, args ...any) error {
	if a.env.StderrTTY {
		return &notFound{msg: fmt.Sprintf(format, args...)}
	}
	return &notFound{}
}

// requireRoot fails with a clear message when the vault does not exist yet
// (reading verbs have nothing to read).
func (a *app) requireRoot(v *vault.Vault) error {
	if fi, err := os.Stat(v.Root()); err != nil || !fi.IsDir() {
		return a.nothing("no vault at %s yet ('astrolabe add' or 'astrolabe new' creates it; -C DIR picks another)", v.Root())
	}
	return nil
}

type findJSON struct {
	Path    string   `json:"path"`
	Abs     string   `json:"abs"`
	Title   string   `json:"title"`
	Line    int      `json:"line"`
	Text    string   `json:"text"`
	Matches [][2]int `json:"matches"`
	Kind    string   `json:"kind"`
}

// cmdFind is `astrolabe find QUERY…`: full-text search.
func (a *app) cmdFind(args []string) error {
	fs := newFlagSet("find")
	files := fs.Bool("-l", "--files")
	asJSON := fs.Bool("", "--json")
	limit := fs.Int("-n", "--limit", 0)
	pos, err := fs.Parse(args)
	if err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(pos, " "))
	if query == "" {
		return usagef("find", "nothing to search for")
	}
	q, err := vault.ParseQuery(query)
	if err != nil {
		return usagef("find", "%v", err)
	}
	if q.Empty() {
		return usagef("find", "nothing to search for")
	}
	v, err := a.vault()
	if err != nil {
		return err
	}
	if err := a.requireRoot(v); err != nil {
		return err
	}
	if q.Tags != nil || q.Titles != nil {
		// Tag and title filters read the index.
		if v, err = a.scannedVault(); err != nil {
			return err
		}
	}
	res, err := v.SearchQuery(context.Background(), q, vault.SearchOptions{Limit: *limit})
	if err != nil {
		return err
	}
	if len(res) == 0 {
		if *asJSON {
			_ = a.writeJSON([]findJSON{})
		}
		return a.nothing("no notes match %q", query)
	}
	switch {
	case *asJSON:
		out := make([]findJSON, 0, len(res))
		for _, r := range res {
			m := r.Matches
			if m == nil {
				m = [][2]int{}
			}
			out = append(out, findJSON{Path: r.Path, Abs: v.Abs(r.Path), Title: r.Title, Line: r.Line,
				Text: r.Text, Matches: m, Kind: r.Class.String()})
		}
		return a.writeJSON(out)
	case *files:
		seen := map[string]bool{}
		for _, r := range res {
			if !seen[r.Path] {
				seen[r.Path] = true
				a.out("%s\n", a.shownPath(r.Path))
			}
		}
		return nil
	case !a.human():
		for _, r := range res {
			a.out("%s:%d:%s\n", a.openablePath(r.Path), max(1, r.Line), oneLine(r.Text))
		}
		return nil
	}
	a.printFindHuman(res)
	return nil
}

// oneLine makes a result line safe for line-oriented output.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, s)
}

// printFindHuman prints results grouped by note, ripgrep-style, with the
// matches highlighted.
func (a *app) printFindHuman(res []vault.Result) {
	p := a.painter()
	var order []string
	groups := map[string][]vault.Result{}
	for _, r := range res {
		if _, ok := groups[r.Path]; !ok {
			order = append(order, r.Path)
		}
		groups[r.Path] = append(groups[r.Path], r)
	}
	hits := 0
	for gi, path := range order {
		g := groups[path]
		if gi > 0 {
			a.out("\n")
		}
		hdr := p.pathInk(path)
		if t := g[0].Title; t != "" && t != baseTitle(path) {
			hdr += "  " + p.muted(p.visual(text.Truncate(t, max(10, p.width-text.Width(path)-2), p.g.Ellipsis)))
		}
		a.out("%s\n", hdr)
		numW := 1
		for _, r := range g {
			numW = max(numW, len(fmt.Sprint(r.Line)))
		}
		for _, r := range g {
			hits++
			num := "-"
			if r.Line > 0 {
				num = fmt.Sprint(r.Line)
			}
			prefix := "  " + p.faint(text.PadLeft(num, numW)) + " " + p.faint(p.g.VLine) + " "
			avail := p.width - (2 + numW + 1 + text.Width(p.g.VLine) + 1)
			// The line as a reader sees it, matches carried across
			// (piped output keeps the raw path:line:text form).
			txt, ms := md.CleanLine(r.Text, r.Matches)
			body := p.highlight(txt, ms, avail)
			if r.Class == vault.RankTitle || r.Class == vault.RankHeading {
				body = p.highlightWith(txt, ms, avail, p.heading)
			}
			a.out("%s%s\n", prefix, body)
		}
	}
	a.out("\n%s\n", p.faint(fmt.Sprintf("%s in %s", plural(hits, "match", "matches"), plural(len(order), "note", "notes"))))
}

// baseTitle is the file name of a vault path without ".md".
func baseTitle(rel string) string {
	_, name := splitPath(rel)
	return strings.TrimSuffix(name, ".md")
}

// highlight renders a result line within avail cells with matches styled.
func (p *painter) highlight(s string, matches [][2]int, avail int) string {
	return p.highlightWith(s, matches, avail, p.text)
}

func (p *painter) highlightWith(s string, matches [][2]int, avail int, ink func(string) string) string {
	// Tabs and control characters become spaces (byte length kept, so the
	// match offsets stay valid).
	b := []byte(s)
	for i, c := range b {
		if c < 0x20 || c == 0x7f {
			b[i] = ' '
		}
	}
	s = string(b)
	if p.human && p.bidi && text.HasRTL(s) {
		// Shaped and in visual order, as everywhere else (DESIGN.md §6):
		// cut in logical order first (so the start of the sentence stays),
		// then reorder with the matches carried along.
		lead := len(s) - len(strings.TrimLeft(s, " "))
		t := text.Truncate(strings.TrimSpace(s), avail, p.g.Ellipsis)
		keep := len(t)
		if strings.HasSuffix(t, p.g.Ellipsis) && !strings.HasSuffix(strings.TrimSpace(s), p.g.Ellipsis) {
			keep -= len(p.g.Ellipsis)
		}
		var ms [][2]int
		for _, m := range matches {
			a, z := max(m[0]-lead, 0), min(m[1]-lead, keep)
			if a < z {
				ms = append(ms, [2]int{a, z})
			}
		}
		vis, vms := text.VisualRanges(t, ms)
		var out strings.Builder
		pos := 0
		for _, m := range vms {
			out.WriteString(ink(vis[pos:m[0]]))
			out.WriteString(p.match(vis[m[0]:m[1]]))
			pos = m[1]
		}
		out.WriteString(ink(vis[pos:]))
		return out.String()
	}
	// Drop leading indentation.
	start := len(s) - len(strings.TrimLeft(s, " "))
	end := len(strings.TrimRight(s, " "))
	if end < start {
		end = start
	}
	ell := p.g.Ellipsis
	ellW := text.Width(ell)
	lead := false
	if avail > 0 && text.Width(s[start:end]) > avail && len(matches) > 0 {
		// Keep the first match in view with a little context before it.
		m0 := matches[0][0]
		if m0 > start && text.Width(s[start:m0]) > avail/3 {
			want := avail / 4
			cut := m0
			for _, g := range text.Graphemes(s[start:m0]) {
				if text.Width(s[start+g.Offset:m0]) <= want {
					cut = start + g.Offset
					break
				}
			}
			start, lead = cut, true
		}
	}
	room := avail
	if lead {
		room -= ellW
	}
	seg := s[start:end]
	vis := seg
	cutRight := false
	if room > 0 && text.Width(seg) > room {
		t := text.Truncate(seg, room, ell)
		if strings.HasSuffix(t, ell) && t != seg {
			vis, cutRight = t[:len(t)-len(ell)], true
		} else {
			vis = t
		}
	}
	visEnd := start + len(vis)
	var out strings.Builder
	if lead {
		out.WriteString(p.faint(ell))
	}
	pos := start
	for _, m := range matches {
		ms, me := max(m[0], start), min(m[1], visEnd)
		if ms >= me {
			continue
		}
		if ms > pos {
			out.WriteString(ink(s[pos:ms]))
		}
		out.WriteString(p.match(s[ms:me]))
		pos = me
	}
	if pos < visEnd {
		out.WriteString(ink(s[pos:visEnd]))
	}
	if cutRight {
		out.WriteString(p.faint(ell))
	}
	return out.String()
}

type noteJSON struct {
	Path     string   `json:"path"`
	Abs      string   `json:"abs"`
	Title    string   `json:"title"`
	Aliases  []string `json:"aliases"`
	Tags     []string `json:"tags"`
	Modified string   `json:"modified"`
	Words    int      `json:"words"`
	Size     int64    `json:"size"`
}

// cmdLs is `astrolabe ls [QUERY]`: notes, most recently modified first.
func (a *app) cmdLs(args []string) error {
	fs := newFlagSet("ls")
	tag := fs.String("", "--tag")
	files := fs.Bool("-l", "--files")
	asJSON := fs.Bool("", "--json")
	limit := fs.Int("-n", "--limit", 0)
	pos, err := fs.Parse(args)
	if err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(pos, " "))
	v, err := a.vault()
	if err != nil {
		return err
	}
	if err := a.requireRoot(v); err != nil {
		return err
	}
	if v, err = a.scannedVault(); err != nil {
		return err
	}
	notes := v.RecentNotes()
	if t := strings.TrimPrefix(strings.TrimSpace(*tag), "#"); t != "" {
		kept := notes[:0:0]
		for _, n := range notes {
			if n.HasTag(t) {
				kept = append(kept, n)
			}
		}
		notes = kept
	}
	if query != "" {
		match := map[*vault.Note]bool{}
		for _, c := range fuzzyNotes(notes, query) {
			match[c.note] = true
		}
		kept := notes[:0:0]
		for _, n := range notes {
			if match[n] {
				kept = append(kept, n)
			}
		}
		notes = kept
	}
	if *limit > 0 && len(notes) > *limit {
		notes = notes[:*limit]
	}
	if len(notes) == 0 {
		if *asJSON {
			_ = a.writeJSON([]noteJSON{})
		}
		switch {
		case v.Len() == 0:
			return a.nothing("the vault at %s has no notes", v.Root())
		case *tag != "":
			return a.nothing("no notes tagged #%s", strings.TrimPrefix(*tag, "#"))
		}
		return a.nothing("no notes match %q", query)
	}
	switch {
	case *asJSON:
		out := make([]noteJSON, 0, len(notes))
		for _, n := range notes {
			out = append(out, noteJSON{Path: n.Path, Abs: v.Abs(n.Path), Title: n.Title,
				Aliases: nonNil(n.Aliases), Tags: nonNil(n.Tags),
				Modified: n.ModTime.Format(time.RFC3339), Words: n.Words, Size: n.Size})
		}
		return a.writeJSON(out)
	case *files:
		for _, n := range notes {
			a.out("%s\n", a.shownPath(n.Path))
		}
		return nil
	case !a.human():
		for _, n := range notes {
			a.out("%s\t%s\t%s\n", a.openablePath(n.Path), n.ModTime.Local().Format("2006-01-02 15:04"), oneLine(n.Title))
		}
		return nil
	}
	p := a.painter()
	now := a.now()
	rows := make([][]cell, 0, len(notes))
	for _, n := range notes {
		rows = append(rows, []cell{
			{s: ago(n.ModTime, now), paint: p.faint, right: true},
			{s: p.visual(n.Title), paint: p.text},
			{s: n.Path, paint: p.pathInk},
		})
	}
	for _, l := range p.table(rows, 2, 2, p.width, 1) {
		a.out("%s\n", l)
	}
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
