package vault

import (
	"path"
	"sort"
	"strings"
	"unicode"
)

// Target is where a link points.
type Target struct {
	// Path is the vault-relative path of the target note or attachment.
	Path string
	// IsNote is true when the target is an indexed Markdown note.
	IsNote bool
	// Line is the 1-based line of the linked heading or block, or 0 when
	// the link names the whole note or the anchor was not found.
	Line int
	// AnchorMissing is true when the link names a heading or block that the
	// target does not contain (the note itself exists).
	AnchorMissing bool
}

// TargetCount is a broken link target with the number of links to it.
type TargetCount struct {
	Target string
	Count  int
}

// Backlink is one incoming link.
type Backlink struct {
	From  string // vault-relative path of the linking note
	Title string // its title
	Link  Link   // the link as written (Line, Col and Context included)
}

// BrokenLink is a link whose target does not exist.
type BrokenLink struct {
	From string
	Link Link
}

// ResolveLink resolves a link written in the note at from, following
// Obsidian (DESIGN.md §3):
//
//  1. Exact path: for Markdown links relative to the linking note's folder,
//     then relative to the vault root; for wikilinks relative to the vault
//     root, then to the note's folder. ".md" is optional; an exact
//     case-sensitive match beats a case-insensitive one.
//  2. Shortest matching path by basename, case-insensitive, ".md"
//     optional; if the target has folders ("a/b"), candidates must end with
//     them. Ties: fewer path segments, then shorter path, then
//     lexicographic order, so resolution is deterministic.
//  3. Aliases (case-insensitive), ties broken the same way.
//
// A link with an empty Target refers to the linking note itself. URLs never
// resolve. Headings and blocks resolve to a line; a missing anchor still
// resolves the note with AnchorMissing set.
func (v *Vault) ResolveLink(from string, l Link) (Target, bool) {
	if l.Kind == KindURL {
		return Target{}, false
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.resolveLocked(from, l)
}

func (v *Vault) resolveLocked(from string, l Link) (Target, bool) {
	var rel string
	if l.Target == "" {
		if _, ok := v.notes[from]; !ok {
			return Target{}, false
		}
		rel = from
	} else {
		var ok bool
		rel, ok = v.resolvePathLocked(from, l.Target, !l.Wiki)
		if !ok {
			return Target{}, false
		}
	}
	t := Target{Path: rel}
	n, isNote := v.notes[rel]
	t.IsNote = isNote
	if isNote {
		switch {
		case l.Block != "":
			if line := blockLine(n, l.Block); line > 0 {
				t.Line = line
			} else {
				t.AnchorMissing = true
			}
		case l.Heading != "":
			if line := headingLine(n, l.Heading); line > 0 {
				t.Line = line
			} else {
				t.AnchorMissing = true
			}
		}
	}
	return t, true
}

// ResolveName resolves a note name typed by the user ("Note", "folder/Note",
// "note.md", an alias) as a wikilink written at the vault root.
func (v *Vault) ResolveName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(strings.TrimPrefix(name, "[["), "]]")
	if name == "" {
		return "", false
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	rel, ok := v.resolvePathLocked("", name, false)
	if !ok {
		return "", false
	}
	if _, isNote := v.notes[rel]; !isNote {
		return "", false
	}
	return rel, true
}

// resolvePathLocked implements the three resolution steps for a target
// path or name.
func (v *Vault) resolvePathLocked(from, target string, relativeFirst bool) (string, bool) {
	target = strings.ReplaceAll(strings.TrimSpace(target), "\\", "/")
	if target == "" {
		return "", false
	}
	dir := path.Dir(from)
	if from == "" {
		dir = "."
	}
	var cands []string
	rootRel := path.Clean(strings.TrimPrefix(target, "/"))
	srcRel := path.Clean(path.Join(dir, target))
	explicitRel := strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../")
	if relativeFirst || explicitRel {
		cands = []string{srcRel, rootRel}
	} else {
		cands = []string{rootRel, srcRel}
	}
	// 1. exact path (case-sensitive, then case-insensitive).
	for _, c := range cands {
		if strings.HasPrefix(c, "../") || c == ".." {
			continue
		}
		for _, p := range []string{c, c + ".md"} {
			if v.exists(p) {
				return p, true
			}
		}
	}
	for _, c := range cands {
		lc := strings.ToLower(c)
		key := baseKey(c)
		var hits []string
		for _, p := range v.byBase[key] {
			lp := strings.ToLower(p)
			if lp == lc || lp == lc+".md" {
				hits = append(hits, p)
			}
		}
		if len(hits) > 0 {
			return bestPath(hits), true
		}
	}
	// 2. shortest path by basename.
	t := strings.TrimPrefix(path.Clean(strings.TrimPrefix(target, "./")), "/")
	for strings.HasPrefix(t, "../") {
		t = t[3:]
	}
	key := baseKey(t)
	lt := strings.ToLower(t)
	ltNoExt := lt
	if isMarkdown(lt) {
		ltNoExt = lt[:len(lt)-3]
	}
	hasExt := path.Ext(lt) != "" && !isMarkdown(lt)
	var hits []string
	for _, p := range v.byBase[key] {
		lp := strings.ToLower(p)
		_, isNote := v.notes[p]
		if !isNote && !hasExt {
			// "[[image]]" does not match image.png; attachments need their
			// extension, as in Obsidian.
			continue
		}
		lpNoExt := lp
		if isMarkdown(lp) {
			lpNoExt = lp[:len(lp)-3]
		}
		if lpNoExt == ltNoExt || strings.HasSuffix(lpNoExt, "/"+ltNoExt) {
			hits = append(hits, p)
		}
	}
	if len(hits) > 0 {
		return bestPath(hits), true
	}
	// 3. aliases.
	if ps := v.byAls[strings.ToLower(target)]; len(ps) > 0 {
		return bestPath(ps), true
	}
	return "", false
}

func (v *Vault) exists(rel string) bool {
	if _, ok := v.notes[rel]; ok {
		return true
	}
	_, ok := v.files[rel]
	return ok
}

// bestPath picks the deterministic winner among candidate paths: fewest
// segments, then shortest, then lexicographically smallest.
func bestPath(ps []string) string {
	best := ps[0]
	for _, p := range ps[1:] {
		if pathLess(p, best) {
			best = p
		}
	}
	return best
}

func pathLess(a, b string) bool {
	sa, sb := strings.Count(a, "/"), strings.Count(b, "/")
	if sa != sb {
		return sa < sb
	}
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

// headingKey normalises heading text for matching: case-folded, markup
// and punctuation dropped, whitespace collapsed (Obsidian matches headings
// loosely in the same way).
func headingKey(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		case unicode.IsSpace(r) || r == '-' || r == '_':
			space = true
		}
	}
	return b.String()
}

