package vault

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// Vault is an open notes folder and its in-memory index. All methods are
// safe for concurrent use. Opening is instant; the index fills in as the
// background scan proceeds, and queries made before the scan completes
// answer from what is known so far.
type Vault struct {
	root string // absolute, cleaned

	mu     sync.RWMutex
	notes  map[string]*Note    // rel path -> note
	files  map[string]struct{} // non-Markdown files (attachments), rel path
	byBase map[string][]string // lower(basename without .md) -> rel paths (notes and files)
	byAls  map[string][]string // lower(alias) -> rel paths
	gen    uint64              // bumped on every index change
	seq    uint64              // read sequence counter
	stamp  map[string]uint64   // rel -> seq of the read that produced the indexed note
	back   *backIndex          // backlink cache, valid for back.gen
	daily  DailyConfig
	ignore []string

	scanOnce sync.Once
	scanDone chan struct{}
	scanErr  error
	scanned  bool
}

// Open returns the vault rooted at dir without scanning it. dir need not
// exist yet (a fresh ~/notes is created on first write). Open reads only
// the small .folioignore and .obsidian/daily-notes.json files.
func Open(dir string) (*Vault, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if fi, err := os.Stat(abs); err == nil && !fi.IsDir() {
		return nil, fmt.Errorf("vault root %s is not a directory", abs)
	}
	v := &Vault{
		root:     abs,
		notes:    map[string]*Note{},
		files:    map[string]struct{}{},
		byBase:   map[string][]string{},
		byAls:    map[string][]string{},
		stamp:    map[string]uint64{},
		scanDone: make(chan struct{}),
	}
	v.ignore = loadIgnore(abs)
	v.daily = LoadDailyConfig(abs)
	return v, nil
}

// Root returns the absolute vault directory.
func (v *Vault) Root() string { return v.root }

// Abs returns the absolute path of a vault-relative path.
func (v *Vault) Abs(rel string) string {
	return filepath.Join(v.root, filepath.FromSlash(rel))
}

// Rel converts a path (absolute, or relative to the working directory) to
// a vault-relative slash path. It fails if the path is outside the vault.
func (v *Vault) Rel(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(v.root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		// Allow paths reached through a symlinked root.
		if r2, e2 := filepath.EvalSymlinks(abs); e2 == nil {
			if root2, e3 := filepath.EvalSymlinks(v.root); e3 == nil {
				if rr, e4 := filepath.Rel(root2, r2); e4 == nil && rr != ".." && !strings.HasPrefix(rr, ".."+string(filepath.Separator)) {
					return filepath.ToSlash(rr), nil
				}
			}
		}
		return "", fmt.Errorf("%s is outside the vault %s", p, v.root)
	}
	return filepath.ToSlash(rel), nil
}

// cleanRel validates a vault-relative path supplied by a caller and
// returns it cleaned, with forward slashes.
func cleanRel(rel string) (string, error) {
	rel = strings.ReplaceAll(rel, "\\", "/")
	c := path.Clean(rel)
	if c == "" || c == "." {
		return "", fmt.Errorf("empty note path")
	}
	if path.IsAbs(c) || c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("note path %q is outside the vault", rel)
	}
	return c, nil
}

// StartScan starts the initial full scan in the background (only the first
// call has an effect) and returns a channel that is closed when it
// completes. Cancelling ctx stops the scan early; ScanErr then reports
// the context error.
func (v *Vault) StartScan(ctx context.Context) <-chan struct{} {
	v.scanOnce.Do(func() {
		go func() {
			_, err := v.Rescan(ctx)
			v.mu.Lock()
			v.scanErr = err
			v.mu.Unlock()
			close(v.scanDone)
		}()
	})
	return v.scanDone
}

