package term

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Profile is a colour capability level.
type Profile int

// Colour profiles, from least to most capable.
const (
	ProfileNone      Profile = iota // no colour: attributes only
	Profile16                       // the 16 ANSI colours, terminal palette
	Profile256                      // xterm 256-colour palette
	ProfileTrueColor                // 24-bit colour
)

// String returns the --color spelling of p.
func (p Profile) String() string {
	switch p {
	case ProfileNone:
		return "none"
	case Profile16:
		return "16"
	case Profile256:
		return "256"
	case ProfileTrueColor:
		return "truecolor"
	}
	return fmt.Sprintf("Profile(%d)", int(p))
}

// ParseProfile parses a --color value: "auto" (returns ok=false with no
// error), "truecolor"/"24bit", "256", "16", "none"/"off"/"never".
func ParseProfile(s string) (p Profile, forced bool, err error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return ProfileNone, false, nil
	case "truecolor", "24bit", "true", "full", "always":
		return ProfileTrueColor, true, nil
	case "256", "8bit":
		return Profile256, true, nil
	case "16", "ansi", "basic":
		return Profile16, true, nil
	case "none", "off", "never", "no", "0":
		return ProfileNone, true, nil
	}
	return ProfileNone, false, fmt.Errorf("invalid colour mode %q (want auto, truecolor, 256, 16 or none)", s)
}

// Caps describes what the terminal can do. Build it with Detect.
type Caps struct {
	Profile Profile
	// UTF8 reports a UTF-8 locale; when false, use the ASCII glyph set.
	UTF8 bool
	// Bidi reports whether astrolabe should reorder and shape RTL text itself
	// (true: BidiOn and BidiRuns) or emit logical order for a terminal that
	// does bidi (false: BidiOff).
	Bidi bool
	// BidiMode is the chosen right-to-left behaviour and BidiWhy says why
	// (for astrolabe doctor).
	BidiMode BidiMode
	BidiWhy  string
	// Hyperlinks reports OSC 8 support (a guess from the environment).
	Hyperlinks bool
	// Clipboard reports OSC 52 support (a guess from the environment).
	Clipboard bool
	// StyledUnderline reports support for curly/dotted underlines (SGR 4:3,
	// 4:4); otherwise they degrade to a plain underline.
	StyledUnderline bool
	// Tmux and Screen report running inside those multiplexers.
	Tmux, Screen bool
	// TTY reports whether the output is a terminal.
	TTY bool
	// Width and Height are the terminal size in cells (80×24 if unknown).
	Width, Height int
	// Term is $TERM; Program is $TERM_PROGRAM (or a detected name).
	Term, Program string
}

// Encoder returns the SGR encoder matching these capabilities.
func (c Caps) Encoder() Encoder {
	return Encoder{Profile: c.Profile, StyledUnderline: c.StyledUnderline, Hyperlinks: c.Hyperlinks,
		BidiRuns: c.BidiMode == BidiRuns && c.TTY}
}

// Options overrides detection. Zero values mean "detect".
type Options struct {
	// Getenv reads environment variables; nil means os.Getenv. Tests pass
	// a map-backed function.
	Getenv func(string) string
	// Color is the --color flag: "auto", "truecolor", "256", "16", "none".
	Color string
	// ASCII forces the ASCII glyph set (UTF8=false).
	ASCII bool
	// Bidi is the --bidi flag: "auto", "on", "off", "runs".
	Bidi string
	// TmuxClientTerm returns the TERM of the terminal attached to the
	// current tmux session ("" if unknown); nil asks tmux itself (see
	// tmuxClientTerm). Detection uses it only inside tmux, on a terminal,
	// with --bidi auto.
	TmuxClientTerm func() string
	// TTY overrides terminal detection of the output (nil = detect from
	// File, or stdout).
	TTY *bool
	// File is the output whose terminal-ness and size are probed; nil
	// means os.Stdout.
	File *os.File
	// Width and Height override the probed size when non-zero.
	Width, Height int
	// GOOS overrides runtime.GOOS (for tests).
	GOOS string
}

