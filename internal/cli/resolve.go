package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/vault"
)

// noteRef is a resolved NOTE argument.
type noteRef struct {
	Rel  string // vault-relative slash path
	Line int    // 1-based line from a "#heading", "#^block" or ":N" suffix (0 = none)
}

// resolveNote turns a NOTE argument into a note of the vault, following
// DESIGN.md §4.1: an existing file path (relative to the working directory,
// absolute, or "~/…"); else a vault-relative path; else a title, basename or
// alias resolved like a wikilink; else a fuzzy match if exactly one note
// matches. "Note#Heading", "Note#^block" and "path:LINE" suffixes give a
// line. A file outside the current vault switches to that file's vault
// (vault.ResolveFileRoot), as `folio some/file.md` does.
//
// Failure is a *notFound error listing the nearest notes.
func (a *app) resolveNote(arg string) (noteRef, error) {
	if strings.TrimSpace(arg) == "" {
		return noteRef{}, usagef("", "empty note name")
	}
	if ref, ok, err := a.resolveFileArg(arg); ok || err != nil {
		return ref, err
	}
	// "path:LINE" (as printed by find) when the whole argument is not a file.
	if i := strings.LastIndexByte(arg, ':'); i > 0 {
		if n, err := strconv.Atoi(arg[i+1:]); err == nil && n > 0 {
			if ref, ok, err := a.resolveFileArg(arg[:i]); ok || err != nil {
				ref.Line = n
				return ref, err
			}
		}
	}
	v, err := a.vault()
	if err != nil {
		return noteRef{}, err
	}
	// Vault-relative path without scanning (first paint stays fast).
	if rel, ok := statNote(v.Root(), arg); ok {
		return noteRef{Rel: rel}, nil
	}
	v, err = a.scannedVault()
	if err != nil {
		return noteRef{}, err
	}
	name, anchor := arg, ""
	if i := strings.IndexByte(arg, '#'); i > 0 {
		name, anchor = arg[:i], arg[i+1:]
	}
	if rel, ok := v.ResolveName(name); ok {
		ref := noteRef{Rel: rel}
		if anchor != "" {
			l := vault.Link{Kind: vault.KindWikilink, Wiki: true}
			if strings.HasPrefix(anchor, "^") {
				l.Block = anchor[1:]
			} else {
				l.Heading = anchor
			}
			if t, ok := v.ResolveLink(rel, l); ok && !t.AnchorMissing {
				ref.Line = t.Line
			}
		}
		return ref, nil
	}
	if rel, ok := v.ResolveName(arg); ok { // a name that really contains '#'
		return noteRef{Rel: rel}, nil
	}
	cands := fuzzyNotes(v.Notes(), name)
	if len(cands) == 1 && !(a.firstArg && typoVerb(firstWord(arg)) != "") {
		return noteRef{Rel: cands[0].note.Path}, nil
	}
	return noteRef{}, a.noteNotFound(arg, cands, v)
}

// resolveFileArg handles a NOTE argument naming an existing file.
func (a *app) resolveFileArg(arg string) (noteRef, bool, error) {
	p := arg
	if strings.HasPrefix(p, "~/") && a.env.Home != "" {
		p = filepath.Join(a.env.Home, p[2:])
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(a.env.Cwd, p)
	}
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return noteRef{}, false, nil
	}
	root, err := a.resolveRoot()
	if err != nil {
		return noteRef{}, true, err
	}
	if within(root.Dir, p) {
		if _, err := a.vault(); err != nil {
			return noteRef{}, true, err
		}
		rel, err := filepath.Rel(root.Dir, p)
		if err != nil {
			return noteRef{}, true, err
		}
		return noteRef{Rel: filepath.ToSlash(rel)}, true, nil
	}
	// Outside the vault: the file's own vault (DESIGN.md §3).
	froot, rel, err := vault.ResolveFileRoot(p, a.env.Cwd)
	if err != nil {
		return noteRef{}, true, err
	}
	if _, err := a.openVaultAt(vault.Root{Dir: froot, Source: vault.RootFromMarker, Exists: true}); err != nil {
		return noteRef{}, true, err
	}
	return noteRef{Rel: rel}, true, nil
}

// statNote checks root/arg and root/arg.md for a regular file.
func statNote(root, arg string) (string, bool) {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(arg)))
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." || filepath.IsAbs(arg) {
		return "", false
	}
	for _, c := range []string{clean, clean + ".md"} {
		if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(c))); err == nil && fi.Mode().IsRegular() {
			return c, true
		}
	}
	return "", false
}

// fuzzyCand is a note matched fuzzily with its best score over path,
// title and aliases.
type fuzzyCand struct {
	note  *vault.Note
	score int
}

// fuzzyNotes returns the notes whose path (without .md), title or an alias
// fuzzily match pattern, best first.
func fuzzyNotes(notes []*vault.Note, pattern string) []fuzzyCand {
	m := text.NewMatcher(pattern)
	var out []fuzzyCand
	for _, n := range notes {
		best, hit := 0, false
		try := func(s string) {
			if mt, ok := m.Match(s); ok && (!hit || mt.Score > best) {
				best, hit = mt.Score, true
			}
		}
		try(strings.TrimSuffix(n.Path, ".md"))
		try(n.Title)
		for _, al := range n.Aliases {
			try(al)
		}
		if hit {
			out = append(out, fuzzyCand{n, best})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].note.Path < out[j].note.Path
	})
	return out
}

