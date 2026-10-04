package cli

import (
	"fmt"
	"strings"

	"github.com/ZahakJ/astrolabe-cli/internal/vault"
)

type linkJSON struct {
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Kind    string `json:"kind"`
	Target  string `json:"target"`
	Heading string `json:"heading,omitempty"`
	Block   string `json:"block,omitempty"`
	Label   string `json:"label,omitempty"`
	Embed   bool   `json:"embed"`
	Broken  bool   `json:"broken"`
	Path    string `json:"path,omitempty"`
	Abs     string `json:"abs,omitempty"`
	ToLine  int    `json:"to_line,omitempty"`
}

// noteArg resolves the single NOTE argument of links/backlinks.
func (a *app) noteArg(verb string, pos []string) (string, *vault.Vault, error) {
	if len(pos) == 0 {
		return "", nil, usagef(verb, "which note? (astrolabe %s NOTE)", verb)
	}
	ref, err := a.resolveNote(strings.Join(pos, " "))
	if err != nil {
		return "", nil, err
	}
	v, err := a.scannedVault()
	if err != nil {
		return "", nil, err
	}
	if _, ok := v.Note(ref.Rel); !ok {
		if _, err := v.Refresh(ref.Rel); err != nil {
			return "", nil, fmt.Errorf("%s: %w", ref.Rel, err)
		}
	}
	return ref.Rel, v, nil
}

// cmdLinks is `astrolabe links NOTE`: outgoing links.
func (a *app) cmdLinks(args []string) error {
	fs := newFlagSet("links")
	files := fs.Bool("-l", "--files")
	asJSON := fs.Bool("", "--json")
	pos, err := fs.Parse(args)
	if err != nil {
		return err
	}
	rel, v, err := a.noteArg("links", pos)
	if err != nil {
		return err
	}
	n, _ := v.Note(rel)
	type resolved struct {
		l      vault.Link
		t      vault.Target
		ok     bool
		kind   string
		target string // display target as written
	}
	var ls []resolved
	for _, l := range n.Links {
		r := resolved{l: l}
		if l.Kind == vault.KindURL {
			r.kind = "url"
		} else {
			r.t, r.ok = v.ResolveLink(rel, l)
			r.kind = l.Kind.String()
			if !r.ok {
				r.kind = "broken"
			}
		}
		r.target = l.Target
		if l.Heading != "" {
			r.target += "#" + l.Heading
		} else if l.Block != "" {
			r.target += "#^" + l.Block
		}
		ls = append(ls, r)
	}
	if len(ls) == 0 {
		if *asJSON {
			_ = a.writeJSON([]linkJSON{})
		}
		return a.nothing("%s has no outgoing links", rel)
	}
	switch {
	case *asJSON:
		out := make([]linkJSON, 0, len(ls))
		for _, r := range ls {
			j := linkJSON{Line: r.l.Line, Col: r.l.Col, Kind: r.kind, Target: r.l.Target, Heading: r.l.Heading,
				Block: r.l.Block, Label: r.l.Label, Embed: r.l.Embed, Broken: r.kind == "broken"}
			if r.ok {
				j.Path, j.Abs, j.ToLine = r.t.Path, v.Abs(r.t.Path), r.t.Line
			}
			out = append(out, j)
		}
		return a.writeJSON(out)
	case *files:
		seen := map[string]bool{}
		for _, r := range ls {
			if r.ok && !seen[r.t.Path] {
				seen[r.t.Path] = true
				a.out("%s\n", a.shownPath(r.t.Path))
			}
		}
		if len(seen) == 0 {
			return a.nothing("%s links to no existing note", rel)
		}
		return nil
	case !a.human():
		for _, r := range ls {
			dest := r.target
			switch {
			case r.kind == "url":
				dest = r.l.Target
			case r.ok:
				dest = a.openablePath(r.t.Path)
			}
			a.out("%d\t%s\t%s\n", r.l.Line, r.kind, oneLine(dest))
		}
		return nil
	}
	p := a.painter()
	rows := make([][]cell, 0, len(ls))
	for _, r := range ls {
		label := r.l.Label
		if label == "" {
			label = r.target
		}
		var mark, dest string
		markInk, labelInk, destInk := p.accent, p.link, p.muted
		switch {
		case r.kind == "url":
			mark, dest = p.g.External, r.l.Target
			if label == r.l.Target {
				dest = ""
			}
		case r.ok:
			mark, dest = p.g.Crumb, r.t.Path
			destInk = p.pathInk
			if r.l.Embed {
				mark = p.g.Image
			}
			if r.t.AnchorMissing {
				dest += "  (no such heading)"
				destInk = p.danger
			}
		default:
			mark, dest = p.g.Callout("failure"), "missing"
			markInk, labelInk, destInk = p.danger, p.danger, p.danger
		}
		rows = append(rows, []cell{
			{s: fmt.Sprint(r.l.Line), paint: p.faint, right: true},
			{s: mark, paint: markInk},
			{s: p.visual(label), paint: labelInk},
			{s: dest, paint: destInk},
		})
	}
	a.out("%s %s\n", p.pathInk(rel), p.faint(fmt.Sprintf("%s %s", p.g.Dot, plural(len(ls), "link", "links"))))
	for _, l := range p.table(rows, 2, 1, p.width, 2) {
		a.out("%s\n", l)
	}
	return nil
}

