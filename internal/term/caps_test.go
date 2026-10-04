package term

import (
	"strings"
	"testing"
)

func envOf(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

func detect(o Options, tty bool, kv ...string) Caps {
	o.Getenv = envOf(kv...)
	o.TTY = &tty
	if o.GOOS == "" {
		o.GOOS = "linux"
	}
	return Detect(o)
}

func TestDetectProfile(t *testing.T) {
	tests := []struct {
		name  string
		color string
		tty   bool
		env   []string
		want  Profile
	}{
		{"truecolor via COLORTERM", "", true, []string{"TERM", "xterm-256color", "COLORTERM", "truecolor"}, ProfileTrueColor},
		{"24bit", "", true, []string{"TERM", "xterm", "COLORTERM", "24bit"}, ProfileTrueColor},
		{"256", "", true, []string{"TERM", "xterm-256color"}, Profile256},
		{"tmux 256", "", true, []string{"TERM", "tmux-256color", "TMUX", "/tmp/x"}, Profile256},
		{"16", "", true, []string{"TERM", "xterm"}, Profile16},
		{"linux console", "", true, []string{"TERM", "linux"}, Profile16},
		{"empty TERM", "", true, nil, Profile16},
		{"NO_COLOR", "", true, []string{"TERM", "xterm-256color", "COLORTERM", "truecolor", "NO_COLOR", "1"}, ProfileNone},
		{"dumb", "", true, []string{"TERM", "dumb"}, ProfileNone},
		{"not a tty", "", false, []string{"TERM", "xterm-256color", "COLORTERM", "truecolor"}, ProfileNone},
		{"flag beats NO_COLOR", "256", true, []string{"NO_COLOR", "1"}, Profile256},
		{"flag beats pipe", "truecolor", false, nil, ProfileTrueColor},
		{"flag none", "none", true, []string{"COLORTERM", "truecolor"}, ProfileNone},
		{"flag auto", "auto", true, []string{"TERM", "xterm-256color"}, Profile256},
		{"kitty", "", true, []string{"TERM", "xterm-kitty"}, ProfileTrueColor},
		{"iterm", "", true, []string{"TERM", "xterm-256color", "TERM_PROGRAM", "iTerm.app"}, ProfileTrueColor},
		{"apple terminal", "", true, []string{"TERM", "xterm-256color", "TERM_PROGRAM", "Apple_Terminal"}, Profile256},
		{"windows terminal", "", true, []string{"TERM", "xterm-256color", "WT_SESSION", "x"}, ProfileTrueColor},
		{"direct", "", true, []string{"TERM", "xterm-direct"}, ProfileTrueColor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := detect(Options{Color: tt.color}, tt.tty, tt.env...)
			if c.Profile != tt.want {
				t.Errorf("Profile = %v, want %v", c.Profile, tt.want)
			}
		})
	}
}

func TestParseProfile(t *testing.T) {
	if _, _, err := ParseProfile("rainbow"); err == nil {
		t.Error("expected error")
	}
	if p, forced, _ := ParseProfile("16"); p != Profile16 || !forced {
		t.Error("16")
	}
	if _, forced, _ := ParseProfile("auto"); forced {
		t.Error("auto is not forced")
	}
	for _, p := range []Profile{ProfileNone, Profile16, Profile256, ProfileTrueColor} {
		q, _, err := ParseProfile(p.String())
		if err != nil || q != p {
			t.Errorf("round trip %v", p)
		}
	}
}

func TestDetectUTF8(t *testing.T) {
	tests := []struct {
		env  []string
		goos string
		want bool
	}{
		{[]string{"LANG", "en_US.UTF-8"}, "linux", true},
		{[]string{"LANG", "en_US.utf8"}, "linux", true},
		{[]string{"LANG", "C"}, "linux", false},
		{[]string{"LC_ALL", "C", "LANG", "en_US.UTF-8"}, "linux", false},               // LC_ALL wins
		{[]string{"LC_CTYPE", "de_DE.UTF-8", "LANG", "C"}, "linux", true},              // LC_CTYPE beats LANG
		{[]string{"LC_ALL", "", "LC_CTYPE", "", "LANG", "fr_FR.UTF-8"}, "linux", true}, // empty values skipped
		{nil, "linux", false},
		{[]string{"TERM_PROGRAM", "Apple_Terminal"}, "darwin", true},
		{nil, "darwin", false},
	}
	for _, tt := range tests {
		c := detect(Options{GOOS: tt.goos}, true, tt.env...)
		if c.UTF8 != tt.want {
			t.Errorf("env %v on %s: UTF8 = %v", tt.env, tt.goos, c.UTF8)
		}
	}
	if c := detect(Options{ASCII: true}, true, "LANG", "en_US.UTF-8"); c.UTF8 {
		t.Error("--ascii must force UTF8 off")
	}
}

