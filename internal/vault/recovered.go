package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Recovered buffers: when astrolabe is terminated (SIGHUP when the terminal
// window closes, SIGTERM) with unsaved edits in the built-in editor and the
// note cannot be written safely (it changed on disk since it was read, or
// the write fails), the buffer is written here instead of being lost. This
// is outside the vault, like the recent-notes file (DESIGN.md §2).

// RecoveredDir returns $XDG_STATE_HOME/astrolabe/recovered.
func RecoveredDir(getenv func(string) string) string {
	return filepath.Join(StateDir(getenv), "recovered")
}

// recoveredSeen marks, by its modification time, the newest recovered
// buffer the user has already been told about.
const recoveredSeen = ".seen"

// WriteRecovered writes data to dir/<timestamp>-<basename>.md, creating
// dir (mode 0700; the file is 0600: it may hold private notes). It never
// overwrites an existing file. It returns the path written.
func WriteRecovered(dir, note string, data []byte, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	base := strings.TrimSuffix(filepath.Base(note), ".md")
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "note"
	}
	stamp := now.Format("20060102-150405")
	for i := 0; i < 100; i++ {
		name := stamp + "-" + base + ".md"
		if i > 0 {
			name = fmt.Sprintf("%s-%s-%d.md", stamp, base, i+1)
		}
		p := filepath.Join(dir, name)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(data)
		if serr := f.Sync(); werr == nil {
			werr = serr
		}
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			os.Remove(p)
			return "", werr
		}
		return p, nil
	}
	return "", fmt.Errorf("too many recovered buffers named %s-%s", stamp, base)
}

// RecoveredFiles lists the recovered buffers in dir, newest first.
func RecoveredFiles(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type f struct {
		p string
		t time.Time
	}
	var fs []f
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fs = append(fs, f{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	sort.SliceStable(fs, func(i, j int) bool {
		if !fs[i].t.Equal(fs[j].t) {
			return fs[i].t.After(fs[j].t)
		}
		return fs[i].p > fs[j].p
	})
	out := make([]string, len(fs))
	for i := range fs {
		out[i] = fs[i].p
	}
	return out
}

// UnseenRecovered returns the recovered buffers written since the last
// call (newest first) and marks them seen, so the next start does not
// announce them again. Files stay until the user deletes them.
func UnseenRecovered(dir string) []string {
	all := RecoveredFiles(dir)
	if len(all) == 0 {
		return nil
	}
	mark := filepath.Join(dir, recoveredSeen)
	var seen time.Time
	if info, err := os.Stat(mark); err == nil {
		seen = info.ModTime()
	}
	var out []string
	newest := seen
	for _, p := range all {
		info, err := os.Stat(p)
		if err != nil || !info.ModTime().After(seen) {
			continue
		}
		out = append(out, p)
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	if len(out) > 0 {
		if err := os.WriteFile(mark, nil, 0o600); err == nil {
			_ = os.Chtimes(mark, newest, newest)
		}
	}
	return out
}