// Detect works out terminal capabilities from the environment, the output
// file and the overrides in o. Precedence for colour: an explicit --color
// wins; then NO_COLOR (any non-empty value) and TERM=dumb or a non-terminal
// output mean none; then COLORTERM=truecolor/24bit, known truecolour
// terminals, TERM containing "256color", else 16 colours.
func Detect(o Options) Caps {
	env := o.Getenv
	if env == nil {
		env = os.Getenv
	}
	goos := o.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	f := o.File
	if f == nil {
		f = os.Stdout
	}
	c := Caps{Term: env("TERM"), Program: env("TERM_PROGRAM")}
	if o.TTY != nil {
		c.TTY = *o.TTY
	} else {
		c.TTY = isTerminal(f)
	}
	c.Tmux = env("TMUX") != "" || strings.HasPrefix(c.Term, "tmux")
	c.Screen = !c.Tmux && (env("STY") != "" || strings.HasPrefix(c.Term, "screen"))

	// Colour.
	if p, forced, err := ParseProfile(o.Color); err == nil && forced {
		c.Profile = p
	} else {
		c.Profile = detectProfile(env, c.TTY)
	}

	// Locale and glyphs.
	c.UTF8 = !o.ASCII && detectUTF8(env, goos)

	// Bidi.
	if m, forced, err := ParseBidi(o.Bidi); err == nil && forced {
		c.BidiMode, c.BidiWhy = m, "set by --bidi or config bidi"
	} else {
		query := o.TmuxClientTerm
		if query == nil {
			query = func() string { return tmuxClientTerm(env("TMUX")) }
		}
		// tmux sets TERM to tmux* or screen* in its panes; a TMUX variable
		// with another TERM was inherited by a terminal started from a tmux
		// pane, and its server says nothing about this terminal.
		inTmux := env("TMUX") != "" && (strings.HasPrefix(c.Term, "tmux") || strings.HasPrefix(c.Term, "screen"))
		c.BidiMode, c.BidiWhy = bidiAuto(env, c.TTY && inTmux, query)
	}
	c.Bidi = c.BidiMode != BidiOff

	c.Hyperlinks = detectHyperlinks(env, c)
	c.Clipboard = detectClipboard(env, c)
	c.StyledUnderline = detectStyledUnderline(env, c)

	// Size.
	c.Width, c.Height = 0, 0
	if c.TTY {
		if w, h, err := getSize(f); err == nil {
			c.Width, c.Height = w, h
		}
	}
	if c.Width <= 0 {
		c.Width = envInt(env, "COLUMNS")
	}
	if c.Height <= 0 {
		c.Height = envInt(env, "LINES")
	}
	if c.Width <= 0 {
		c.Width = 80
	}
	if c.Height <= 0 {
		c.Height = 24
	}
	if o.Width > 0 {
		c.Width = o.Width
	}
	if o.Height > 0 {
		c.Height = o.Height
	}
	return c
}

func envInt(env func(string) string, key string) int {
	n, err := strconv.Atoi(strings.TrimSpace(env(key)))
	if err != nil {
		return 0
	}
	return n
}

func detectProfile(env func(string) string, tty bool) Profile {
	term := strings.ToLower(env("TERM"))
	if env("NO_COLOR") != "" || term == "dumb" || !tty {
		return ProfileNone
	}
	ct := strings.ToLower(env("COLORTERM"))
	if ct == "truecolor" || ct == "24bit" {
		return ProfileTrueColor
	}
	switch env("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm", "vscode", "ghostty", "Hyper", "Tabby", "rio":
		return ProfileTrueColor
	case "Apple_Terminal":
		return Profile256
	}
	if env("KITTY_WINDOW_ID") != "" || env("WT_SESSION") != "" || env("ALACRITTY_WINDOW_ID") != "" ||
		env("WEZTERM_EXECUTABLE") != "" {
		return ProfileTrueColor
	}
	for _, s := range []string{"-direct", "truecolor", "24bit", "kitty", "ghostty", "alacritty", "foot", "wezterm"} {
		if strings.Contains(term, s) {
			return ProfileTrueColor
		}
	}
	if strings.Contains(term, "256color") || strings.Contains(ct, "256") {
		return Profile256
	}
	return Profile16
}