func TestDetectBidi(t *testing.T) {
	tests := []struct {
		flag string
		env  []string
		want bool
	}{
		{"", nil, true},
		{"auto", []string{"VTE_VERSION", "7600"}, false},
		{"", []string{"KONSOLE_VERSION", "230800"}, false},
		{"", []string{"TERM_PROGRAM", "Apple_Terminal"}, false},
		{"", []string{"TERM", "mlterm-256color"}, false},
		{"", []string{"TERM", "xterm-kitty"}, true},
		{"on", []string{"VTE_VERSION", "7600"}, true},
		{"off", nil, false},
	}
	for _, tt := range tests {
		c := detect(Options{Bidi: tt.flag}, true, tt.env...)
		if c.Bidi != tt.want {
			t.Errorf("bidi=%q env %v: %v, want %v", tt.flag, tt.env, c.Bidi, tt.want)
		}
	}
}

func TestDetectFeatures(t *testing.T) {
	tests := []struct {
		name                         string
		env                          []string
		links, clip, curly, tmux, sc bool
	}{
		{"kitty", []string{"TERM", "xterm-kitty", "KITTY_WINDOW_ID", "1"}, true, true, true, false, false},
		{"vte", []string{"TERM", "xterm-256color", "VTE_VERSION", "7600"}, true, false, true, false, false},
		{"old vte", []string{"TERM", "xterm-256color", "VTE_VERSION", "4800"}, false, false, true, false, false},
		{"apple", []string{"TERM", "xterm-256color", "TERM_PROGRAM", "Apple_Terminal"}, false, false, false, false, false},
		{"tmux 3.4", []string{"TERM", "tmux-256color", "TMUX", "/s", "TERM_PROGRAM", "tmux", "TERM_PROGRAM_VERSION", "3.4"}, true, true, true, true, false},
		{"tmux 3.3a", []string{"TERM", "tmux-256color", "TMUX", "/s", "TERM_PROGRAM", "tmux", "TERM_PROGRAM_VERSION", "3.3a"}, false, true, true, true, false},
		{"screen", []string{"TERM", "screen-256color", "STY", "1.pts"}, false, false, false, false, true},
		{"plain xterm", []string{"TERM", "xterm-256color"}, false, true, false, false, false},
		{"linux console", []string{"TERM", "linux"}, false, false, false, false, false},
	}
	for _, tt := range tests {
		c := detect(Options{}, true, tt.env...)
		if c.Hyperlinks != tt.links || c.Clipboard != tt.clip || c.StyledUnderline != tt.curly || c.Tmux != tt.tmux || c.Screen != tt.sc {
			t.Errorf("%s: links=%v clip=%v curly=%v tmux=%v screen=%v", tt.name, c.Hyperlinks, c.Clipboard, c.StyledUnderline, c.Tmux, c.Screen)
		}
	}
	// Nothing is guessed for a pipe.
	c := detect(Options{}, false, "TERM", "xterm-kitty", "KITTY_WINDOW_ID", "1")
	if c.Hyperlinks || c.Clipboard || c.StyledUnderline {
		t.Error("features enabled on a pipe")
	}
}

func TestDetectSize(t *testing.T) {
	c := detect(Options{}, false, "COLUMNS", "100", "LINES", "30")
	if c.Width != 100 || c.Height != 30 {
		t.Errorf("size from env = %dx%d", c.Width, c.Height)
	}
	c = detect(Options{}, false)
	if c.Width != 80 || c.Height != 24 {
		t.Errorf("default size = %dx%d", c.Width, c.Height)
	}
	c = detect(Options{Width: 60, Height: 16}, false, "COLUMNS", "100")
	if c.Width != 60 || c.Height != 16 {
		t.Errorf("override size = %dx%d", c.Width, c.Height)
	}
}

func TestEditorCommand(t *testing.T) {
	tests := []struct {
		env  []string
		line int
		want string // the args after "sh -c <script> astrolabe-editor"
		ed   string
	}{
		{nil, 12, "+12 notes/a.md", "vi"},
		{[]string{"EDITOR", "nvim"}, 3, "+3 notes/a.md", "nvim"},
		{[]string{"EDITOR", "nano", "VISUAL", "hx"}, 7, "notes/a.md:7", "hx"},
		{[]string{"EDITOR", "emacsclient -t"}, 2, "+2 notes/a.md", "emacsclient -t"},
		{[]string{"EDITOR", "code --wait"}, 5, "-g notes/a.md:5", "code --wait"},
		{[]string{"EDITOR", "/usr/local/bin/vim"}, 0, "notes/a.md", "/usr/local/bin/vim"},
		{[]string{"EDITOR", "weird-editor"}, 9, "notes/a.md", "weird-editor"},
	}
	for _, tt := range tests {
		cmd := editorCommand(envOf(tt.env...), "notes/a.md", tt.line)
		if cmd.Args[1] != "-c" || cmd.Args[2] != tt.ed+` "$@"` || cmd.Args[3] != "astrolabe-editor" {
			t.Errorf("env %v: args %q", tt.env, cmd.Args)
			continue
		}
		if got := strings.Join(cmd.Args[4:], " "); got != tt.want {
			t.Errorf("env %v: editor args %q, want %q", tt.env, got, tt.want)
		}
	}
}