// Scan runs the initial scan synchronously (or waits for a running one).
func (v *Vault) Scan(ctx context.Context) error {
	done := v.StartScan(ctx)
	select {
	case <-done:
		return v.ScanErr()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Done returns a channel closed when the initial scan started by StartScan
// or Scan has finished. It is never closed if no scan was started.
func (v *Vault) Done() <-chan struct{} { return v.scanDone }

// Scanned reports whether a full scan (the initial one or a Rescan) has
// completed successfully, i.e. the index covers the whole vault.
func (v *Vault) Scanned() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.scanned
}

// ScanErr returns the error of the finished initial scan, if any.
func (v *Vault) ScanErr() error {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.scanErr
}

// walkEntry is one file found by the walker.
type walkEntry struct {
	rel  string
	abs  string
	info fs.FileInfo // of the target for symlinks
	md   bool
}

// walk lists the vault's files, applying the scan rules of DESIGN.md §3:
// dot-directories, node_modules and .folioignore matches are skipped; file
// symlinks are followed, directory symlinks are not.
//
// Names containing control characters (a newline, a tab, ESC…) are skipped
// too: the shell verbs promise one result per line in path:line:text form,
// which such a name would break, and printing it to a terminal would emit
// raw control bytes. Such a file can still be opened by naming its path.
func (v *Vault) walk(ctx context.Context, out chan<- walkEntry) error {
	defer close(out)
	root := v.root
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r // the root itself may be a symlink; walk its target
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				if errors.Is(err, fs.ErrNotExist) {
					return filepath.SkipAll
				}
				return err
			}
			return nil // unreadable subtree: skip it
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if p == root {
			return nil
		}
		rel := filepath.ToSlash(p[len(root)+1:])
		name := d.Name()
		if hasControl(name) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(name, ".") || name == "node_modules" || v.ignored(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || v.ignored(rel, false) {
			return nil
		}
		var info fs.FileInfo
		if d.Type()&fs.ModeSymlink != 0 {
			fi, err := os.Stat(p)
			if err != nil || !fi.Mode().IsRegular() {
				return nil // dangling, or a directory symlink: not followed
			}
			info = fi
		} else if d.Type().IsRegular() {
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			info = fi
		} else {
			return nil
		}
		select {
		case out <- walkEntry{rel: rel, abs: p, info: info, md: isMarkdown(name)}:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	return err
}

// hasControl reports whether name contains a C0 control character or DEL.
func hasControl(name string) bool {
	for i := 0; i < len(name); i++ {
		if c := name[i]; c < 0x20 || c == 0x7f {
			return true
		}
	}
	return false
}

func isMarkdown(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".md")
}

// Rescan brings the index up to date with the disk, re-reading only files
// whose modification time or size changed and dropping files that are gone.
// It returns the number of notes added, changed or removed. Files are read
// by a bounded pool of workers.
func (v *Vault) Rescan(ctx context.Context) (int, error) {
	entries := make(chan walkEntry, 256)
	walkErr := make(chan error, 1)
	go func() { walkErr <- v.walk(ctx, entries) }()

	workers := runtime.GOMAXPROCS(0)
	if workers < 2 {
		workers = 2
	}
	if workers > 16 {
		workers = 16
	}
	var (
		wg      sync.WaitGroup
		seenMu  sync.Mutex
		seen    = map[string]bool{}
		changed int
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range entries {
				seenMu.Lock()
				seen[e.rel] = true
				seenMu.Unlock()
				if !e.md {
					v.mu.Lock()
					if _, ok := v.files[e.rel]; !ok {
						v.files[e.rel] = struct{}{}
						v.addBase(e.rel)
						v.gen++
					}
					v.mu.Unlock()
					continue
				}
				v.mu.RLock()
				old := v.notes[e.rel]
				v.mu.RUnlock()
				if old != nil && old.ModTime.Equal(e.info.ModTime()) && old.Size == e.info.Size() {
					continue
				}
				rs := v.nextSeq()
				n, err := readNote(e.abs, e.rel, e.info)
				if err != nil {
					continue
				}
				if v.put(n, rs) {
					seenMu.Lock()
					changed++
					seenMu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	err := <-walkErr
	if err != nil {
		return changed, err
	}
	// Drop what disappeared.
	v.mu.Lock()
	for rel := range v.notes {
		if !seen[rel] {
			v.removeLocked(rel)
			changed++
		}
	}
	for rel := range v.files {
		if !seen[rel] {
			delete(v.files, rel)
			v.dropBase(rel)
			v.gen++
		}
	}
	v.scanned = true // a complete walk: the index now covers the vault
	v.mu.Unlock()
	return changed, nil
}

// readNote reads and indexes one file.
func readNote(abs, rel string, info fs.FileInfo) (*Note, error) {
	if info.Size() > LargeFileSize {
		f, err := os.Open(abs)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		head := make([]byte, 64<<10)
		k, _ := f.Read(head)
		return parseNote(rel, head[:k], info.ModTime(), info.Size()), nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	return parseNote(rel, data, info.ModTime(), info.Size()), nil
}

// nextSeq returns a sequence number to take before reading a file, so
// that put can tell which of two racing reads is the newer.
func (v *Vault) nextSeq() uint64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.seq++
	return v.seq
}

// put inserts or replaces a note read under sequence number rs, unless the
// index already holds a note from a later read (a Refresh that raced with
// a scan). It reports whether the index changed.
func (v *Vault) put(n *Note, rs uint64) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.stamp[n.Path] > rs {
		return false
	}
	if old := v.notes[n.Path]; old != nil {
		v.unindexLocked(old)
	} else {
		v.addBase(n.Path)
	}
	v.notes[n.Path] = n
	v.stamp[n.Path] = rs
	for _, a := range n.Aliases {
		k := strings.ToLower(a)
		v.byAls[k] = appendUnique(v.byAls[k], n.Path)
	}
	v.gen++
	return true
}

func (v *Vault) unindexLocked(old *Note) {
	for _, a := range old.Aliases {
		k := strings.ToLower(a)
		v.byAls[k] = removeStr(v.byAls[k], old.Path)
		if len(v.byAls[k]) == 0 {
			delete(v.byAls, k)
		}
	}
}

func (v *Vault) removeLocked(rel string) {
	old := v.notes[rel]
	if old == nil {
		return
	}
	v.unindexLocked(old)
	delete(v.notes, rel)
	delete(v.stamp, rel)
	v.dropBase(rel)
	v.gen++
}

func baseKey(rel string) string {
	b := path.Base(rel)
	if isMarkdown(b) {
		b = b[:len(b)-3]
	}
	return strings.ToLower(b)
}

func (v *Vault) addBase(rel string) {
	k := baseKey(rel)
	v.byBase[k] = appendUnique(v.byBase[k], rel)
}

func (v *Vault) dropBase(rel string) {
	k := baseKey(rel)
	v.byBase[k] = removeStr(v.byBase[k], rel)
	if len(v.byBase[k]) == 0 {
		delete(v.byBase, k)
	}
}

func appendUnique(s []string, x string) []string {
	for _, e := range s {
		if e == x {
			return s
		}
	}
	return append(s, x)
}

func removeStr(s []string, x string) []string {
	out := s[:0]
	for _, e := range s {
		if e != x {
			out = append(out, e)
		}
	}
	return out
}

// Refresh re-reads one note from disk and updates the index, returning the
// fresh Note. Use it to open a note before the scan reaches it and after
// editing it. If the file no longer exists it is removed from the index
// and an fs.ErrNotExist error is returned.
func (v *Vault) Refresh(rel string) (*Note, error) {
	rel, err := cleanRel(rel)
	if err != nil {
		return nil, err
	}
	abs := v.Abs(rel)
	fi, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			v.mu.Lock()
			v.removeLocked(rel)
			v.mu.Unlock()
		}
		return nil, err
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("%s is a directory", rel)
	}
	if !isMarkdown(rel) {
		v.mu.Lock()
		if _, ok := v.files[rel]; !ok {
			v.files[rel] = struct{}{}
			v.addBase(rel)
			v.gen++
		}
		v.mu.Unlock()
		return nil, fmt.Errorf("%s is not a Markdown note", rel)
	}
	rs := v.nextSeq()
	n, err := readNote(abs, rel, fi)
	if err != nil {
		return nil, err
	}
	if !v.put(n, rs) {
		if cur, ok := v.Note(rel); ok {
			return cur, nil // a later read already indexed it
		}
	}
	return n, nil
}

// Note returns the indexed note at rel, if known.
func (v *Vault) Note(rel string) (*Note, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	n, ok := v.notes[rel]
	return n, ok
}

// Len returns the number of indexed notes.
func (v *Vault) Len() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.notes)
}