// headingLine finds the line of the heading named h (the last segment of
// a nested "A#B" chain is used), or 0.
func headingLine(n *Note, h string) int {
	if i := strings.LastIndexByte(h, '#'); i >= 0 {
		h = h[i+1:]
	}
	want := headingKey(h)
	if want == "" {
		return 0
	}
	for _, hd := range n.Headings {
		if hd.Text == h {
			return hd.Line
		}
	}
	for _, hd := range n.Headings {
		if headingKey(hd.Text) == want {
			return hd.Line
		}
	}
	return 0
}

func blockLine(n *Note, id string) int {
	for _, b := range n.Blocks {
		if b.ID == id {
			return b.Line
		}
	}
	for _, b := range n.Blocks {
		if strings.EqualFold(b.ID, id) {
			return b.Line
		}
	}
	return 0
}

// backIndex caches the resolved link graph for one index generation.
type backIndex struct {
	gen    uint64
	in     map[string][]Backlink
	broken []BrokenLink
}

func (v *Vault) graph() *backIndex {
	v.mu.RLock()
	if v.back != nil && v.back.gen == v.gen {
		b := v.back
		v.mu.RUnlock()
		return b
	}
	v.mu.RUnlock()
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.back != nil && v.back.gen == v.gen {
		return v.back
	}
	b := &backIndex{gen: v.gen, in: map[string][]Backlink{}}
	paths := make([]string, 0, len(v.notes))
	for p := range v.notes {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		n := v.notes[p]
		for _, l := range n.Links {
			if l.Kind == KindURL {
				continue
			}
			t, ok := v.resolveLocked(p, l)
			if !ok {
				b.broken = append(b.broken, BrokenLink{From: p, Link: l})
				continue
			}
			if t.Path == p && l.Target == "" {
				continue // in-note anchor links are not backlinks
			}
			b.in[t.Path] = append(b.in[t.Path], Backlink{From: p, Title: n.Title, Link: l})
		}
	}
	v.back = b
	return b
}

// Backlinks returns the links pointing at the note rel, ordered by linking
// note path then line. Each carries the source line as context.
func (v *Vault) Backlinks(rel string) []Backlink {
	g := v.graph()
	src := g.in[rel]
	out := make([]Backlink, len(src))
	copy(out, src)
	return out
}

// BacklinkCount returns the number of distinct notes linking to rel.
func (v *Vault) BacklinkCount(rel string) int {
	g := v.graph()
	seen := map[string]bool{}
	for _, b := range g.in[rel] {
		seen[b.From] = true
	}
	return len(seen)
}

// Unresolved returns every link whose target does not exist (URLs
// excluded), ordered by linking note path then line.
func (v *Vault) Unresolved() []BrokenLink {
	g := v.graph()
	out := make([]BrokenLink, len(g.broken))
	copy(out, g.broken)
	return out
}

// UnresolvedTargets returns the distinct broken targets (as written,
// grouped case-insensitively) with the number of links to each, sorted by
// target.
func (v *Vault) UnresolvedTargets() []TargetCount {
	counts := map[string]int{}
	spell := map[string]string{}
	for _, b := range v.Unresolved() {
		k := strings.ToLower(b.Link.Target)
		if _, ok := spell[k]; !ok {
			spell[k] = b.Link.Target
		}
		counts[k]++
	}
	out := make([]TargetCount, 0, len(counts))
	for k, c := range counts {
		out = append(out, TargetCount{Target: spell[k], Count: c})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Target) < strings.ToLower(out[j].Target) })
	return out
}
