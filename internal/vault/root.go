package vault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// RootSource says which rule chose the vault root.
type RootSource int

const (
	RootFromFlag    RootSource = iota // -C DIR / --dir
	RootFromEnv                       // $FOLIO_DIR
	RootFromConfig                    // config "dir"
	RootFromMarker                    // nearest ancestor of cwd with .obsidian/ or .folio/
	RootFromHome                      // ~/notes exists
	RootFromCwd                       // cwd directly contains .md files
	RootFromDefault                   // ~/notes, not yet existing (created on first write)
)

// String names the source for `folio doctor`.
func (s RootSource) String() string {
	return [...]string{"flag", "FOLIO_DIR", "config", "vault marker", "~/notes", "working directory", "~/notes (new)"}[s]
}

// RootInputs are the explicit inputs of root resolution, so that it can be
// tested without touching the process environment.
type RootInputs struct {
	Flag   string // value of -C/--dir ("" if absent)
	Env    string // value of $FOLIO_DIR
	Config string // config key "dir"
	Cwd    string // working directory (absolute)
	Home   string // home directory (absolute)
}

// Root is the outcome of ResolveRoot.
type Root struct {
	Dir    string // absolute, cleaned
	Source RootSource
	// Exists reports whether Dir exists. Only RootFromDefault (and explicit
	// flag/env/config values) may name a missing directory.
	Exists bool
}

// ResolveRoot picks the vault root in the order of DESIGN.md §3: flag,
// $FOLIO_DIR, config dir, the nearest ancestor of Cwd containing .obsidian/
// or .folio/, ~/notes if it exists, Cwd if it directly contains a .md file,
// else ~/notes (to be created on first write). Explicit values may start
// with "~/" and may be relative to Cwd. The only side effects are stat and
// directory reads.
func ResolveRoot(in RootInputs) (Root, error) {
	explicit := []struct {
		v   string
		src RootSource
	}{{in.Flag, RootFromFlag}, {in.Env, RootFromEnv}, {in.Config, RootFromConfig}}
	for _, e := range explicit {
		if strings.TrimSpace(e.v) == "" {
			continue
		}
		dir := expandPath(strings.TrimSpace(e.v), in.Cwd, in.Home)
		fi, err := os.Stat(dir)
		if err == nil && !fi.IsDir() {
			return Root{}, errors.New(dir + " is not a directory")
		}
		return Root{Dir: dir, Source: e.src, Exists: err == nil}, nil
	}
	if in.Cwd != "" {
		if dir, ok := findMarker(in.Cwd); ok {
			return Root{Dir: dir, Source: RootFromMarker, Exists: true}, nil
		}
	}
	if in.Home == "" {
		if in.Cwd == "" {
			return Root{}, errors.New("cannot find a vault: no home or working directory")
		}
		return Root{Dir: filepath.Clean(in.Cwd), Source: RootFromCwd, Exists: true}, nil
	}
	notes := filepath.Join(in.Home, "notes")
	if fi, err := os.Stat(notes); err == nil && fi.IsDir() {
		return Root{Dir: notes, Source: RootFromHome, Exists: true}, nil
	}
	if in.Cwd != "" && hasMarkdown(in.Cwd) {
		return Root{Dir: filepath.Clean(in.Cwd), Source: RootFromCwd, Exists: true}, nil
	}
	return Root{Dir: notes, Source: RootFromDefault, Exists: false}, nil
}

// ResolveFileRoot gives the vault for a file opened directly ("folio
// some/file.md" outside any vault): the nearest ancestor of the file with
// .obsidian/ or .folio/, else the file's own directory. It returns the root
// and the file's vault-relative slash path.
func ResolveFileRoot(file, cwd string) (root, rel string, err error) {
	abs := file
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, abs)
	}
	abs = filepath.Clean(abs)
	dir := filepath.Dir(abs)
	if r, ok := findMarker(dir); ok {
		root = r
	} else {
		root = dir
	}
	rel, err = filepath.Rel(root, abs)
	if err != nil {
		return "", "", err
	}
	return root, filepath.ToSlash(rel), nil
}

// expandPath expands a leading "~" and makes p absolute against cwd.
func expandPath(p, cwd, home string) string {
	if p == "~" {
		p = home
	} else if strings.HasPrefix(p, "~/") && home != "" {
		p = filepath.Join(home, p[2:])
	}
	if !filepath.IsAbs(p) && cwd != "" {
		p = filepath.Join(cwd, p)
	}
	return filepath.Clean(p)
}

// findMarker walks up from dir looking for .obsidian/ or .folio/.
func findMarker(dir string) (string, bool) {
	dir = filepath.Clean(dir)
	for {
		for _, m := range []string{".obsidian", ".folio"} {
			if fi, err := os.Stat(filepath.Join(dir, m)); err == nil && fi.IsDir() {
				return dir, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// hasMarkdown reports whether dir directly contains a .md file.
func hasMarkdown(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && isMarkdown(e.Name()) && !strings.HasPrefix(e.Name(), ".") {
			return true
		}
	}
	return false
}
