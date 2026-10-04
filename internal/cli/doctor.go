package cli

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
	"github.com/ZahakJ/astrolabe-cli/internal/vault"
)

// cmdDoctor is `astrolabe doctor`: what astrolabe detected, which vault and config
// it uses, and a glyph/colour test card. It always exits 0 unless the
// command line is wrong; problems are reported as warnings.
func (a *app) cmdDoctor(args []string) error {
	fs := newFlagSet("doctor")
	pos, err := fs.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("doctor", "unexpected argument %q", pos[0])
	}
	p := a.painter()
	d := a.display()
	c := d.Caps
	var warnings []string

	keyW := 18
	row := func(k, v string) {
		a.out("  %s%s\n", p.muted(text.PadRight(k, keyW)), v)
	}
	yes := func(b bool) string {
		if b {
			return p.ok("yes")
		}
		return p.faint("no")
	}
	section := func(s string) { a.out("\n%s\n", p.heading(s)) }

	v := a.env.Version
	if v == "" {
		v = "dev"
	}
	a.out("%s %s  %s\n", p.accent(p.g.Brand), p.heading("astrolabe doctor"),
		p.faint(fmt.Sprintf("%s %s %s/%s", v, p.g.Dot, runtime.GOOS, runtime.GOARCH)))

	// Terminal.
	section("Terminal")
	termName := c.Term
	if termName == "" {
		termName = "(unset)"
	}
	extra := []string{}
	if c.Program != "" {
		extra = append(extra, c.Program)
	}
	if c.Tmux && c.Program != "tmux" {
		extra = append(extra, "inside tmux")
	}
	if c.Screen {
		extra = append(extra, "inside screen")
	}
	if len(extra) > 0 {
		termName += "  " + p.faint(strings.Join(extra, ", "))
	}
	row("TERM", termName)
	stdout := "terminal"
	if !c.TTY {
		stdout = "not a terminal (plain output)"
	}
	row("stdout", stdout)
	why := ""
	if _, forced, _ := term.ParseProfile(a.g.color); forced {
		why = "set by --color"
	} else if a.getenv("NO_COLOR") != "" {
		why = "NO_COLOR is set"
	} else if c.Term == "dumb" {
		why = "TERM=dumb"
	} else if !c.TTY {
		why = "stdout is not a terminal"
	} else if ct := a.getenv("COLORTERM"); ct != "" {
		why = "COLORTERM=" + ct
	}
	prof := c.Profile.String()
	if why != "" {
		prof += "  " + p.faint(why)
	}
	row("colour", prof)
	locale := firstNonEmpty(a.getenv("LC_ALL"), a.getenv("LC_CTYPE"), a.getenv("LANG"))
	glyphs := d.Glyphs.Name
	switch {
	case d.Term.ASCII:
		glyphs += "  " + p.faint("forced by --ascii or config")
	case locale != "":
		glyphs += "  " + p.faint("locale "+locale)
	default:
		glyphs += "  " + p.faint("no locale set")
	}
	row("glyphs", glyphs)
	if !c.UTF8 && !d.Term.ASCII {
		warnings = append(warnings, "the locale is not UTF-8, so astrolabe uses ASCII glyphs (set LANG=en_US.UTF-8 or similar for the Unicode set)")
	}
	var bidi string
	switch c.BidiMode {
	case term.BidiOff:
		bidi = "off  " + p.faint("the terminal lays out right-to-left text itself")
	case term.BidiRuns:
		bidi = "runs  " + p.faint("astrolabe reorders and shapes; the terminal reverses each right-to-left run back")
	default:
		bidi = "on  " + p.faint("astrolabe reorders and shapes right-to-left text")
	}
	row("bidi", bidi)
	row("", p.faint(c.BidiWhy))
	row("hyperlinks", yes(c.Hyperlinks)+p.faint("  OSC 8"))
	row("clipboard", yes(c.Clipboard)+p.faint("  OSC 52"))
	if c.Tmux {
		// tmux drops OSC 52 unless told to pass it on (or set it itself).
		row("", p.faint("inside tmux, copying needs `set -g set-clipboard on` in tmux.conf"))
	}
	row("curly underline", yes(c.StyledUnderline))
	row("size", fmt.Sprintf("%d×%d", c.Width, c.Height))

	// Vault.
	section("Vault")
	r, err := a.resolveRoot()
	if err != nil {
		row("root", p.danger(err.Error()))
		warnings = append(warnings, err.Error())
	} else {
		row("root", r.Dir+"  "+p.faint(rootWhy(r.Source)))
		if !r.Exists {
			row("", p.faint("does not exist yet; created on the first write"))
		} else if vv, err := a.scannedVault(); err != nil {
			row("scan", p.danger(err.Error()))
			warnings = append(warnings, err.Error())
		} else {
			broken := len(vv.Unresolved())
			open := len(vv.Tasks(false))
			parts := []string{plural(vv.Len(), "note", "notes"), plural(len(vv.Files()), "attachment", "attachments"),
				plural(len(vv.Tags()), "tag", "tags"), plural(open, "open task", "open tasks")}
			row("contents", strings.Join(parts, p.faint(" "+p.g.Dot+" ")))
			bl := plural(broken, "broken link", "broken links")
			if broken > 0 {
				bl = p.danger(bl)
				if ts := vv.UnresolvedTargets(); len(ts) > 0 {
					names := []string{}
					for i, t := range ts {
						if i == 3 {
							names = append(names, p.g.Ellipsis)
							break
						}
						names = append(names, t.Target)
					}
					bl += "  " + p.faint(strings.Join(names, ", "))
				}
			}
			row("links", bl)
			dc := vv.DailyConfig()
			daily := vv.DailyPath(a.now())
			state := "not written yet"
			if fileExists(vv.Abs(daily)) {
				state = "exists"
			}
			folder := dc.Folder
			if folder == "" {
				folder = "."
			}
			row("daily notes", fmt.Sprintf("%s/%s.md  %s", folder, dc.Format, p.faint("today: "+daily+", "+state)))
			if fileExists(vv.Abs(".astrolabeignore")) {
				row(".astrolabeignore", "present")
			}
		}
	}

	// Config.
	section("Config")
	cfgFile := vault.ConfigFile(a.getenv)
	cfg, cfgErr := vault.LoadConfig(cfgFile, a.getenv)
	switch {
	case cfgErr != nil:
		row("file", cfgFile+"  "+p.danger(cfgErr.Error()))
		warnings = append(warnings, "config: "+cfgErr.Error())
	case fileExists(cfgFile):
		row("file", cfgFile)
	default:
		row("file", cfgFile+"  "+p.faint("not present (nothing needs configuring)"))
	}
	if cfg != nil {
		for _, k := range vault.ConfigKeys {
			if val, ok := cfg.Get(k); ok {
				src := ""
				if a.getenv("ASTROLABE_"+strings.ToUpper(k)) != "" {
					src = "  " + p.faint("from ASTROLABE_"+strings.ToUpper(k))
				}
				row(k, val+src)
			}
		}
		for _, k := range cfg.Unknown {
			warnings = append(warnings, fmt.Sprintf("config: unknown key %q is ignored (known: %s)", k, strings.Join(vault.ConfigKeys, ", ")))
		}
		for _, b := range cfg.Bad {
			warnings = append(warnings, "config: cannot read "+b)
		}
		if name, ok := cfg.Get("theme"); ok {
			if _, ok := theme.Lookup(name); !ok {
				warnings = append(warnings, fmt.Sprintf("config: unknown theme %q, using %s", name, theme.DefaultName))
			}
		}
	}
	themes := []string{}
	for _, n := range theme.Names() {
		if n == d.ThemeName {
			n = p.accent(n)
		} else {
			n = p.faint(n)
		}
		themes = append(themes, n)
	}
	row("theme", strings.Join(themes, " "))
	rf := vault.RecentFile(a.getenv)
	recent := len(vault.LoadRecent(rf).Entries())
	row("recent notes", rf+"  "+p.faint(plural(recent, "entry", "entries")))
	if rec := vault.RecoveredFiles(vault.RecoveredDir(a.getenv)); len(rec) > 0 {
		row("recovered", vault.RecoveredDir(a.getenv)+"  "+p.faint(plural(len(rec), "unsaved buffer", "unsaved buffers")+" kept when astrolabe was terminated mid-edit"))
	}

	// Test card.
	section("Test card")
	a.testCard(p, d, row)

	if len(warnings) > 0 {
		section("Warnings")
		for _, w := range warnings {
			a.out("  %s %s\n", p.danger("!"), w)
		}
	}
	return nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// rootWhy explains a root source.