func TestDetectBidiMode(t *testing.T) {
	tests := []struct {
		name string
		flag string
		tmux string // what tmux reports as the client terminal ("" = no answer)
		env  []string
		want BidiMode
	}{
		{"default", "", "", nil, BidiOn},
		{"kitty TERM", "", "", []string{"TERM", "xterm-kitty"}, BidiRuns},
		{"kitty window", "", "", []string{"TERM", "xterm-256color", "KITTY_WINDOW_ID", "3"}, BidiRuns},
		{"TERM_PROGRAM", "", "", []string{"TERM_PROGRAM", "kitty"}, BidiRuns},
		{"TERMINAL_EMULATOR", "", "", []string{"TERMINAL_EMULATOR", "kitty"}, BidiRuns},
		{"foot started from kitty", "", "", []string{"TERM", "foot", "KITTY_WINDOW_ID", "3"}, BidiOn},
		{"VTE started from kitty", "", "", []string{"TERM", "xterm-256color", "KITTY_WINDOW_ID", "3", "VTE_VERSION", "7600"}, BidiOff},
		{"kitty started from Konsole", "", "", []string{"TERM", "xterm-kitty", "KONSOLE_VERSION", "230800"}, BidiRuns},
		{"Konsole", "", "", []string{"KONSOLE_VERSION", "230800"}, BidiOff},
		{"tmux in kitty", "", "xterm-kitty", []string{"TERM", "tmux-256color", "TMUX", "/tmp/s,1,0"}, BidiRuns},
		{"tmux, stale kitty hint", "", "foot", []string{"TERM", "tmux-256color", "TMUX", "/tmp/s,1,0", "KITTY_WINDOW_ID", "3"}, BidiOn},
		{"tmux, no answer, kitty hint", "", "", []string{"TERM", "tmux-256color", "TMUX", "/tmp/s,1,0", "KITTY_WINDOW_ID", "3"}, BidiRuns},
		{"tmux in foot", "", "foot", []string{"TERM", "tmux-256color", "TMUX", "/tmp/s,1,0"}, BidiOn},
		{"foot started from a kitty tmux pane", "", "xterm-kitty", []string{"TERM", "foot", "TMUX", "/tmp/s,1,0", "KITTY_WINDOW_ID", "3"}, BidiOn},
		{"forced on in kitty", "on", "", []string{"TERM", "xterm-kitty"}, BidiOn},
		{"forced runs", "runs", "", nil, BidiRuns},
		{"forced off", "off", "", []string{"TERM", "xterm-kitty"}, BidiOff},
	}
	for _, tt := range tests {
		asked := false
		o := Options{Bidi: tt.flag, TmuxClientTerm: func() string { asked = true; return tt.tmux }}
		c := detect(o, true, tt.env...)
		if c.BidiMode != tt.want || c.Bidi != (tt.want != BidiOff) || c.BidiWhy == "" {
			t.Errorf("%s: mode %v (%s), Bidi %v; want %v", tt.name, c.BidiMode, c.BidiWhy, c.Bidi, tt.want)
		}
		if asked && !strings.Contains(strings.Join(tt.env, " "), "tmux-256color") {
			t.Errorf("%s: asked tmux outside tmux", tt.name)
		}
		if enc := c.Encoder(); enc.BidiRuns != (tt.want == BidiRuns) {
			t.Errorf("%s: Encoder.BidiRuns = %v", tt.name, enc.BidiRuns)
		}
	}
	// No terminal: tmux is not asked and the encoder never pre-reverses.
	asked := false
	c := detect(Options{TmuxClientTerm: func() string { asked = true; return "xterm-kitty" }}, false, "TMUX", "/tmp/s,1,0", "TERM", "xterm-kitty")
	if asked || c.Encoder().BidiRuns {
		t.Errorf("pipe: asked=%v BidiRuns=%v", asked, c.Encoder().BidiRuns)
	}
	if _, _, err := ParseBidi("sideways"); err == nil {
		t.Error("ParseBidi accepted a bad mode")
	}
	if tmuxClientTerm("") != "" {
		t.Error("tmuxClientTerm without TMUX")
	}
	// A TMUX value naming no server fails quietly and quickly.
	if got := tmuxClientTerm("/nonexistent/astrolabe-test-socket,1,0"); got != "" {
		t.Errorf("tmuxClientTerm on a missing server = %q", got)
	}
}
