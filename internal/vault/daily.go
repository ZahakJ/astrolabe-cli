package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DailyConfig says where daily notes live.
type DailyConfig struct {
	// Folder is the vault-relative folder ("" = vault root).
	Folder string
	// Format is the file name format in Moment.js tokens, without ".md".
	// It may contain '/' to place notes in sub-folders.
	Format string
}

// DefaultDailyConfig is daily/YYYY-MM-DD.md.
var DefaultDailyConfig = DailyConfig{Folder: "daily", Format: "YYYY-MM-DD"}

// LoadDailyConfig reads .obsidian/daily-notes.json under root ("folder"
// and "format" keys); missing keys or file fall back to
// DefaultDailyConfig. An Obsidian config with an empty folder means the
// vault root, as in Obsidian.
func LoadDailyConfig(root string) DailyConfig {
	data, err := os.ReadFile(filepath.Join(root, ".obsidian", "daily-notes.json"))
	if err != nil {
		return DefaultDailyConfig
	}
	var raw struct {
		Folder *string `json:"folder"`
		Format *string `json:"format"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return DefaultDailyConfig
	}
	c := DailyConfig{Folder: "", Format: DefaultDailyConfig.Format}
	if raw.Folder != nil {
		c.Folder = strings.Trim(strings.TrimSpace(*raw.Folder), "/")
	}
	if raw.Format != nil && strings.TrimSpace(*raw.Format) != "" {
		c.Format = strings.TrimSpace(*raw.Format)
	}
	return c
}

// DailyConfig returns the daily-note configuration in effect.
func (v *Vault) DailyConfig() DailyConfig {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.daily
}

// SetDailyConfig overrides the daily-note configuration (used for the
// daily_dir / daily_format config keys, which take precedence over
// .obsidian/daily-notes.json). Empty fields keep the current values.
func (v *Vault) SetDailyConfig(c DailyConfig) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if c.Folder != "" {
		v.daily.Folder = strings.Trim(c.Folder, "/")
	}
	if c.Format != "" {
		v.daily.Format = c.Format
	}
}

var (
	monthNames = [...]string{"January", "February", "March", "April", "May", "June", "July",
		"August", "September", "October", "November", "December"}
	dayNames = [...]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
)

// FormatMoment formats t with a Moment.js format string. Supported tokens:
// YYYY YY MMMM MMM MM M DD D dddd ddd, plus HH mm ss for completeness;
// text in [brackets] is literal and any other character (including
// unsupported Moment tokens such as week numbers) is copied as is.
// Month and weekday names are English.
func FormatMoment(format string, t time.Time) string {
	var b strings.Builder
	for i := 0; i < len(format); {
		rest := format[i:]
		if rest[0] == '[' {
			if j := strings.IndexByte(rest, ']'); j > 0 {
				b.WriteString(rest[1:j])
				i += j + 1
				continue
			}
		}
		tok, val := "", ""
		switch {
		case strings.HasPrefix(rest, "YYYY"):
			tok, val = "YYYY", fmt.Sprintf("%04d", t.Year())
		case strings.HasPrefix(rest, "YY"):
			tok, val = "YY", fmt.Sprintf("%02d", t.Year()%100)
		case strings.HasPrefix(rest, "MMMM"):
			tok, val = "MMMM", monthNames[t.Month()-1]
		case strings.HasPrefix(rest, "MMM"):
			tok, val = "MMM", monthNames[t.Month()-1][:3]
		case strings.HasPrefix(rest, "MM"):
			tok, val = "MM", fmt.Sprintf("%02d", int(t.Month()))
		case strings.HasPrefix(rest, "M"):
			tok, val = "M", strconv.Itoa(int(t.Month()))
		case strings.HasPrefix(rest, "dddd"):
			tok, val = "dddd", dayNames[t.Weekday()]
		case strings.HasPrefix(rest, "ddd"):
			tok, val = "ddd", dayNames[t.Weekday()][:3]
		case strings.HasPrefix(rest, "DD"):
			tok, val = "DD", fmt.Sprintf("%02d", t.Day())
		case strings.HasPrefix(rest, "D"):
			tok, val = "D", strconv.Itoa(t.Day())
		case strings.HasPrefix(rest, "HH"):
			tok, val = "HH", fmt.Sprintf("%02d", t.Hour())
		case strings.HasPrefix(rest, "mm"):
			tok, val = "mm", fmt.Sprintf("%02d", t.Minute())
		case strings.HasPrefix(rest, "ss"):
			tok, val = "ss", fmt.Sprintf("%02d", t.Second())
		}
		if tok == "" {
			b.WriteByte(rest[0])
			i++
			continue
		}
		b.WriteString(val)
		i += len(tok)
	}
	return b.String()
}

// DailyPath returns the vault-relative path of the daily note for t.
func (v *Vault) DailyPath(t time.Time) string {
	c := v.DailyConfig()
	name := FormatMoment(c.Format, t) + ".md"
	if c.Folder == "" {
		return path.Clean(name)
	}
	return path.Clean(c.Folder + "/" + name)
}

// DailyHeading is the first line of a new daily note:
// "# Saturday, 3 October 2026".
func DailyHeading(t time.Time) string {
	return fmt.Sprintf("# %s, %d %s %d", dayNames[t.Weekday()], t.Day(), monthNames[t.Month()-1], t.Year())
}

// EnsureDaily returns the daily note for t, creating it (and its folders)
// with the DailyHeading when missing.
func (v *Vault) EnsureDaily(t time.Time) (rel string, created bool, err error) {
	rel, err = cleanRel(v.DailyPath(t))
	if err != nil {
		return "", false, fmt.Errorf("daily note format: %w", err)
	}
	abs := v.Abs(rel)
	if _, err := os.Stat(abs); err == nil {
		if _, ok := v.Note(rel); !ok {
			_, _ = v.Refresh(rel)
		}
		return rel, false, nil
	}
	err = createExclusive(abs, []byte(DailyHeading(t)+"\n\n"))
	if errors.Is(err, fs.ErrExist) {
		return rel, false, nil
	}
	if err != nil {
		return "", false, err
	}
	_, _ = v.Refresh(rel)
	return rel, true, nil
}

// CaptureOptions configure Capture.
type CaptureOptions struct {
	// Task captures "- [ ] text" instead of "- HH:MM text".
	Task bool
	// Target is the vault-relative path of the note to append to; empty
	// means today's daily note (created if missing). A missing target note
	// is created empty.
	Target string
	// Heading, if set, inserts the line at the end of that section instead
	// of the end of the file. The heading is matched case-insensitively and
	// loosely (as heading links are); if no such heading exists it is
	// appended as "## Heading" followed by the line.
	Heading string
	// Now is the capture time (zero = time.Now()). It picks the daily note
	// and the HH:MM stamp.
	Now time.Time
}

// CaptureResult says where a capture landed.
type CaptureResult struct {
	Path string // vault-relative note path
	Line int    // 1-based line of the captured item
}

// CaptureLine formats text as the list item Capture writes, without a line
// terminator. Multi-line text becomes one item: the first non-blank line
// follows the marker and every further non-blank line is a continuation
// indented by two spaces (blank lines are dropped so the item stays one
// tight list item and trailing whitespace is trimmed).
func CaptureLine(text string, task bool, now time.Time) string {
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		l = strings.TrimRight(l, " \t\r")
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	first := strings.TrimSpace(lines[0])
	var b strings.Builder
	if task {
		b.WriteString("- [ ] ")
	} else {
		b.WriteString("- ")
		b.WriteString(now.Format("15:04"))
		b.WriteString(" ")
	}
	b.WriteString(first)
	for _, l := range lines[1:] {
		b.WriteString("\n  ")
		b.WriteString(strings.TrimSpace(l))
	}
	return strings.TrimRight(b.String(), " ")
}

// Capture appends one item to today's daily note (or opts.Target),
// creating the note and its folders as needed. Appending adds exactly the
// item's newline-terminated line(s) and changes nothing else, except that a
// file not ending in a newline first gets one so the item starts on its own
// line. The file's line ending style (LF or CRLF) is kept. The write is
// atomic and conflict-checked against the version just read.
func (v *Vault) Capture(text string, opts CaptureOptions) (CaptureResult, error) {
	if strings.TrimSpace(text) == "" {
		return CaptureResult{}, errors.New("nothing to capture")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	rel := opts.Target
	if rel == "" {
		r, _, err := v.EnsureDaily(now)
		if err != nil {
			return CaptureResult{}, err
		}
		rel = r
	}
	rel, err := cleanRel(rel)
	if err != nil {
		return CaptureResult{}, err
	}
	data, tok, err := v.ReadNote(rel)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return CaptureResult{}, err
	}
	item := CaptureLine(text, opts.Task, now)
	out, line := insertItem(data, item, opts.Heading)
	if _, err := v.WriteNote(rel, out, &tok); err != nil {
		return CaptureResult{}, err
	}
	return CaptureResult{Path: rel, Line: line}, nil
}

// lineEnding returns "\r\n" if data's first line ends in CRLF, else "\n".
func lineEnding(data []byte) string {
	if i := bytes.IndexByte(data, '\n'); i > 0 && data[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

// insertItem returns data with item inserted (at the end, or at the end of
// the section under heading) and the 1-based line of the item.
func insertItem(data []byte, item, heading string) ([]byte, int) {
	nl := lineEnding(data)
	item = strings.ReplaceAll(item, "\n", nl)
	if heading != "" {
		if out, line, ok := insertUnderHeading(data, item, heading, nl); ok {
			return out, line
		}
	}
	out := make([]byte, 0, len(data)+len(item)+len(heading)+16)
	out = append(out, data...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, nl...)
	}
	if heading != "" {
		// Heading not found: add it, separated by a blank line.
		if len(out) > 0 && !bytes.HasSuffix(out, []byte(nl+nl)) {
			out = append(out, nl...)
		}
		out = append(out, "## "+strings.TrimSpace(strings.TrimLeft(heading, "#"))+nl+nl...)
	}
	line := bytes.Count(out, []byte("\n")) + 1
	out = append(out, item+nl...)
	return out, line
}

// insertUnderHeading inserts item after the last non-blank line of the
// section headed by heading (the section ends at the next heading of the
// same or a higher level).
func insertUnderHeading(data []byte, item, heading, nl string) ([]byte, int, bool) {
	n := parseNote("x.md", data, time.Time{}, int64(len(data)))
	want := headingKey(strings.TrimLeft(heading, "# "))
	idx := -1
	for i, h := range n.Headings {
		if headingKey(h.Text) == want {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, 0, false
	}
	h := n.Headings[idx]
	end := n.Lines + 1 // line number where the next section starts
	for _, h2 := range n.Headings[idx+1:] {
		if h2.Level <= h.Level {
			end = h2.Line
			break
		}
	}
	// Find the last non-blank line in (h.Line, end).
	lines := splitLinesKeep(data)
	insertAfter := h.Line
	for ln := end - 1; ln > h.Line; ln-- {
		if ln-1 < len(lines) && strings.TrimSpace(string(lines[ln-1])) != "" {
			insertAfter = ln
			break
		}
	}
	var out []byte
	for i := 0; i < insertAfter && i < len(lines); i++ {
		out = append(out, lines[i]...)
	}
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, nl...)
	}
	if insertAfter == h.Line {
		// Empty section: keep a blank line between heading and item.
		out = append(out, nl...)
	}
	line := bytes.Count(out, []byte("\n")) + 1
	out = append(out, item+nl...)
	rest := lines[min(insertAfter, len(lines)):]
	if insertAfter == h.Line && len(rest) > 0 && strings.TrimSpace(string(rest[0])) != "" {
		out = append(out, nl...) // keep the next heading off the item
	}
	for _, l := range rest {
		out = append(out, l...)
	}
	return out, line, true
}

// splitLinesKeep splits data into lines keeping their terminators.
func splitLinesKeep(data []byte) [][]byte {
	var out [][]byte
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			out = append(out, data)
			break
		}
		out = append(out, data[:i+1])
		data = data[i+1:]
	}
	return out
}
