package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxNameBytes keeps file names well under the common 255-byte limit,
// leaving room for " 99" and ".md".
const maxNameBytes = 200

// SafeFilename turns a note title into a file name stem (without ".md")
// that is valid on Linux, macOS and Windows and safe inside a wikilink:
// path separators and the characters \ / : * ? " < > | # ^ [ ] are
// replaced by spaces, control characters dropped, runs of whitespace
// collapsed, leading/trailing spaces and dots trimmed. All other Unicode,
// Arabic included, is kept as written. Reserved Windows device names get a
// trailing underscore; an empty result becomes "Untitled".
func SafeFilename(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case strings.ContainsRune(`\/:*?"<>|#^[]`, r):
			b.WriteByte(' ')
		case unicode.IsControl(r) || r == utf8.RuneError || r == '\uFEFF':
			if r == '\t' || r == '\n' || r == '\r' {
				b.WriteByte(' ')
			}
		case unicode.IsSpace(r):
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	s := strings.Join(strings.Fields(b.String()), " ")
	s = strings.Trim(s, " .")
	if len(s) > maxNameBytes {
		cut := maxNameBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = strings.Trim(s[:cut], " .")
	}
	if s == "" {
		return "Untitled"
	}
	stem := strings.ToUpper(s)
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	switch stem {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		s += "_"
	}
	return s
}

// NewNoteOptions configure CreateNote.
type NewNoteOptions struct {
	// Dir is the vault-relative folder to create the note in ("" = root).
	Dir string
	// Body is appended after the "# Title" heading and a blank line; a
	// missing final newline is added.
	Body []byte
}

// CreateNote creates a note for title and returns its vault-relative path.
// The file name is SafeFilename(title) + ".md"; an existing file is never
// overwritten: " 2", " 3", … are appended until the name is free. The file
// starts with "# Title" (the title as given), a blank line, then the body.
// Folders are created as needed.
func (v *Vault) CreateNote(title string, opts NewNoteOptions) (string, error) {
	title = strings.TrimSpace(strings.Join(strings.Fields(title), " "))
	if title == "" {
		return "", errors.New("a note needs a title")
	}
	dir := ""
	if d := strings.Trim(strings.ReplaceAll(opts.Dir, "\\", "/"), "/"); d != "" && d != "." {
		c, err := cleanRel(d)
		if err != nil {
			return "", err
		}
		dir = c
	}
	content := "# " + title + "\n\n"
	if len(opts.Body) > 0 {
		body := string(opts.Body)
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		content += body
	}
	stem := SafeFilename(title)
	for i := 1; i < 1000; i++ {
		name := stem + ".md"
		if i > 1 {
			name = fmt.Sprintf("%s %d.md", stem, i)
		}
		rel := path.Join(dir, name)
		if v.existsCaseInsensitive(rel) {
			continue
		}
		err := createExclusive(v.Abs(rel), []byte(content))
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, _ = v.Refresh(rel)
		return rel, nil
	}
	return "", fmt.Errorf("no free file name for %q", title)
}

// existsCaseInsensitive guards against creating "note.md" next to an
// indexed "Note.md": harmless on Linux but a collision on macOS/Windows
// and ambiguous for wikilinks.
func (v *Vault) existsCaseInsensitive(rel string) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	lr := strings.ToLower(rel)
	for _, p := range v.byBase[baseKey(rel)] {
		if strings.ToLower(p) == lr {
			return true
		}
	}
	return false
}
