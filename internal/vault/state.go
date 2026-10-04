package vault

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// MaxRecent caps the recent-notes list.
const MaxRecent = 200

// StateDir returns $XDG_STATE_HOME/folio, else ~/.local/state/folio.
// getenv is typically os.Getenv.
func StateDir(getenv func(string) string) string {
	if d := getenv("XDG_STATE_HOME"); d != "" && filepath.IsAbs(d) {
		return filepath.Join(d, "folio")
	}
	return filepath.Join(getenv("HOME"), ".local", "state", "folio")
}

// RecentFile returns the path of the recent-notes state file.
func RecentFile(getenv func(string) string) string {
	return filepath.Join(StateDir(getenv), "recent")
}

// RecentEntry is a note opened recently with the reader's last cursor
// line in it.
type RecentEntry struct {
	Path string // absolute path of the note
	Line int    // 1-based source line of the cursor (0 = unknown)
}

// Recent is the recent-notes list (most recent first) backed by a small
// plain-text file: one "LINE<TAB>ABSOLUTE PATH" per line. Losing or
// corrupting the file loses nothing important: unreadable lines are
// skipped. Safe for concurrent use.
type Recent struct {
	file    string
	mu      sync.Mutex
	entries []RecentEntry
}

// LoadRecent reads the state file. A missing or corrupt file yields an
// empty (or partial) list, never an error.
func LoadRecent(file string) *Recent {
	r := &Recent{file: file}
	data, err := os.ReadFile(file)
	if err != nil {
		return r
	}
	seen := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		tab := strings.IndexByte(line, '\t')
		if tab <= 0 {
			continue
		}
		n, err := strconv.Atoi(line[:tab])
		p := line[tab+1:]
		if err != nil || n < 0 || p == "" || !filepath.IsAbs(p) || seen[p] {
			continue
		}
		seen[p] = true
		r.entries = append(r.entries, RecentEntry{Path: p, Line: n})
		if len(r.entries) >= MaxRecent {
			break
		}
	}
	return r
}

// Entries returns a copy of the list, most recent first.
func (r *Recent) Entries() []RecentEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]RecentEntry(nil), r.entries...)
}

// Line returns the remembered cursor line for an absolute note path.
func (r *Recent) Line(path string) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.Path == path {
			return e.Line, true
		}
	}
	return 0, false
}

// Touch moves path to the front with the given cursor line.
func (r *Recent) Touch(path string, line int) {
	if path == "" || strings.ContainsAny(path, "\n\r") {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RecentEntry, 0, len(r.entries)+1)
	out = append(out, RecentEntry{Path: path, Line: line})
	for _, e := range r.entries {
		if e.Path != path {
			out = append(out, e)
		}
	}
	if len(out) > MaxRecent {
		out = out[:MaxRecent]
	}
	r.entries = out
}

// Remove forgets path (e.g. a deleted note).
func (r *Recent) Remove(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = removeEntry(r.entries, path)
}

// Rename moves the entry for oldPath to newPath, keeping its position.
func (r *Recent) Rename(oldPath, newPath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = removeEntry(r.entries, newPath)
	for i := range r.entries {
		if r.entries[i].Path == oldPath {
			r.entries[i].Path = newPath
		}
	}
}

func removeEntry(es []RecentEntry, path string) []RecentEntry {
	out := es[:0]
	for _, e := range es {
		if e.Path != path {
			out = append(out, e)
		}
	}
	return out
}

// Save writes the list atomically, creating the state directory.
func (r *Recent) Save() error {
	r.mu.Lock()
	var b strings.Builder
	for _, e := range r.entries {
		b.WriteString(strconv.Itoa(e.Line))
		b.WriteByte('\t')
		b.WriteString(e.Path)
		b.WriteByte('\n')
	}
	r.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(r.file), 0o700); err != nil {
		return err
	}
	_, err := WriteFile(r.file, []byte(b.String()), nil)
	return err
}