// detectUTF8 applies POSIX locale precedence: the first non-empty of
// LC_ALL, LC_CTYPE, LANG decides. On macOS an entirely unset locale in a GUI
// terminal is treated as UTF-8 (Terminal.app and iTerm2 are UTF-8 by
// default even when LANG is not exported).
func detectUTF8(env func(string) string, goos string) bool {
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := env(k); v != "" {
			v = strings.ToLower(v)
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return goos == "darwin" && env("TERM_PROGRAM") != ""
}

// BidiMode is how right-to-left text reaches the terminal (DESIGN.md §6).
type BidiMode int

// Bidi modes.
const (
	// BidiOn: astrolabe shapes Arabic and reorders each line into visual
	// order, for terminals that do no bidi (most of them).
	BidiOn BidiMode = iota
	// BidiOff: logical order, untouched, for terminals that implement bidi
	// themselves.
	BidiOff
	// BidiRuns: as BidiOn, but every stretch of right-to-left letters that
	// the terminal will reverse itself (kitty reverses each such run while
	// shaping) is emitted pre-reversed, so the terminal's reversal restores
	// the visual order. See text.RTLRuns.
	BidiRuns
)

// String returns the --bidi spelling of m.
func (m BidiMode) String() string {
	switch m {
	case BidiOn:
		return "on"
	case BidiOff:
		return "off"
	case BidiRuns:
		return "runs"
	}
	return fmt.Sprintf("BidiMode(%d)", int(m))
}

// ParseBidi parses a --bidi value: "auto" or "" (forced=false, no error),
// "on", "off" or "runs".
func ParseBidi(s string) (m BidiMode, forced bool, err error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return BidiOn, false, nil
	case "on", "true", "yes", "1":
		return BidiOn, true, nil
	case "off", "false", "no", "0":
		return BidiOff, true, nil
	case "runs":
		return BidiRuns, true, nil
	}
	return BidiOn, false, fmt.Errorf("invalid bidi mode %q (want auto, on, off or runs)", s)
}

// bidiAuto is DESIGN.md §6 for --bidi auto: runs for kitty, off for the
// terminals known to implement bidi themselves (VTE, Konsole, Apple
// Terminal, mlterm), else on. Inside tmux the environment describes the
// terminal the session was started from, which may not be the one attached
// now; when askTmux is set, query asks tmux for the attached client's TERM
// and that answer decides between kitty and the rest.
func bidiAuto(env func(string) string, askTmux bool, query func() string) (BidiMode, string) {
	term := strings.ToLower(env("TERM"))
	kittyHints := true
	if askTmux {
		if ct := strings.TrimSpace(query()); ct != "" {
			if strings.Contains(strings.ToLower(ct), "kitty") {
				return BidiRuns, "tmux client terminal is " + ct + " (kitty reverses right-to-left runs)"
			}
			kittyHints = false // KITTY_WINDOW_ID etc. are left over from another terminal
		}
	}
	if strings.Contains(term, "kitty") {
		return BidiRuns, "TERM=" + env("TERM") + " (kitty reverses right-to-left runs)"
	}
	for _, other := range []string{"foot", "alacritty", "wezterm", "ghostty", "st-", "rxvt", "linux", "mlterm", "contour", "rio"} {
		if strings.HasPrefix(term, other) {
			kittyHints = false // TERM names another terminal: kitty variables are inherited
		}
	}
	switch {
	case env("VTE_VERSION") != "":
		return BidiOff, "VTE_VERSION is set (the terminal does bidi)"
	case env("KONSOLE_VERSION") != "":
		return BidiOff, "KONSOLE_VERSION is set (the terminal does bidi)"
	case env("TERM_PROGRAM") == "Apple_Terminal":
		return BidiOff, "TERM_PROGRAM=Apple_Terminal (the terminal does bidi)"
	case strings.HasPrefix(term, "mlterm"):
		return BidiOff, "TERM=" + env("TERM") + " (the terminal does bidi)"
	}
	if kittyHints {
		switch {
		case env("KITTY_WINDOW_ID") != "":
			return BidiRuns, "KITTY_WINDOW_ID is set (kitty reverses right-to-left runs)"
		case strings.EqualFold(env("TERM_PROGRAM"), "kitty"):
			return BidiRuns, "TERM_PROGRAM=" + env("TERM_PROGRAM") + " (kitty reverses right-to-left runs)"
		case strings.EqualFold(env("TERMINAL_EMULATOR"), "kitty"):
			return BidiRuns, "TERMINAL_EMULATOR=" + env("TERMINAL_EMULATOR") + " (kitty reverses right-to-left runs)"
		}
	}
	return BidiOn, "default: the terminal is not known to do bidi"
}