// Notes returns all indexed notes sorted by path.
func (v *Vault) Notes() []*Note {
	v.mu.RLock()
	out := make([]*Note, 0, len(v.notes))
	for _, n := range v.notes {
		out = append(out, n)
	}
	v.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// RecentNotes returns all indexed notes, most recently modified first
// (ties by path).
func (v *Vault) RecentNotes() []*Note {
	out := v.Notes()
	sort.SliceStable(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out
}

// Files returns the vault-relative paths of indexed non-Markdown files
// (attachments), sorted.
func (v *Vault) Files() []string {
	v.mu.RLock()
	out := make([]string, 0, len(v.files))
	for f := range v.files {
		out = append(out, f)
	}
	v.mu.RUnlock()
	sort.Strings(out)
	return out
}

// ReadNote reads the raw bytes of a vault note with its version token
// (pass the token to WriteNote).
func (v *Vault) ReadNote(rel string) ([]byte, StatToken, error) {
	rel, err := cleanRel(rel)
	if err != nil {
		return nil, StatToken{}, err
	}
	return ReadFile(v.Abs(rel))
}

// WriteNote atomically writes a vault note (see WriteFile for the conflict
// rules) and refreshes it in the index.
func (v *Vault) WriteNote(rel string, data []byte, expect *StatToken) (StatToken, error) {
	rel, err := cleanRel(rel)
	if err != nil {
		return StatToken{}, err
	}
	tok, err := WriteFile(v.Abs(rel), data, expect)
	if err != nil {
		return tok, err
	}
	if isMarkdown(rel) {
		_, _ = v.Refresh(rel)
	}
	return tok, nil
}

// ---- .folioignore ----

// loadIgnore reads the top-level .folioignore: one glob per line, '#'
// comments and blank lines ignored.
func loadIgnore(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, ".folioignore"))
	if err != nil {
		return nil
	}
	var pats []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pats = append(pats, line)
	}
	return pats
}

