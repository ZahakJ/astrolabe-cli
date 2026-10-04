package cli

import (
	"strconv"
	"strings"

	"github.com/ZahakJ/folio/internal/term"
	"github.com/ZahakJ/folio/internal/theme"
)

// globals are the flags accepted before or after any verb (DESIGN.md §4.1).
type globals struct {
	dir     string // -C DIR / --dir DIR
	theme   string // --theme NAME
	color   string // --color MODE
	ascii   bool   // --ascii
	bidi    string // --bidi MODE
	help    bool   // -h / --help
	version bool   // --version
	// rest is the argument list with the global flags removed. A "--"
	// terminator is kept so that verb parsing also stops there.
	rest []string
}

// globalValueFlags maps the spellings of value-taking global flags to a
// setter.
func (g *globals) valueFlag(name string) (func(string), bool) {
	switch name {
	case "-C", "--dir":
		return func(v string) { g.dir = v }, true
	case "--theme":
		return func(v string) { g.theme = v }, true
	case "--color", "--colour":
		return func(v string) { g.color = v }, true
	case "--bidi":
		return func(v string) { g.bidi = v }, true
	}
	return nil, false
}

// parseGlobals extracts the global flags from anywhere before a "--".
func parseGlobals(args []string) (globals, error) {
	var g globals
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			g.rest = append(g.rest, args[i:]...)
			break
		}
		name, val, hasVal := arg, "", false
		if strings.HasPrefix(arg, "--") {
			if k, v, ok := strings.Cut(arg, "="); ok {
				name, val, hasVal = k, v, true
			}
		} else if strings.HasPrefix(arg, "-C") && len(arg) > 2 {
			// -CDIR and -C=DIR
			name, val, hasVal = "-C", strings.TrimPrefix(arg[2:], "="), true
		}
		if set, ok := g.valueFlag(name); ok {
			if !hasVal {
				if i+1 >= len(args) {
					return g, usagef("", "flag %s needs a value", name)
				}
				i++
				val = args[i]
			}
			set(val)
			continue
		}
		switch arg {
		case "--ascii":
			g.ascii = true
			continue
		case "-h", "--help":
			g.help = true
			continue
		case "--version":
			g.version = true
			continue
		}
		g.rest = append(g.rest, arg)
	}
	if g.color != "" {
		if _, _, err := term.ParseProfile(g.color); err != nil {
			return g, usagef("", "--color: %v", err)
		}
	}
	if g.theme != "" {
		if _, ok := theme.Lookup(g.theme); !ok {
			return g, usagef("", "unknown theme %q (themes: %s)", g.theme, strings.Join(theme.Names(), ", "))
		}
	}
	switch strings.ToLower(g.bidi) {
	case "", "auto", "on", "off":
	default:
		return g, usagef("", "--bidi: invalid mode %q (want auto, on or off)", g.bidi)
	}
	return g, nil
}

// flagSet is a small GNU-style option parser for one verb: short (-t) and
// long (--task) spellings, values as "-n X", "-nX", "--at X" or "--at=X",
// options mixed freely with positional arguments, and "--" to end options.
// Combined short booleans ("-lt") are accepted.
type flagSet struct {
	verb  string
	flags []*flagDef
}

type flagDef struct {
	short, long string
	value       bool
	set         func(string) error
}

func newFlagSet(verb string) *flagSet { return &flagSet{verb: verb} }

// Bool defines a boolean option.
func (fs *flagSet) Bool(short, long string) *bool {
	p := new(bool)
	fs.flags = append(fs.flags, &flagDef{short: short, long: long, set: func(string) error { *p = true; return nil }})
	return p
}

// String defines an option with a string value.
func (fs *flagSet) String(short, long string) *string {
	p := new(string)
	fs.flags = append(fs.flags, &flagDef{short: short, long: long, value: true, set: func(v string) error { *p = v; return nil }})
	return p
}

// Int defines an option with a non-negative integer value.
func (fs *flagSet) Int(short, long string, def int) *int {
	p := new(int)
	*p = def
	name := long
	if name == "" {
		name = short
	}
	fs.flags = append(fs.flags, &flagDef{short: short, long: long, value: true, set: func(v string) error {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return usagef(fs.verb, "%s wants a number, not %q", name, v)
		}
		*p = n
		return nil
	}})
	return p
}

func (fs *flagSet) lookup(short byte, long string) *flagDef {
	for _, f := range fs.flags {
		if long != "" && f.long == "--"+long {
			return f
		}
		if long == "" && f.short == "-"+string(short) {
			return f
		}
	}
	return nil
}

// Parse returns the positional arguments.
func (fs *flagSet) Parse(args []string) ([]string, error) {
	var pos []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			return append(pos, args[i+1:]...), nil
		case arg == "-" || !strings.HasPrefix(arg, "-"):
			pos = append(pos, arg)
		case strings.HasPrefix(arg, "--"):
			name, val, hasVal := strings.Cut(arg[2:], "=")
			f := fs.lookup(0, name)
			if f == nil {
				return nil, usagef(fs.verb, "unknown option --%s", name)
			}
			if !f.value {
				if hasVal {
					return nil, usagef(fs.verb, "option --%s takes no value", name)
				}
				_ = f.set("")
				continue
			}
			if !hasVal {
				if i+1 >= len(args) {
					return nil, usagef(fs.verb, "option --%s needs a value", name)
				}
				i++
				val = args[i]
			}
			if err := f.set(val); err != nil {
				return nil, err
			}
		default:
			// One or more short options: -t, -lt, -nNOTE, -n NOTE.
			for j := 1; j < len(arg); j++ {
				f := fs.lookup(arg[j], "")
				if f == nil {
					return nil, usagef(fs.verb, "unknown option -%c", arg[j])
				}
				if !f.value {
					_ = f.set("")
					continue
				}
				val := strings.TrimPrefix(arg[j+1:], "=")
				if j+1 >= len(arg) {
					if i+1 >= len(args) {
						return nil, usagef(fs.verb, "option -%c needs a value", arg[j])
					}
					i++
					val = args[i]
				}
				if err := f.set(val); err != nil {
					return nil, err
				}
				break
			}
		}
	}
	return pos, nil
}