type backlinkJSON struct {
	Path    string `json:"path"`
	Abs     string `json:"abs"`
	Title   string `json:"title"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Kind    string `json:"kind"`
	Context string `json:"context"`
}

// cmdBacklinks is `astrolabe backlinks NOTE`: incoming links with context.
func (a *app) cmdBacklinks(args []string) error {
	fs := newFlagSet("backlinks")
	files := fs.Bool("-l", "--files")
	asJSON := fs.Bool("", "--json")
	pos, err := fs.Parse(args)
	if err != nil {
		return err
	}
	rel, v, err := a.noteArg("backlinks", pos)
	if err != nil {
		return err
	}
	bl := v.Backlinks(rel)
	if len(bl) == 0 {
		if *asJSON {
			_ = a.writeJSON([]backlinkJSON{})
		}
		return a.nothing("no notes link to %s", rel)
	}
	switch {
	case *asJSON:
		out := make([]backlinkJSON, 0, len(bl))
		for _, b := range bl {
			out = append(out, backlinkJSON{Path: b.From, Abs: v.Abs(b.From), Title: b.Title, Line: b.Link.Line,
				Col: b.Link.Col, Kind: b.Link.Kind.String(), Context: b.Link.Context})
		}
		return a.writeJSON(out)
	case *files:
		seen := map[string]bool{}
		for _, b := range bl {
			if !seen[b.From] {
				seen[b.From] = true
				a.out("%s\n", a.shownPath(b.From))
			}
		}
		return nil
	case !a.human():
		for _, b := range bl {
			a.out("%s:%d:%s\n", a.openablePath(b.From), b.Link.Line, oneLine(b.Link.Context))
		}
		return nil
	}
	p := a.painter()
	notes := map[string]bool{}
	for _, b := range bl {
		notes[b.From] = true
	}
	a.out("%s %s\n", p.pathInk(rel), p.faint(fmt.Sprintf("%s linked from %s", p.g.Dot, plural(len(notes), "note", "notes"))))
	prev := ""
	numW := 1
	for _, b := range bl {
		numW = max(numW, len(fmt.Sprint(b.Link.Line)))
	}
	for _, b := range bl {
		if b.From != prev {
			a.out("\n")
			hdr := "  " + p.pathInk(b.From)
			if b.Title != baseTitle(b.From) {
				hdr += "  " + p.muted(p.visual(b.Title))
			}
			a.out("%s\n", hdr)
			prev = b.From
		}
		num := fmt.Sprint(b.Link.Line)
		prefix := "    " + p.faint(strings.Repeat(" ", numW-len(num))+num) + " " + p.faint(p.g.VLine) + " "
		avail := p.width - (4 + numW + 3)
		a.out("%s%s\n", prefix, p.highlight(b.Link.Context, linkSpan(b.Link), avail))
	}
	return nil
}

// linkSpan locates the link inside its context line (which is trimmed, so
// the link's column cannot be used directly): the [[…]] or […](…) construct
// naming the target.
func linkSpan(l vault.Link) [][2]int {
	ctx := l.Context
	low := strings.ToLower(ctx)
	if l.Wiki {
		needle := "[[" + strings.ToLower(l.Target)
		i := strings.Index(low, needle)
		if i < 0 {
			return nil
		}
		if i > 0 && ctx[i-1] == '!' {
			i--
		}
		j := strings.Index(ctx[i:], "]]")
		if j < 0 {
			return nil
		}
		return [][2]int{{i, i + j + 2}}
	}
	if l.Label != "" {
		needle := "[" + l.Label + "]("
		if i := strings.Index(ctx, needle); i >= 0 {
			if j := strings.IndexByte(ctx[i+len(needle):], ')'); j >= 0 {
				if i > 0 && ctx[i-1] == '!' {
					i--
				}
				return [][2]int{{i, i + len(needle) + j + 1}}
			}
		}
	}
	return nil
}

// cmdPath is `astrolabe path [NOTE]`: the vault root or a note's absolute path.
func (a *app) cmdPath(args []string) error {
	fs := newFlagSet("path")
	pos, err := fs.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		r, err := a.resolveRoot()
		if err != nil {
			return err
		}
		a.raw("%s\n", r.Dir)
		return nil
	}
	ref, err := a.resolveNote(strings.Join(pos, " "))
	if err != nil {
		return err
	}
	a.raw("%s\n", a.v.Abs(ref.Rel))
	return nil
}