func rootWhy(s vault.RootSource) string {
	switch s {
	case vault.RootFromFlag:
		return "from -C"
	case vault.RootFromEnv:
		return "from $ASTROLABE_DIR"
	case vault.RootFromConfig:
		return "from config dir"
	case vault.RootFromMarker:
		return "folder with .obsidian/ or .astrolabe/"
	case vault.RootFromHome:
		return "~/notes"
	case vault.RootFromCwd:
		return "the current folder holds notes"
	case vault.RootFromDefault:
		return "default ~/notes"
	}
	return s.String()
}

// testCard prints colour swatches, attributes, glyphs and scripts so the
// user can see at a glance what the terminal renders.
func (a *app) testCard(p *painter, d *Display, row func(k, v string)) {
	th := p.th
	g := d.Glyphs
	sw := func(name string, c theme.Color) string {
		block := "██"
		if !d.Caps.UTF8 {
			block = "##"
		}
		return p.style(block, theme.Style{FG: c}) + " " + p.style(name, theme.Style{FG: c})
	}
	row("ink", strings.Join([]string{sw("text", th.Text), sw("muted", th.Muted), sw("faint", th.Faint),
		sw("heading", th.Heading)}, "  "))
	row("", strings.Join([]string{sw("accent", th.Accent), sw("link", th.Link), sw("danger", th.Danger),
		sw("ok", th.Ok), sw("math", th.Math)}, "  "))
	row("code", strings.Join([]string{sw("comment", th.CodeComment), sw("string", th.CodeString),
		sw("number", th.CodeNumber), sw("keyword", th.CodeKeyword)}, "  "))
	if p.on && d.Caps.Profile >= term.Profile256 {
		// Grounds: the page room the reader paints (CLI output never does).
		ground := func(name string, c theme.Color) string {
			return p.enc.Styled(" "+name+" ", theme.Style{FG: th.Text, BG: c})
		}
		row("grounds", ground("ground", th.Ground)+ground("raised", th.Raised)+ground("hover", th.Hover)+
			ground("highlight", th.AccentSoft)+" "+p.enc.Styled(" READ ", th.PillRead)+" "+p.enc.Styled(" INSERT ", th.PillInsert))
	}
	attrs := []struct {
		n string
		a theme.Attr
	}{{"bold", theme.Bold}, {"italic", theme.Italic}, {"underline", theme.Underline},
		{"curly", theme.CurlyUnderline}, {"strike", theme.Strike}, {"faint", theme.Faint}, {"reverse", theme.Reverse}}
	var as []string
	for _, x := range attrs {
		as = append(as, p.style(x.n, theme.Style{Attrs: x.a}))
	}
	row("attributes", strings.Join(as, " "))
	glyphs := []string{g.Brand, g.Bullets[0], g.Bullets[1], g.Bullets[2], g.TaskOpen, g.TaskDone, g.TaskDoing,
		g.TaskCancelled, g.Bar, g.Rule, g.VLine, g.Cross, g.FoldOpen, g.FoldClosed, g.External, g.Ellipsis,
		g.Dot, g.Dirty, g.Crumb, g.Image}
	row("glyphs", p.accent(strings.Join(glyphs, " ")))
	var callouts []string
	for _, k := range theme.CalloutKinds() {
		if k == "info" || k == "todo" || k == "danger" || k == "abstract" {
			continue
		}
		callouts = append(callouts, p.style(g.Callout(k)+" "+k, theme.Style{FG: th.CalloutColor(k)}))
	}
	row("callouts", strings.Join(callouts, "  "))
	row("rule", p.faint(g.Ornament)+"   "+p.rule(24))
	if d.Caps.UTF8 {
		row("scripts", "Café · naïve · 日本語 · é · "+p.visual("الأسطرلاب")+"  "+p.faint("(the last should read joined, right to left)"))
	}
	if _, err := os.Stat("/dev/tty"); err != nil {
		row("tty", p.danger("no /dev/tty: the interactive reader needs a terminal"))
	}
}
