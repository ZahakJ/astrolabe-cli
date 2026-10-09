package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
	"github.com/ZahakJ/astrolabe-cli/internal/vault"
)

// verbFunc runs a verb with its arguments (global flags already removed).
type verbFunc func(a *app, args []string) error

// verbs are the reserved words of DESIGN.md §4.1. They are matched exactly:
// unique prefixes are deliberately not accepted, because a note may be
// called "to".
var verbs map[string]verbFunc

func init() {
	verbs = map[string]verbFunc{
		"add":       (*app).cmdAdd,
		"new":       (*app).cmdNew,
		"today":     (*app).cmdToday,
		"find":      (*app).cmdFind,
		"ls":        (*app).cmdLs,
		"pick":      (*app).cmdPick,
		"tasks":     (*app).cmdTasks,
		"tags":      (*app).cmdTags,
		"links":     (*app).cmdLinks,
		"backlinks": (*app).cmdBacklinks,
		"render":    (*app).cmdRender,
		"export":    (*app).cmdExport,
		"path":      (*app).cmdPath,
		"vault":     (*app).cmdVault,
		"learn":     (*app).cmdLearn,
		"doctor":    (*app).cmdDoctor,
		"help":      (*app).cmdHelp,
		"version":   (*app).cmdVersion,
	}
}

// IsVerb reports whether word is a reserved verb.
func IsVerb(word string) bool {
	_, ok := verbs[word]
	return ok
}

// Verbs returns the reserved verbs in the order of DESIGN.md §4.1.
func Verbs() []string {
	return append([]string(nil), verbOrder...)
}

var verbOrder = []string{
	"add", "new", "today", "find", "ls", "pick", "tasks", "tags", "links",
	"backlinks", "render", "export", "path", "vault", "learn", "doctor", "help",
	"version",
}

func (a *app) run() int {
	g, err := parseGlobals(a.env.Args)
	if err != nil {
		return a.finish(err)
	}
	a.g = g
	if g.version {
		return a.finish(a.cmdVersion(nil))
	}
	rest := g.rest
	if len(rest) == 0 {
		if g.help {
			return a.finish(a.cmdHelp(nil))
		}
		return a.finish(a.openDefault())
	}
	first := rest[0]
	if first == "--" {
		// "astrolabe -- add" opens a note called add.
		if len(rest) == 1 {
			return a.finish(a.openDefault())
		}
		if g.help {
			return a.finish(a.cmdHelp(nil))
		}
		return a.finish(a.openNote(rest[1:]))
	}
	if fn, ok := verbs[first]; ok {
		if g.help {
			if first == "help" {
				return a.finish(a.cmdHelp(rest[1:]))
			}
			return a.finish(a.cmdHelp([]string{first}))
		}
		return a.finish(fn(a, rest[1:]))
	}
	if g.help {
		return a.finish(a.cmdHelp(nil))
	}
	if first == "-" {
		if len(rest) > 1 {
			return a.finish(usagef("", "unexpected argument %q after -", rest[1]))
		}
		return a.finish(a.openStdin())
	}
	if strings.HasPrefix(first, "-") {
		return a.finish(usagef("", "unknown flag %s", first))
	}
	return a.finish(a.openNote(rest))
}

// config loads the optional config file once.
func (a *app) config() *vault.Config {
	if a.cfg != nil {
		return a.cfg
	}
	cfg, err := vault.LoadConfig(vault.ConfigFile(a.getenv), a.getenv)
	if err != nil || cfg == nil {
		// An unreadable config must not stop the tool (principle 2);
		// `astrolabe doctor` reports it.
		cfg, _ = vault.LoadConfig("", a.getenv)
		if cfg == nil {
			cfg = &vault.Config{}
		}
	}
	a.cfg = cfg
	return cfg
}