// ignored reports whether rel matches a .folioignore pattern. Semantics
// (gitignore-like, as the design is silent): a pattern without '/' matches
// the name of a file or directory at any depth; a pattern containing '/'
// is matched against the whole relative path (a leading '/' is optional);
// a trailing '/' restricts the pattern to directories; "**" matches any
// number of path segments. Ignoring a directory skips its whole subtree.
func (v *Vault) ignored(rel string, isDir bool) bool {
	for _, p := range v.ignore {
		if matchIgnore(p, rel, isDir) {
			return true
		}
	}
	return false
}

func matchIgnore(pat, rel string, isDir bool) bool {
	dirOnly := strings.HasSuffix(pat, "/")
	pat = strings.TrimSuffix(pat, "/")
	if dirOnly && !isDir {
		return false
	}
	if pat == "" {
		return false
	}
	if !strings.Contains(pat, "/") {
		ok, _ := path.Match(pat, path.Base(rel))
		return ok
	}
	pat = strings.TrimPrefix(pat, "/")
	return matchSegments(strings.Split(pat, "/"), strings.Split(rel, "/"))
}

func matchSegments(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			pat = pat[1:]
			if len(pat) == 0 {
				return true
			}
			for i := 0; i <= len(segs); i++ {
				if matchSegments(pat, segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], segs[0]); !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}
