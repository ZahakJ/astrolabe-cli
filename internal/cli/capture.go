package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ZahakJ/folio/internal/term"
	"github.com/ZahakJ/folio/internal/vault"
)

// cmdAdd is `folio add TEXT…`: capture one item into today's daily note.
func (a *app) cmdAdd(args []string) error {
	fs := newFlagSet("add")
	task := fs.Bool("-t", "--task")
	target := fs.String("-n", "--note")
	at := fs.String("", "--at")
	pos, err := fs.Parse(args)
	if err != nil {
		return err
	}
	txt := strings.Join(pos, " ")
	if len(pos) == 0 {
		b, ok, err := a.readStdin()
		if err != nil {
			return err
		}
		if !ok {
			return usagef("add", "nothing to capture: give the text as arguments or on stdin")
		}
		txt = string(b)
	}
	if strings.TrimSpace(txt) == "" {
		return usagef("add", "nothing to capture: the text is empty")
	}
	v, err := a.vault()
	if err != nil {
		return err
	}
	opts := vault.CaptureOptions{Task: *task, Heading: strings.TrimSpace(*at), Now: a.now()}
	if *target != "" {
		rel, err := a.exactNote(*target)
		if err != nil {
			return err
		}
		opts.Target = rel
	}
	res, err := v.Capture(txt, opts)
	if err != nil {
		return fmt.Errorf("capture failed: %w", err)
	}
	if !a.human() {
		a.out("%s:%d\n", a.openablePath(res.Path), res.Line)
		return nil
	}
	p := a.painter()
	item := vault.CaptureLine(txt, *task, opts.Now)
	if i := strings.IndexByte(item, '\n'); i >= 0 {
		item = item[:i] + " " + p.g.Ellipsis
	}
	a.out("%s %s %s%s\n", p.accent(p.g.Brand), p.text("Added to"), p.pathInk(res.Path), p.faint(fmt.Sprintf(":%d", res.Line)))
	a.out("  %s\n", p.muted(p.visual(item)))
	return nil
}

// exactNote resolves the -n target of add without fuzzy matching: an
// existing file path, a vault path, a title/basename/alias; anything else
// names a new note at that vault-relative path (".md" added), which Capture
// creates.
func (a *app) exactNote(name string) (string, error) {
	if ref, ok, err := a.resolveFileArg(name); ok || err != nil {
		return ref.Rel, err
	}
	v, err := a.vault()
	if err != nil {
		return "", err
	}
	if rel, ok := statNote(v.Root(), name); ok {
		return rel, nil
	}
	if v, err = a.scannedVault(); err != nil {
		return "", err
	}
	if rel, ok := v.ResolveName(name); ok {
		return rel, nil
	}
	rel, err := vaultRelDir(v.Root(), name)
	if err != nil || rel == "" {
		return "", usagef("add", "-n: %q is not a note name inside the vault", name)
	}
	if !strings.EqualFold(filepath.Ext(rel), ".md") {
		rel += ".md"
	}
	a.infof("creating %s", rel)
	return rel, nil
}

// vaultRelDir turns a user-given folder or path into a clean vault-relative
// slash path, refusing anything that escapes the vault.
func vaultRelDir(root, p string) (string, error) {
	p = strings.TrimSpace(p)
	if filepath.IsAbs(p) {
		if !within(root, p) {
			return "", fmt.Errorf("%s is outside the vault %s", p, root)
		}
		r, err := filepath.Rel(root, p)
		if err != nil {
			return "", err
		}
		p = r
	}
	c := filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
	if c == "." {
		return "", nil
	}
	if c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("%s is outside the vault", p)
	}
	return c, nil
}

// cmdNew is `folio new TITLE…`: create a note and print its path.
func (a *app) cmdNew(args []string) error {
	fs := newFlagSet("new")
	open := fs.Bool("-o", "--open")
	edit := fs.Bool("-e", "--edit")
	dir := fs.String("-d", "--dir")
	pos, err := fs.Parse(args)
	if err != nil {
		return err
	}
	title := strings.TrimSpace(strings.Join(pos, " "))
	if title == "" {
		return usagef("new", "a title is required")
	}
	if *open && *edit {
		return usagef("new", "-o and -e are exclusive: choose the built-in editor or $EDITOR")
	}
	if *open && a.env.Hooks.RunTUI == nil {
		return fmt.Errorf("new -o: the interactive editor is %w; use -e for $EDITOR", ErrNotBuilt)
	}
	var body []byte
	if b, ok, err := a.readStdin(); err != nil {
		return err
	} else if ok && len(strings.TrimSpace(string(b))) > 0 {
		body = b
	}
	v, err := a.vault()
	if err != nil {
		return err
	}
	d := ""
	if *dir != "" {
		if d, err = vaultRelDir(v.Root(), *dir); err != nil {
			return usagef("new", "-d: %v", err)
		}
	}
	rel, err := v.CreateNote(title, vault.NewNoteOptions{Dir: d, Body: body})
	if err != nil {
		return fmt.Errorf("creating note: %w", err)
	}
	abs := v.Abs(rel)
	switch {
	case *edit:
		// Line 3 is the first body line, after "# Title" and a blank line.
		if err := runEditor(abs, 3); err != nil {
			return fmt.Errorf("created %s, but the editor failed: %w", abs, err)
		}
	case *open:
		req := a.tuiRequest(ModeEdit)
		req.Path = rel
		if _, err := a.env.Hooks.RunTUI(context.Background(), req); err != nil && !errors.Is(err, ErrCancelled) {
			return err
		}
	}
	if a.human() {
		a.out("%s\n", a.painter().accent(abs))
	} else {
		a.out("%s\n", abs)
	}
	return nil
}

// runEditor opens file at line in $VISUAL/$EDITOR (default vi), connected to
// the controlling terminal so that it works inside $(…) and with piped stdin.
func runEditor(file string, line int) error {
	cmd := term.EditorCommand(file, line)
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		defer tty.Close()
		cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	} else {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stderr, os.Stderr
	}
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return fmt.Errorf("editor exited with status %d", ee.ExitCode())
		}
		return err
	}
	return nil
}

// cmdToday is `folio today`: open today's daily note, or print its path.
func (a *app) cmdToday(args []string) error {
	fs := newFlagSet("today")
	pathOnly := fs.Bool("-p", "--print")
	pos, err := fs.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("today", "unexpected argument %q", pos[0])
	}
	if !*pathOnly && a.env.Hooks.RunTUI == nil {
		return fmt.Errorf("today: the interactive reader is %w; 'folio today -p' prints the path", ErrNotBuilt)
	}
	v, err := a.vault()
	if err != nil {
		return err
	}
	rel, created, err := v.EnsureDaily(a.now())
	if err != nil {
		return fmt.Errorf("creating today's note: %w", err)
	}
	if *pathOnly {
		if created {
			a.infof("created %s", rel)
		}
		a.out("%s\n", v.Abs(rel))
		return nil
	}
	req := a.tuiRequest(ModeToday)
	req.Path = rel
	return a.runTUI(req)
}
