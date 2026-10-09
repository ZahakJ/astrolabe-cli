// Package cli implements astrolabe's shell face: the dispatcher for the command
// line and every non-interactive verb of DESIGN.md §4.1 (add, new, today -p,
// find, ls, tags, tasks, links, backlinks, path, doctor, help, version).
//
// The interactive entry points (astrolabe, astrolabe NOTE, astrolabe -, astrolabe today,
// astrolabe pick, astrolabe new -o) and the render/export verbs live in packages that
// cli does not import. They are reached through Hooks, which cmd/astrolabe fills
// in; a nil hook makes the verb fail with ErrNotBuilt before any side effect.
//
// Conventions (DESIGN.md §4.1):
//   - results go to stdout, messages to stderr;
//   - when stdout is not a terminal the output is machine-readable: no
//     colour, no decoration, stable "path:line:text" or tab-separated forms;
//   - exit codes: 0 success, 1 nothing found (or a runtime failure), 2 usage
//     error, 130 cancelled;
//   - the global flags -C/--dir, --theme, --color, --ascii and --bidi may
//     appear before or after the verb; "--" ends flag parsing;
//   - verbs are reserved words matched exactly (no prefixes); anything else
//     names a note, and "./add.md" opens a file with a reserved name.
//
// Paths: in a terminal, notes are shown by their vault-relative path. In a
// pipe they are printed so they can be opened from the current directory:
// relative to it when it is inside the vault, absolute otherwise. Verbs that
// print a single path for scripting (new, today -p, path, pick) always print
// it absolute.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
	"github.com/ZahakJ/astrolabe-cli/internal/vault"
)

// Exit codes.
const (
	ExitOK        = 0   // success
	ExitNotFound  = 1   // nothing found, or the operation failed
	ExitUsage     = 2   // bad command line
	ExitCancelled = 130 // the user cancelled (picker Esc, Ctrl-C)
)

// ErrNotBuilt is returned by a verb whose implementation (a Hook) is not
// linked into this binary.
var ErrNotBuilt = errors.New("not built into this binary yet")

// ErrCancelled may be returned by a hook when the user cancelled; the verb
// then exits with ExitCancelled and prints nothing.
var ErrCancelled = errors.New("cancelled")

// Env is everything a run of astrolabe reads from the outside world, so that the
// whole command line can be exercised in-process by tests.
type Env struct {
	// Args are the command-line arguments without the program name.
	Args []string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// StdinTTY, StdoutTTY and StderrTTY report whether the streams are
	// terminals. StdoutTTY selects human (styled, aligned) or
	// machine-readable output; StdinTTY decides whether stdin is read as
	// input for add/new/render; StderrTTY decides whether informational
	// "nothing found" messages are printed.
	StdinTTY, StdoutTTY, StderrTTY bool

	// Getenv reads the environment (nil = os.Getenv).
	Getenv func(string) string
	// Cwd and Home are absolute paths (empty = os.Getwd / os.UserHomeDir).
	Cwd, Home string
	// Now returns the current time (nil = time.Now). It picks the daily
	// note, capture stamps and the Overdue/Today/Upcoming split.
	Now func() time.Time

	// Width overrides the terminal width used for human output (0 = detect).
	Width int

	// Version is the build version printed by `astrolabe version`.
	Version string

	// Hooks connect the verbs implemented outside this package.
	Hooks Hooks
}

// Hooks are the entry points implemented by other packages (internal/tui,
// internal/render, internal/export). cmd/astrolabe fills them in. A nil hook
// makes the verbs that need it fail with ErrNotBuilt (exit 1) before any
// side effect such as creating a note.
type Hooks struct {
	// RunTUI runs the interactive application. For ModePick it returns the
	// chosen vault-relative path in TUIResult.Path (the CLI prints it);
	// returning ErrCancelled exits 130.
	RunTUI func(ctx context.Context, req TUIRequest) (TUIResult, error)
	// Render writes req.Source rendered for the terminal (or as plain text)
	// to req.Out.
	Render func(ctx context.Context, req RenderRequest) error
	// Export writes req.Source as one standalone HTML document to req.Out.
	Export func(ctx context.Context, req ExportRequest) error
}

