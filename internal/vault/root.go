package vault

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// RootSource says which rule chose the vault root.
type RootSource int

// The sources, in the order ResolveRoot tries them (DESIGN.md §3).
const (
	RootFromFlag    RootSource = iota // -C DIR / --dir
	RootFromEnv                       // $ASTROLABE_DIR
	RootFromMarker                    // nearest ancestor of cwd with .obsidian/ or .astrolabe/
	RootFromConfig                    // the remembered vault: config "dir"
	RootFromHome                      // ~/notes exists
	RootFromCwd                       // cwd directly contains .md files
	RootFromDefault                   // ~/notes, not yet existing (created on first write)
)

// String names the source for `astrolabe doctor` and `astrolabe vault`.
func (s RootSource) String() string {
	return [...]string{"flag", "ASTROLABE_DIR", "vault marker", "remembered", "~/notes", "working directory", "~/notes (new)"}[s]
}

// RootInputs are the explicit inputs of root resolution, so that it can be
// tested without touching the process environment.
type RootInputs struct {
	Flag   string // value of -C/--dir ("" if absent)
	Env    string // value of $ASTROLABE_DIR
	Config string // config key "dir": the remembered vault
	Cwd    string // working directory (absolute)
	Home   string // home directory (absolute)
}

// Root is the outcome of ResolveRoot.
type Root struct {
	Dir    string // absolute, cleaned
	Source RootSource
	// Exists reports whether Dir exists. Only RootFromDefault (and explicit
	// flag/env values) may name a missing directory.
	Exists bool
	// Remembered is the remembered vault (config "dir", expanded), whatever
	// rule won; "" when nothing is remembered.
	Remembered string
	// Stale is set when the remembered vault was skipped because it no
	// longer exists or is not a directory: a short reason such as
	// "/home/u/notes no longer exists".
	Stale string
}

// ResolveRoot picks the vault root in the order of DESIGN.md §3: flag,
// $ASTROLABE_DIR, the nearest ancestor of Cwd containing .obsidian/ or
// .astrolabe/ (you are standing in a vault), the remembered vault (config
// dir, skipped with Stale set when it no longer exists), ~/notes if it
// exists, Cwd if it directly contains a .md file, else ~/notes (to be
// created on first write). Flag and env values may start with "~/" and may
// be relative to Cwd; the remembered value may start with "~/" and is
// otherwise relative to Home. The only side effects are stat and directory
// reads.
func ResolveRoot(in RootInputs) (Root, error) {
	var base Root
	if c := strings.TrimSpace(in.Config); c != "" {
		base.Remembered = expandPath(c, in.Home, in.Home)
	}
	explicit := []struct {
		v   string
		src RootSource
	}{{in.Flag, RootFromFlag}, {in.Env, RootFromEnv}}
	for _, e := range explicit {
		if strings.TrimSpace(e.v) == "" {
			continue
		}
		dir := expandPath(strings.TrimSpace(e.v), in.Cwd, in.Home)
		fi, err := os.Stat(dir)
		if err == nil && !fi.IsDir() {
			return Root{}, errors.New(dir + " is not a directory")
		}
		return base.with(dir, e.src, err == nil), nil
	}
	if in.Cwd != "" {
		if dir, ok := findMarker(in.Cwd); ok {
			return base.with(dir, RootFromMarker, true), nil
		}
	}
	if base.Remembered != "" {
		fi, err := os.Stat(base.Remembered)
		switch {
		case err == nil && fi.IsDir():
			return base.with(base.Remembered, RootFromConfig, true), nil
		case err == nil:
			base.Stale = base.Remembered + " is not a directory"
		case errors.Is(err, fs.ErrNotExist):
			base.Stale = base.Remembered + " no longer exists"
		default:
			base.Stale = err.Error()
		}
	}
	if in.Home == "" {
		if in.Cwd == "" {
			return Root{}, errors.New("cannot find a vault: no home or working directory")
		}
		return base.with(filepath.Clean(in.Cwd), RootFromCwd, true), nil
	}
	notes := filepath.Join(in.Home, "notes")
	if fi, err := os.Stat(notes); err == nil && fi.IsDir() {
		return base.with(notes, RootFromHome, true), nil
	}
	if in.Cwd != "" && hasMarkdown(in.Cwd) {
		return base.with(filepath.Clean(in.Cwd), RootFromCwd, true), nil
	}
	return base.with(notes, RootFromDefault, false), nil
}

func (r Root) with(dir string, src RootSource, exists bool) Root {
	r.Dir, r.Source, r.Exists = dir, src, exists
	return r
}

// SamePath reports whether a and b name the same directory: equal after
// cleaning, or the same file once symlinks are followed.
func SamePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}

// DisplayPath is dir as shown to people: "~/…" below home, else as is.
func DisplayPath(dir, home string) string {
	dir = filepath.Clean(dir)
	if home != "" {
		home = filepath.Clean(home)
		if dir == home {
			return "~"
		}
		if rel, err := filepath.Rel(home, dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "~" + string(filepath.Separator) + rel
		}
	}
	return dir
}

// ShortNameMax is the longest DisplayPath ShortName keeps whole.
const ShortNameMax = 20

// ShortName is the vault's name for the status bar: its home-relative path
// when that is short ("~/notes"), else the last path component.
func ShortName(dir, home string) string {
	if d := DisplayPath(dir, home); utf8.RuneCountInString(d) <= ShortNameMax {
		return d
	}
	return filepath.Base(filepath.Clean(dir))
}

// ResolveFileRoot gives the vault for a file opened directly ("astrolabe
// some/file.md" outside any vault): the nearest ancestor of the file with
// .obsidian/ or .astrolabe/, else the file's own directory. It returns the root
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

// findMarker walks up from dir looking for .obsidian/ or .astrolabe/.
func findMarker(dir string) (string, bool) {
	dir = filepath.Clean(dir)
	for {
		for _, m := range []string{".obsidian", ".astrolabe", ".folio"} { // .folio: the former name
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