// resolveRoot picks the vault root per DESIGN.md §3. A remembered vault
// that no longer exists is skipped with a one-line warning on stderr.
func (a *app) resolveRoot() (vault.Root, error) {
	if a.root.Dir != "" {
		return a.root, nil
	}
	cfgDir, _ := a.config().FileValue("dir")
	r, err := vault.ResolveRoot(vault.RootInputs{
		Flag:   a.g.dir,
		Env:    a.envDir(),
		Config: cfgDir,
		Cwd:    a.env.Cwd,
		Home:   a.env.Home,
	})
	if err != nil {
		if a.g.dir != "" {
			return r, usagef("", "-C: %v", err)
		}
		return r, err
	}
	if r.Stale != "" && !a.quietStale && r.Source != vault.RootFromFlag && r.Source != vault.RootFromEnv {
		a.errorf("the remembered vault %s; skipping it (`astrolabe vault` to change)", a.staleShown(r))
	}
	a.root = r
	return r, nil
}

// envDir is $ASTROLABE_DIR (or $FOLIO_DIR, the former name).
func (a *app) envDir() string {
	return firstNonEmpty(a.getenv("ASTROLABE_DIR"), a.getenv("FOLIO_DIR"))
}

// staleShown is Root.Stale with the path home-relative.
func (a *app) staleShown(r vault.Root) string {
	if strings.HasPrefix(r.Stale, r.Remembered) {
		return vault.DisplayPath(r.Remembered, a.env.Home) + r.Stale[len(r.Remembered):]
	}
	return r.Stale
}

// vault opens the vault (without scanning it).
func (a *app) vault() (*vault.Vault, error) {
	if a.v != nil {
		return a.v, nil
	}
	r, err := a.resolveRoot()
	if err != nil {
		return nil, err
	}
	v, err := a.openVaultAt(r)
	if err == nil {
		a.rooted = true
	}
	return v, err
}

func (a *app) openVaultAt(r vault.Root) (*vault.Vault, error) {
	v, err := vault.Open(r.Dir)
	if err != nil {
		return nil, err
	}
	cfg := a.config()
	v.SetDailyConfig(vault.DailyConfig{
		Folder: cfg.String("daily_dir", ""),
		Format: cfg.String("daily_format", ""),
	})
	a.root = r
	a.v = v
	a.rooted = false
	a.scanned = false
	return v, nil
}

// scannedVault opens the vault and runs the full scan (for verbs that need
// the whole index: ls, tags, tasks, links, backlinks, name resolution).
func (a *app) scannedVault() (*vault.Vault, error) {
	v, err := a.vault()
	if err != nil {
		return nil, err
	}
	if !a.scanned {
		if err := v.Scan(context.Background()); err != nil {
			return nil, fmt.Errorf("scanning %s: %w", v.Root(), err)
		}
		a.scanned = true
	}
	return v, nil
}

// display resolves theme, glyphs and terminal capabilities once.
func (a *app) display() *Display {
	if a.disp != nil {
		return a.disp
	}
	cfg := a.config()
	name := a.g.theme
	if name == "" {
		name = cfg.String("theme", theme.DefaultName)
	}
	th, ok := theme.Lookup(name)
	if !ok {
		th = theme.DefaultTheme()
	}
	bidi := a.g.bidi
	if bidi == "" {
		bidi = cfg.String("bidi", "auto")
	}
	opts := term.Options{
		Getenv: a.getenv,
		Color:  a.g.color,
		ASCII:  a.g.ascii || cfg.Bool("ascii", false),
		Bidi:   bidi,
	}
	detect := opts
	tty := a.env.StdoutTTY
	detect.TTY = &tty
	detect.Width = a.env.Width
	if a.env.Width > 0 || !isStdout(a.env.Stdout) {
		// Never probe os.Stdout's size for an injected writer.
		detect.File = devNull()
	}
	caps := term.Detect(detect)
	editor := strings.ToLower(cfg.String("editor", "builtin"))
	if editor != "external" {
		editor = "builtin"
	}
	measure := cfg.Int("measure", 78)
	if measure < 20 {
		measure = 78
	}
	a.disp = &Display{
		ThemeName: th.Name,
		Theme:     th,
		Glyphs:    theme.GlyphsFor(!caps.UTF8),
		Ground:    cfg.Bool("ground", true),
		Measure:   measure,
		Mouse:     cfg.Bool("mouse", true),
		Editor:    editor,
		Term:      opts,
		Caps:      caps,
	}
	return a.disp
}

