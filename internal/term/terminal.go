package term

import (
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultEscTimeout is how long a lone ESC waits for the rest of an escape
// sequence before it is taken as the Escape key (DESIGN.md §6).
const DefaultEscTimeout = 25 * time.Millisecond

// OpenOptions configures Open.
type OpenOptions struct {
	// Options overrides capability detection (colour, glyphs, bidi, size).
	Options
	// Mouse enables mouse reporting (wheel, press, release) on open.
	Mouse bool
	// KittyKeyboard asks kitty-protocol terminals for unambiguous key
	// reports (CSI u). Terminals without the protocol ignore the request.
	KittyKeyboard bool
	// FocusEvents enables focus in/out reporting.
	FocusEvents bool
	// EscTimeout overrides DefaultEscTimeout.
	EscTimeout time.Duration
	// TTYPath is the terminal device to use; default "/dev/tty", so the
	// TUI works when stdin or stdout is a pipe.
	TTYPath string
	// OnSignal, when set, runs when a termination signal (SIGHUP when the
	// terminal goes away, SIGTERM, or an external SIGINT/SIGQUIT) is about
	// to end the process: before the terminal is restored and the signal
	// re-raised with its default action. It runs on the signal goroutine,
	// so it must synchronise with the application (the TUI hands the work
	// to its event loop); it gets SignalHookTimeout to finish.
	OnSignal func(sig os.Signal)
}

// SignalHookTimeout bounds how long a termination signal waits for
// OpenOptions.OnSignal before the process exits anyway.
const SignalHookTimeout = 5 * time.Second

// EditorCommand builds the command that opens file at line (1-based; 0 for
// none) in the user's editor: $VISUAL, else $EDITOR, else vi. The editor
// string is run through /bin/sh so values such as "emacsclient -t" or
// "code --wait" work. The line is passed as "+N" to editors known to accept
// it (vi, vim, nvim, nano, emacs, micro, kak, …) and as "file:N" to helix;
// other editors just get the file. Run it with Terminal.RunExternal.
func EditorCommand(file string, line int) *exec.Cmd {
	return editorCommand(os.Getenv, file, line)
}

func editorCommand(env func(string) string, file string, line int) *exec.Cmd {
	ed := strings.TrimSpace(env("VISUAL"))
	if ed == "" {
		ed = strings.TrimSpace(env("EDITOR"))
	}
	if ed == "" {
		ed = "vi"
	}
	base := ""
	if f := strings.Fields(ed); len(f) > 0 {
		base = filepath.Base(f[0])
	}
	var args []string
	switch {
	case line > 0 && plusLineEditors[base]:
		args = []string{"+" + strconv.Itoa(line), file}
	case line > 0 && (base == "hx" || base == "helix"):
		args = []string{file + ":" + strconv.Itoa(line)}
	case line > 0 && (base == "code" || base == "codium" || base == "subl" || base == "zed"):
		args = []string{"-g", file + ":" + strconv.Itoa(line)}
	default:
		args = []string{file}
	}
	return exec.Command("/bin/sh", append([]string{"-c", ed + ` "$@"`, "folio-editor"}, args...)...)
}

var plusLineEditors = map[string]bool{
	"vi": true, "vim": true, "nvim": true, "gvim": true, "view": true, "nano": true, "pico": true,
	"emacs": true, "emacsclient": true, "micro": true, "kak": true, "joe": true, "jed": true,
	"ne": true, "mg": true, "vis": true, "ed": true, "ex": true, "elvis": true, "nvi": true,
	"busybox": false,
}

// ErrClipboardUnsupported is returned by Copy when the terminal probably
// ignores OSC 52.
var ErrClipboardUnsupported = errors.New("terminal may not support OSC 52 clipboard")

// OSC52 returns the escape sequence that sets the clipboard to text.
func OSC52(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
}