// Display carries the presentation settings resolved from flags, the
// environment and the config file, for the hooks to use.
type Display struct {
	// ThemeName is the selected theme (flag --theme, ASTROLABE_THEME, config
	// theme, else the default) and Theme its authored 24-bit tokens.
	// Renderers apply term.ThemeFor(Theme, Caps.Profile) and, when Ground
	// is false, Theme.WithoutGround().
	ThemeName string
	Theme     theme.Theme
	// Glyphs is the glyph set (ASCII when --ascii, config ascii, or a
	// non-UTF-8 locale).
	Glyphs theme.Glyphs
	// Ground reports config ground (default on).
	Ground bool
	// Measure is config measure (default 78).
	Measure int
	// Mouse reports config mouse (default on).
	Mouse bool
	// Editor is config editor: "builtin" (default) or "external".
	Editor string
	// Term holds the overrides to pass to term.Detect / term.Open (colour,
	// ASCII, bidi and the environment); TTY and File are left unset.
	Term term.Options
	// Caps are the capabilities detected for stdout with those overrides.
	Caps term.Caps
}

// TUIMode says what the interactive application should open on.
type TUIMode int

// TUI modes, one per interactive entry point of DESIGN.md §4.1.
const (
	// ModeDefault is plain `astrolabe`. The CLI has already chosen the note:
	// today's daily note if it exists, else the most recent note of this
	// vault from the recent file (Path and Line set), else Path is empty
	// and the home screen should be shown.
	ModeDefault TUIMode = iota
	// ModeOpen is `astrolabe NOTE`: open Path (at Line when non-zero).
	ModeOpen
	// ModeStdin is `astrolabe -`: page Source (read from stdin) in the reader.
	// Path is empty; Name is "-".
	ModeStdin
	// ModeToday is `astrolabe today`: Path is today's daily note, already
	// created if it was missing.
	ModeToday
	// ModePick is `astrolabe pick [QUERY]`: show the fuzzy note picker with
	// Query pre-filled, return the chosen path, ErrCancelled on Esc.
	ModePick
	// ModeEdit is `astrolabe new -o`: open the freshly created Path in the
	// built-in editor.
	ModeEdit
)

// String names the mode.
func (m TUIMode) String() string {
	switch m {
	case ModeDefault:
		return "default"
	case ModeOpen:
		return "open"
	case ModeStdin:
		return "stdin"
	case ModeToday:
		return "today"
	case ModePick:
		return "pick"
	case ModeEdit:
		return "edit"
	}
	return fmt.Sprintf("TUIMode(%d)", int(m))
}

// TUIRequest is what RunTUI is asked to do.
type TUIRequest struct {
	Mode TUIMode
	// Vault is open but not scanned yet (StartScan has been called, so the
	// index fills in concurrently; paint first, per DESIGN.md §1.3).
	Vault *vault.Vault
	// Root is how the vault root was chosen. In single-file mode (a file
	// outside any vault was named) Root.Dir is the file's vault per
	// vault.ResolveFileRoot.
	Root vault.Root
	// Path is the vault-relative note to open ("" = home screen).
	Path string
	// Line is the 1-based source line to place the cursor on (0 = top, or
	// the remembered position).
	Line int
	// Source is the Markdown read from stdin for ModeStdin.
	Source []byte
	// Name is a display name for Source ("-").
	Name string
	// Query pre-fills the picker for ModePick.
	Query string
	// Config is the loaded configuration (never nil).
	Config *vault.Config
	// RecentFile is the path of the recent-notes state file.
	RecentFile string
	// Home is the home directory, for showing the vault as "~/notes".
	Home string
	// StdoutTTY reports whether stdout is a terminal (the TUI itself always
	// draws on /dev/tty).
	StdoutTTY bool
	// Display holds theme, glyphs and terminal overrides.
	Display Display
}

// TUIResult is what RunTUI reports back.
type TUIResult struct {
	// Path is the vault-relative note chosen in ModePick.
	Path string
}

// RenderRequest asks Render to typeset one Markdown document.
type RenderRequest struct {
	// Source is the raw Markdown.
	Source []byte
	// Name is the file name as given on the command line, or "-".
	Name string
	// Path is the vault-relative path when the file is a note of Vault,
	// else "".
	Path string
	// Vault is the vault the document belongs to (or the current vault for
	// stdin), open with its scan started; wait on Vault.Done() before
	// resolving links. Never nil.
	Vault *vault.Vault
	// Width is the output width in cells: -w COLS, else the terminal
	// width, else $COLUMNS, else 80.
	Width int
	// Styled selects ANSI output (stdout is a terminal, or --color was
	// forced); otherwise plain text with no escape sequences.
	Styled bool
	// Display holds theme, glyphs, caps (Caps.Encoder() for ANSI).
	Display Display
	// Out receives the output.
	Out io.Writer
}