func isStdout(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && f == os.Stdout
}

var devNullFile *os.File

func devNull() *os.File {
	if devNullFile == nil {
		devNullFile, _ = os.Open(os.DevNull)
	}
	return devNullFile
}

// painter returns the styler for human output on stdout.
func (a *app) painter() *painter {
	if a.pt == nil {
		d := a.display()
		a.pt = newPainter(a.styledStdout(), d)
		a.pt.human = a.human()
	}
	return a.pt
}

// human reports whether stdout gets human-oriented output.
func (a *app) human() bool { return a.env.StdoutTTY }

// styledStdout reports whether escape sequences may be written to stdout:
// it is a terminal that is not TERM=dumb, or --color forced a profile. A
// dumb terminal (Emacs shell buffers, some CI logs, serial consoles) prints
// escape sequences as garbage, so it gets the plain text a pipe would get
// (DESIGN.md §1, "degrade, never break"); the human layout, which
// depends only on StdoutTTY, is kept.
func (a *app) styledStdout() bool {
	if _, forced, _ := term.ParseProfile(a.g.color); forced {
		return true
	}
	return a.env.StdoutTTY && !a.dumbTerm()
}

// dumbTerm reports TERM=dumb.
func (a *app) dumbTerm() bool { return a.getenv("TERM") == "dumb" }

// out writes human or machine output to stdout. Human lines are in visual
// order (painter.visual); on a terminal that reverses right-to-left runs
// itself (bidi=runs) each line is passed through term.RunsLine, the last
// step before the terminal, after all styling.
func (a *app) out(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	if a.human() && text.MayHaveRTLRuns(s) && a.display().Caps.Encoder().BidiRuns {
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			lines[i] = term.RunsLine(l)
		}
		s = strings.Join(lines, "\n")
	}
	io.WriteString(a.env.Stdout, s)
}

// raw writes to stdout untransformed: paths printed for scripts and
// copying, in logical order even on a terminal.
func (a *app) raw(format string, args ...any) {
	fmt.Fprintf(a.env.Stdout, format, args...)
}

// shownPath is how a vault-relative path is printed: vault-relative in a
// terminal; in a pipe, relative to the working directory when that is inside
// the vault, else absolute, so the path can always be opened as printed.
func (a *app) shownPath(rel string) string {
	if a.human() {
		return rel
	}
	return a.openablePath(rel)
}

// openablePath is the machine form of shownPath.
func (a *app) openablePath(rel string) string {
	abs := a.absPath(rel)
	root := a.root.Dir
	if root != "" && a.env.Cwd != "" && within(root, a.env.Cwd) {
		if r, err := filepath.Rel(a.env.Cwd, abs); err == nil {
			return r
		}
	}
	return abs
}

// absPath returns the absolute path of a vault-relative path.
func (a *app) absPath(rel string) string {
	if a.v != nil {
		return a.v.Abs(rel)
	}
	return filepath.Join(a.root.Dir, filepath.FromSlash(rel))
}

// within reports whether p is root or below it.
func within(root, p string) bool {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return r == "." || (r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)))
}

// readStdin reads all of stdin when it is not a terminal; ok is false when
// stdin is a terminal (nothing to read without blocking on the user).
func (a *app) readStdin() ([]byte, bool, error) {
	if a.env.StdinTTY {
		return nil, false, nil
	}
	b, err := io.ReadAll(a.env.Stdin)
	if err != nil {
		return nil, true, fmt.Errorf("reading stdin: %w", err)
	}
	return b, true, nil
}

func (a *app) cmdVersion(args []string) error {
	if len(args) > 0 {
		return usagef("version", "version takes no arguments")
	}
	v := a.env.Version
	if v == "" {
		v = "dev"
	}
	a.out("astrolabe %s (%s/%s, %s)\n", v, runtime.GOOS, runtime.GOARCH, runtime.Version())
	return nil
}