// noteNotFound builds the "no such note" error with the nearest matches
// (fuzzy matches if any, else by edit distance) and a verb suggestion when
// the argument looks like a mistyped command.
func (a *app) noteNotFound(arg string, cands []fuzzyCand, v *vault.Vault) error {
	var b strings.Builder
	var near []*vault.Note
	// A mistyped verb is the likelier mistake: say so first, before any
	// note matches (`folio tsks`).
	verb := nearestVerb(arg)
	if a.firstArg {
		w := firstWord(arg)
		if v := nearestVerb(w); v != "" {
			fmt.Fprintf(&b, "did you mean `folio %s`?\n", strings.TrimSpace(v+" "+strings.TrimSpace(arg[len(w):])))
		}
	}
	if len(cands) > 1 {
		fmt.Fprintf(&b, "%q matches %d notes; be more specific", arg, len(cands))
		for i := 0; i < len(cands) && i < 6; i++ {
			near = append(near, cands[i].note)
		}
	} else {
		fmt.Fprintf(&b, "no note or command called %q", arg)
		near = nearestNotes(v.Notes(), arg, 5)
		if len(cands) == 1 { // a fuzzy match not taken (a verb typo): list it first
			kept := []*vault.Note{cands[0].note}
			for _, n := range near {
				if n != cands[0].note {
					kept = append(kept, n)
				}
			}
			near = kept
		}
	}
	if verb != "" && !a.firstArg {
		fmt.Fprintf(&b, "\n  did you mean: folio %s", verb)
	}
	if len(near) > 0 {
		b.WriteString("\n  nearest notes:")
		w := 0
		for _, n := range near {
			w = max(w, text.Width(n.Path))
		}
		for _, n := range near {
			line := "\n    " + text.PadRight(n.Path, w)
			if n.Title != strings.TrimSuffix(filepath.Base(n.Path), ".md") {
				line += "  " + n.Title
			}
			b.WriteString(strings.TrimRight(line, " "))
		}
	} else if v.Len() == 0 {
		fmt.Fprintf(&b, "\n  the vault at %s has no notes", v.Root())
	}
	return &notFound{msg: b.String()}
}

// nearestNotes ranks notes by edit distance between arg and their title or
// basename, keeping only plausible matches.
func nearestNotes(notes []*vault.Note, arg string, k int) []*vault.Note {
	type scored struct {
		n *vault.Note
		d int
	}
	q := strings.ToLower(arg)
	limit := max(2, utf8.RuneCountInString(q)/2)
	var s []scored
	for _, n := range notes {
		d := levenshtein(q, strings.ToLower(n.Title))
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(n.Path), ".md"))
		d = min(d, levenshtein(q, base))
		for _, al := range n.Aliases {
			d = min(d, levenshtein(q, strings.ToLower(al)))
		}
		if d <= limit {
			s = append(s, scored{n, d})
		}
	}
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].d != s[j].d {
			return s[i].d < s[j].d
		}
		return s[i].n.Path < s[j].n.Path
	})
	var out []*vault.Note
	for i := 0; i < len(s) && i < k; i++ {
		out = append(out, s[i].n)
	}
	return out
}

// nearestVerb returns a verb within edit distance 2 of a single-word arg
// (or of which arg is a prefix of at least two letters).
func nearestVerb(arg string) string {
	if strings.ContainsAny(arg, " /.") || arg == "" {
		return ""
	}
	best, bestD := "", 3
	for _, v := range verbOrder {
		d := levenshtein(strings.ToLower(arg), v)
		if len(arg) >= 2 && strings.HasPrefix(v, strings.ToLower(arg)) {
			d = min(d, 1)
		}
		if d < bestD && d < len(v) {
			best, bestD = v, d
		}
	}
	return best
}

// typoVerb returns the verb a first argument is a typo of: within edit
// distance 2 of it but not one of its prefixes (DESIGN.md §4.1: unique verb
// prefixes are not verbs, so `folio ad` may still open add.md). Such an
// argument is not opened by a fuzzy match; it gets the suggestion instead.
func typoVerb(arg string) string {
	a := strings.ToLower(arg)
	if utf8.RuneCountInString(a) < 3 || strings.ContainsAny(a, " /.#:") {
		return ""
	}
	best, bestD := "", 3
	for _, v := range verbOrder {
		if strings.HasPrefix(v, a) {
			continue
		}
		if d := levenshtein(a, v); d < bestD {
			best, bestD = v, d
		}
	}
	return best
}

// firstWord is arg up to its first blank.
func firstWord(arg string) string {
	arg = strings.TrimSpace(arg)
	if i := strings.IndexAny(arg, " \t"); i >= 0 {
		return arg[:i]
	}
	return arg
}

// levenshtein is the edit distance between a and b in runes.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