// ExportRequest asks Export to write one standalone HTML document.
type ExportRequest struct {
	Source []byte
	// Name is the note's file name as given, or "-".
	Name string
	// Path is the vault-relative path of the note ("" for stdin).
	Path string
	// Vault is the note's vault, open with its scan started (wait on
	// Vault.Done() before resolving wikilinks). Never nil.
	Vault *vault.Vault
	// Display holds the theme; export may ignore terminal settings.
	Display Display
	// Out receives the HTML. With -o FILE the CLI buffers it and writes the
	// file atomically afterwards.
	Out io.Writer
}

// Run executes one astrolabe command line and returns the process exit code.
func Run(env Env) int {
	a := newApp(env)
	return a.run()
}

// app is one run's state.
type app struct {
	env    Env
	getenv func(string) string
	now    func() time.Time

	g globals

	// Lazily resolved.
	cfg  *vault.Config
	root vault.Root
	v    *vault.Vault
	disp *Display
	pt   *painter
	// firstArg is set while resolving `astrolabe NOTE` (the first argument),
	// where an unknown word may be a mistyped verb.
	firstArg bool
	scanned  bool
	// rooted is set when a.v is the vault ResolveRoot chose (not the vault
	// of a single file opened from outside it): only that one is
	// remembered (DESIGN.md §3).
	rooted bool
	// noRemember stops this run from remembering its vault (doctor, vault).
	noRemember bool
	// quietStale stops resolveRoot from warning about a stale remembered
	// vault (the verb reports it itself).
	quietStale bool
}

func newApp(env Env) *app {
	a := &app{env: env}
	a.getenv = env.Getenv
	if a.getenv == nil {
		a.getenv = os.Getenv
	}
	a.now = env.Now
	if a.now == nil {
		a.now = time.Now
	}
	if a.env.Stdin == nil {
		a.env.Stdin = strings.NewReader("")
	}
	if a.env.Stdout == nil {
		a.env.Stdout = io.Discard
	}
	if a.env.Stderr == nil {
		a.env.Stderr = io.Discard
	}
	if a.env.Cwd == "" {
		a.env.Cwd, _ = os.Getwd()
	}
	if a.env.Home == "" {
		a.env.Home, _ = os.UserHomeDir()
	}
	return a
}

// usageError is a command-line mistake (exit 2).
type usageError struct {
	msg  string
	verb string // verb whose usage line to print ("" = general)
}

func (e *usageError) Error() string { return e.msg }

func usagef(verb, format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...), verb: verb}
}

// notFound is "nothing found" (exit 1). An empty message prints nothing.
type notFound struct{ msg string }

func (e *notFound) Error() string { return e.msg }

// exitCode maps a verb's error to the exit code and prints its message.
func (a *app) finish(err error) int {
	a.afterRun()
	if err == nil {
		return ExitOK
	}
	var ue *usageError
	var nf *notFound
	switch {
	case errors.Is(err, ErrCancelled), errors.Is(err, context.Canceled):
		return ExitCancelled
	case errors.As(err, &ue):
		a.errorf("%s", ue.msg)
		if line := usageLine(ue.verb); line != "" {
			fmt.Fprintf(a.env.Stderr, "usage: %s\n", line)
		}
		if ue.verb != "" {
			fmt.Fprintf(a.env.Stderr, "Run 'astrolabe help %s' for details.\n", ue.verb)
		} else {
			fmt.Fprintf(a.env.Stderr, "Run 'astrolabe help' for the list of commands.\n")
		}
		return ExitUsage
	case errors.As(err, &nf):
		if nf.msg != "" {
			a.errorf("%s", nf.msg)
		}
		return ExitNotFound
	}
	a.errorf("%v", err)
	return ExitNotFound
}

// errorf prints "astrolabe: message" on stderr.
func (a *app) errorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(a.env.Stderr, "astrolabe: %s\n", strings.TrimRight(msg, "\n"))
}

// infof prints an informational message on stderr only when stderr is a
// terminal (so pipelines stay quiet).
func (a *app) infof(format string, args ...any) {
	if a.env.StderrTTY {
		fmt.Fprintf(a.env.Stderr, "astrolabe: %s\n", fmt.Sprintf(format, args...))
	}
}