func tmuxAtLeast(env func(string) string, major, minor int) bool {
	if env("TERM_PROGRAM") != "tmux" {
		return false
	}
	v := env("TERM_PROGRAM_VERSION") // e.g. "3.4", "3.3a", "next-3.5"
	v = strings.TrimPrefix(v, "next-")
	parts := strings.SplitN(v, ".", 2)
	if len(parts) != 2 {
		return false
	}
	ma, err1 := strconv.Atoi(parts[0])
	mi := 0
	for _, ch := range parts[1] {
		if ch < '0' || ch > '9' {
			break
		}
		mi = mi*10 + int(ch-'0')
	}
	if err1 != nil {
		return false
	}
	return ma > major || (ma == major && mi >= minor)
}

// detectHyperlinks guesses OSC 8 support. Unknown terminals get false: a
// terminal that does not parse OSC may print the sequence as text.
func detectHyperlinks(env func(string) string, c Caps) bool {
	if !c.TTY || c.Screen {
		return false
	}
	if c.Tmux {
		return tmuxAtLeast(env, 3, 4)
	}
	switch c.Program {
	case "iTerm.app", "WezTerm", "vscode", "ghostty", "Hyper", "Tabby", "rio":
		return true
	case "Apple_Terminal":
		return false
	}
	if env("KITTY_WINDOW_ID") != "" || env("WT_SESSION") != "" || env("WEZTERM_EXECUTABLE") != "" {
		return true
	}
	if v := envInt(env, "VTE_VERSION"); v >= 5000 {
		return true
	}
	if env("KONSOLE_VERSION") != "" {
		return true
	}
	t := strings.ToLower(c.Term)
	for _, s := range []string{"kitty", "foot", "ghostty", "wezterm", "alacritty"} {
		if strings.Contains(t, s) {
			return true
		}
	}
	return false
}

// detectClipboard guesses OSC 52 support. tmux handles OSC 52 itself
// (set-clipboard) without passthrough; VTE, Konsole and Apple Terminal
// ignore it.
func detectClipboard(env func(string) string, c Caps) bool {
	if !c.TTY || c.Screen {
		return false
	}
	if c.Tmux {
		return true
	}
	if env("VTE_VERSION") != "" || c.Program == "Apple_Terminal" {
		return false
	}
	if strings.HasPrefix(c.Term, "linux") || c.Term == "dumb" {
		return false
	}
	switch c.Program {
	case "iTerm.app", "WezTerm", "vscode", "ghostty", "Tabby", "rio":
		return true
	}
	if env("KITTY_WINDOW_ID") != "" || env("WT_SESSION") != "" || env("ALACRITTY_WINDOW_ID") != "" {
		return true
	}
	t := strings.ToLower(c.Term)
	for _, s := range []string{"xterm", "kitty", "foot", "alacritty", "ghostty", "wezterm", "st-"} {
		if strings.Contains(t, s) {
			return true
		}
	}
	return false
}

// detectStyledUnderline guesses support for SGR 4:3 / 4:4. tmux parses the
// sub-parameters itself and degrades them for outer terminals that lack
// them, so it is safe there.
func detectStyledUnderline(env func(string) string, c Caps) bool {
	if !c.TTY || c.Screen || c.Profile == ProfileNone {
		return false
	}
	if c.Tmux {
		return true
	}
	if env("VTE_VERSION") != "" || env("KITTY_WINDOW_ID") != "" || env("WEZTERM_EXECUTABLE") != "" {
		return true
	}
	switch c.Program {
	case "iTerm.app", "WezTerm", "vscode", "ghostty", "rio":
		return true
	}
	t := strings.ToLower(c.Term)
	for _, s := range []string{"kitty", "foot", "ghostty", "wezterm", "alacritty"} {
		if strings.Contains(t, s) {
			return true
		}
	}
	return false
}
