package vault

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// StatToken identifies the version of a file that a caller read. Pass it
// back to WriteFile so the write is refused if the file changed in the
// meantime. The zero StatToken (Exists false) means "the file did not exist
// when read".
type StatToken struct {
	Exists  bool
	ModTime time.Time
	Size    int64
	Hash    [sha256.Size]byte
}

// String is a short human-readable form for messages and debugging.
func (t StatToken) String() string {
	if !t.Exists {
		return "absent"
	}
	return fmt.Sprintf("%d bytes @ %s (%x)", t.Size, t.ModTime.Format(time.RFC3339Nano), t.Hash[:4])
}

// tokenFor builds the token of data read from a file with info fi.
func tokenFor(data []byte, fi fs.FileInfo) StatToken {
	return StatToken{Exists: true, ModTime: fi.ModTime(), Size: fi.Size(), Hash: sha256.Sum256(data)}
}

// ConflictError is returned by WriteFile when the file on disk no longer
// matches the StatToken the caller read. The caller should offer reload or
// overwrite (overwrite = WriteFile with a nil token).
type ConflictError struct {
	Path     string
	Expected StatToken
	Actual   StatToken
}

func (e *ConflictError) Error() string {
	switch {
	case e.Expected.Exists && !e.Actual.Exists:
		return fmt.Sprintf("%s was deleted since it was read", e.Path)
	case !e.Expected.Exists && e.Actual.Exists:
		return fmt.Sprintf("%s was created by someone else since it was read", e.Path)
	}
	return fmt.Sprintf("%s changed on disk since it was read", e.Path)
}

// ErrConflict matches any *ConflictError with errors.Is.
var ErrConflict = errors.New("file changed on disk")

// Is makes errors.Is(err, ErrConflict) true for conflicts.
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// ReadFile reads a file and returns its raw bytes (BOM, CRLF and all) with
// the token identifying this version. A missing file is reported as an
// fs.ErrNotExist error together with a zero token, which WriteFile accepts
// as "must still be absent".
func ReadFile(name string) ([]byte, StatToken, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, StatToken{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, StatToken{}, err
	}
	if fi.IsDir() {
		return nil, StatToken{}, fmt.Errorf("%s is a directory", name)
	}
	var buf bytes.Buffer
	buf.Grow(int(fi.Size()) + 1)
	if _, err := buf.ReadFrom(f); err != nil {
		return nil, StatToken{}, err
	}
	data := buf.Bytes()
	return data, tokenFor(data, fi), nil
}

// currentToken stats and hashes the file as it is now.
func currentToken(name string) (StatToken, error) {
	data, tok, err := ReadFile(name)
	_ = data
	if errors.Is(err, fs.ErrNotExist) {
		return StatToken{}, nil
	}
	return tok, err
}

// sameVersion compares tokens. The content hash decides: a file touched
// without changes is not a conflict, and a same-size edit within the
// filesystem's timestamp granularity still is.
func sameVersion(a, b StatToken) bool {
	if a.Exists != b.Exists {
		return false
	}
	if !a.Exists {
		return true
	}
	return a.Size == b.Size && a.Hash == b.Hash
}

// WriteFile atomically replaces name with data: it writes a temporary file
// in the same directory, fsyncs it, gives it the original file's mode (0644
// for a new file), renames it over the target and fsyncs the directory.
// When name is a symlink the write goes through to the link's target, so
// the link survives. Missing parent directories are created.
//
// If expect is non-nil the write is refused with a *ConflictError when the
// file on disk differs from the version expect describes. Pass nil to
// overwrite unconditionally. The new token is returned.
func WriteFile(name string, data []byte, expect *StatToken) (StatToken, error) {
	target := name
	if resolved, err := filepath.EvalSymlinks(name); err == nil {
		target = resolved
	} else if fi, lerr := os.Lstat(name); lerr == nil && fi.Mode()&fs.ModeSymlink != 0 {
		// Dangling symlink: write to where it points.
		if dest, rerr := os.Readlink(name); rerr == nil {
			if !filepath.IsAbs(dest) {
				dest = filepath.Join(filepath.Dir(name), dest)
			}
			target = dest
		}
	}
	if expect != nil {
		cur, err := currentToken(target)
		if err != nil {
			return StatToken{}, err
		}
		if !sameVersion(*expect, cur) {
			return StatToken{}, &ConflictError{Path: name, Expected: *expect, Actual: cur}
		}
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return StatToken{}, err
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(target); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".folio-*.tmp")
	if err != nil {
		return StatToken{}, err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return StatToken{}, err
	}
	if err := tmp.Chmod(mode); err != nil {
		return StatToken{}, err
	}
	if err := tmp.Sync(); err != nil {
		return StatToken{}, err
	}
	if err := tmp.Close(); err != nil {
		return StatToken{}, err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return StatToken{}, err
	}
	ok = true
	syncDir(dir)
	fi, err := os.Stat(target)
	if err != nil {
		return StatToken{}, err
	}
	return tokenFor(data, fi), nil
}

// createExclusive writes a new file atomically and fails with fs.ErrExist
// if name already exists. It writes a temp file and hard-links it into
// place (link fails if the name exists), falling back to O_EXCL on
// filesystems without hard links.
func createExclusive(name string, data []byte) error {
	dir := filepath.Dir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".folio-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	_, werr := tmp.Write(data)
	if werr == nil {
		werr = tmp.Chmod(0o644)
	}
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	if err := os.Link(tmpName, name); err == nil {
		syncDir(dir)
		return nil
	} else if errors.Is(err, fs.ErrExist) {
		return err
	}
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync() // best effort; not supported everywhere
		d.Close()
	}
}
