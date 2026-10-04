package vault

import (
	"sort"
	"strings"
)

// TagCount is a tag with the number of notes carrying it. Nested tags roll
// up: a note tagged #a/b counts towards "a/b" and towards "a".
type TagCount struct {
	Tag   string
	Count int
}

// tagPrefixes returns "a", "a/b", "a/b/c" for "a/b/c".
func tagPrefixes(t string) []string {
	var out []string
	for i := 0; i < len(t); i++ {
		if t[i] == '/' {
			out = append(out, t[:i])
		}
	}
	return append(out, t)
}

// Tags returns every tag in the vault with its note count, sorted by tag
// (case-insensitively). Tags compare case-insensitively, as in Obsidian;
// the spelling shown is the first met in path order.
func (v *Vault) Tags() []TagCount {
	counts := map[string]int{}
	spell := map[string]string{}
	for _, n := range v.Notes() {
		seen := map[string]bool{}
		for _, t := range n.Tags {
			for _, p := range tagPrefixes(t) {
				k := strings.ToLower(p)
				if seen[k] {
					continue
				}
				seen[k] = true
				counts[k]++
				if _, ok := spell[k]; !ok {
					spell[k] = p
				}
			}
		}
	}
	out := make([]TagCount, 0, len(counts))
	for k, c := range counts {
		out = append(out, TagCount{Tag: spell[k], Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Tag), strings.ToLower(out[j].Tag)
		if a != b {
			return a < b
		}
		return out[i].Tag < out[j].Tag
	})
	return out
}

// HasTag reports whether the note carries tag or a tag nested under it
// (case-insensitive; a leading '#' is ignored).
func (n *Note) HasTag(tag string) bool {
	want := strings.ToLower(strings.Trim(strings.TrimPrefix(tag, "#"), "/"))
	if want == "" {
		return false
	}
	for _, t := range n.Tags {
		lt := strings.ToLower(t)
		if lt == want || strings.HasPrefix(lt, want+"/") {
			return true
		}
	}
	return false
}

// NotesWithTag returns the notes carrying tag or a tag nested under it,
// most recently modified first.
func (v *Vault) NotesWithTag(tag string) []*Note {
	var out []*Note
	for _, n := range v.RecentNotes() {
		if n.HasTag(tag) {
			out = append(out, n)
		}
	}
	return out
}
