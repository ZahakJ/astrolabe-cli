package cli

import (
	"strings"
)

// verbDoc is the manual of one verb. On a locked-down machine the help is
// the manual, so each entry is short, complete and led by examples.
type verbDoc struct {
	usage   string // one line, without "folio "
	summary string // one line
	body    string // options, examples, output; may be empty
}

var docs = map[string]verbDoc{
	"add": {
		usage:   "add [-t] [-n NOTE] [--at HEADING] TEXT…",
		summary: "Capture a line to today's daily note.",
		body: `Appends one list item, "- HH:MM text", to today's daily note (created if
missing). With no TEXT the text is read from stdin; several lines become one
item. Nothing else in the file changes.

Options
  -t, --task          capture a task, "- [ ] text"
  -n, --note NOTE     append to NOTE instead (created if it does not exist)
  --at HEADING        insert at the end of that section (added if missing)

Examples
  folio add "call the plumber about the boiler"
  folio add -t "renew passport due:2026-11-02 !!"
  folio add -n Inbox --at Reading "The Quiet Machine, part II"
  pbpaste | folio add                # capture the clipboard

Output
  The note and line written: "Added to daily/2026-10-04.md:12" in a
  terminal, "daily/2026-10-04.md:12" in a pipe.`,
	},
	"new": {
		usage:   "new [-e | -o] [-d DIR] TITLE…",
		summary: "Create a note and print its path.",
		body: `The file is named after the title (characters that break links or file
systems are dropped) and starts with "# TITLE". An existing note is never
overwritten: "Title 2.md" is created instead. Piped stdin becomes the body.

Options
  -e, --edit          open the new note in $VISUAL / $EDITOR (default vi)
  -o, --open          open it in folio's built-in editor
  -d, --dir DIR       create it in DIR, a folder of the vault

Examples
  folio new "Lantern retro"
  folio new -d meetings -e "Platform sync 2026-10-05"
  git log --oneline -20 | folio new "Release notes draft"
  nvim "$(folio new 'Scratch')"

Output
  The note's absolute path.`,
	},
	"today": {
		usage:   "today [-p]",
		summary: "Open today's daily note (created if missing).",
		body: `Daily notes live in daily/YYYY-MM-DD.md, or where .obsidian/daily-notes.json
or the daily_dir / daily_format settings say. A new one starts with a
heading such as "# Sunday, 4 October 2026".

Options
  -p, --print         print its absolute path instead of opening it

Examples
  folio today
  vim "$(folio today -p)"`,
	},
	"find": {
		usage:   "find [-l] [--json] [-n N] QUERY…",
		summary: "Search the text of every note.",
		body: `Terms are ANDed and matched as substrings, ignoring case unless a term has
an upper-case letter. Titles rank first, then headings, then body lines.

Query syntax
  word "two words"    substrings (quote a phrase)
  re:PATTERN          a regular expression (Go syntax)
  tag:x  path:x       only notes with tag x / whose path contains x
  title:x             only notes whose title or an alias contains x

Options
  -l, --files         print each matching note's path once
  -n, --limit N       at most N results
  --json              an array of {path, abs, title, line, text, matches, kind}

Examples
  folio find cutover
  folio find 'tag:runbook "connection pool"'
  folio find 're:TODO|FIXME' path:projects
  vim -q <(folio find deploy)           # load the hits into the quickfix list

Output
  Grouped and highlighted in a terminal; "path:line:text" in a pipe.
  Exit status 1 when nothing matches.`,
	},
	"ls": {
		usage:   "ls [--tag T] [-l] [--json] [-n N] [QUERY]",
		summary: "List notes, most recently modified first.",
		body: `QUERY keeps the notes whose path, title or an alias matches it fuzzily.

Options
  --tag T             only notes tagged T (nested tags count: --tag work
                      includes #work/lantern)
  -l, --files         paths only
  -n, --limit N       at most N notes
  --json              an array of {path, abs, title, aliases, tags, modified,
                      words, size}

Examples
  folio ls
  folio ls -n 5
  folio ls --tag meeting
  folio ls -l lantern | tr '\n' '\0' | xargs -0 grep -l rollback

Output
  Aligned columns in a terminal; "path<TAB>YYYY-MM-DD HH:MM<TAB>title" in a
  pipe. Exit status 1 when no note matches.`,
	},
	"pick": {
		usage:   "pick [QUERY]",
		summary: "Choose a note with the fuzzy picker and print its path.",
		body: `Draws on the terminal even inside $(…), so it composes with other tools.
Esc cancels with exit status 130 and prints nothing.

Examples
  nvim "$(folio pick)"
  wc -w "$(folio pick meet)"`,
	},
	"tasks": {
		usage:   "tasks [--all] [--due] [--json]",
		summary: "Open tasks across the vault, grouped by due date.",
		body: `Groups: Overdue, Today, Upcoming, Undated; each sorted by due date, then
priority. A task is "- [ ] text"; "[/]" is in progress, "[x]" done, "[-]"
cancelled. Due dates are written "📅 2026-10-05", "due:2026-10-05" or
"@due(2026-10-05)"; priority "!", "!!", "!!!" (or ⏫ 🔼 🔽).

Options
  -a, --all           include done and cancelled tasks
  --due               only tasks with a due date
  --json              an array of {path, abs, title, line, state, status,
                      text, raw, due, priority, group}

Examples
  folio tasks
  folio tasks --due --json | jq -r '.[] | select(.group=="overdue") | .text'

Output
  Grouped with due chips in a terminal; "path:line:[ ] text due:DATE" in a
  pipe. Exit status 1 when there are none.`,
	},
	"tags": {
		usage:   "tags [-c] [--json]",
		summary: "List tags with the number of notes carrying each.",
		body: `Tags come from #tags in the text and the frontmatter "tags" list. A note
tagged #work/lantern also counts towards #work.

Options
  -c, --count         most used first
  --json              an array of {tag, count}

Examples
  folio tags
  folio ls --tag "$(folio tags -c | head -1 | cut -f1)"

Output
  An indented tree in a terminal; "tag<TAB>count" in a pipe.`,
	},
	"links": {
		usage:   "links [-l] [--json] NOTE",
		summary: "Outgoing links of a note, resolved.",
		body: `Wikilinks, Markdown links, embeds and URLs, in document order, each with
the note or file it resolves to. Broken links are marked.

Options
  -l, --files         the linked notes' paths, once each
  --json              an array of {line, col, kind, target, heading, block,
                      label, embed, broken, path, abs, to_line}

Examples
  folio links "Lantern migration"
  folio links -l Inbox | while IFS= read -r f; do folio render "$f"; done

Output
  "line<TAB>kind<TAB>destination" in a pipe; kind is wikilink, markdown,
  embed, url or broken.`,
	},
	"backlinks": {
		usage:   "backlinks [-l] [--json] NOTE",
		summary: "Notes linking to NOTE, with the line that links.",
		body: `Options
  -l, --files         the linking notes' paths, once each
  --json              an array of {path, abs, title, line, col, kind, context}

Examples
  folio backlinks "Lantern migration"
  folio backlinks -l Inbox

Output
  Grouped by note in a terminal; "path:line:context" in a pipe.`,
	},
	"render": {
		usage:   "render [-w COLS] [FILE|NOTE|-]",
		summary: "Typeset Markdown to stdout.",
		body: `Styled for the terminal (with the theme, --color and --ascii) when stdout is
a terminal, plain text in a pipe. Reads stdin with "-" or no argument.

Options
  -w, --width COLS    wrap to COLS (default: the terminal width, else 80)

Examples
  folio render README.md
  folio render "Lantern migration" | less -R
  curl -s https://example.org/notes.md | folio render --color=256`,
	},
	"export": {
		usage:   "export [-o FILE] NOTE",
		summary: "Write a note as one standalone HTML file.",
		body: `Inline CSS, no scripts, no external requests; light and dark styles;
wikilinks become relative .html links.

Options
  -o, --output FILE   write FILE instead of stdout

Examples
  folio export "Lantern migration" -o lantern.html`,
	},
	"path": {
		usage:   "path [NOTE]",
		summary: "Print the vault root, or a note's absolute path.",
		body: `Examples
  cd "$(folio path)"
  folio path Inbox
  folio -C ~/work-notes path`,
	},
	"doctor": {
		usage:   "doctor",
		summary: "Show terminal capabilities, vault, config and a test card.",
		body: `Use it when something looks wrong: it shows what folio detected (colour
depth, glyphs, bidi, hyperlinks, clipboard), which vault it chose and why,
the config file and any unknown keys, and a card of colours and glyphs.

Examples
  folio doctor
  folio doctor --color 16 --ascii`,
	},
	"help": {
		usage:   "help [VERB]",
		summary: "Show help for folio or one verb.",
	},
	"version": {
		usage:   "version",
		summary: "Print the version.",
	},
}

// usageLine returns "folio <usage>" for a verb, "" for none.
func usageLine(verb string) string {
	if d, ok := docs[verb]; ok {
		return "folio " + d.usage
	}
	return ""
}

const overview = `folio — Markdown notes in the terminal: a reader, an editor and shell verbs
over a folder of plain .md files.

Open
  folio                 today's daily note, else the last note, else home
  folio NOTE            a note by path, title, alias or unique fuzzy match
  folio -               page Markdown from stdin

Capture
  add TEXT…             add a line to today's daily note  (-t task, -n NOTE)
  new TITLE…            create a note, print its path  (-e $EDITOR, -d DIR)
  today                 open today's daily note  (-p print its path)

Find
  find QUERY…           search note text: path:line:text  (-l, --json)
  ls [QUERY]            notes, most recently modified first  (--tag T)
  pick [QUERY]          fuzzy picker that prints the chosen path
  tasks                 open tasks grouped by due date  (--all, --due)
  tags                  tags with counts
  links NOTE            outgoing links
  backlinks NOTE        incoming links with context

Output
  render [FILE|-]       typeset Markdown for the terminal, or plain text
  export NOTE           standalone HTML  (-o FILE)
  path [NOTE]           the vault root, or a note's absolute path

Other
  doctor                terminal, vault and config check with a test card
  help [VERB]           the manual for one verb
  version               print the version

Global flags, before or after the verb
  -C, --dir DIR         the vault folder (default: $FOLIO_DIR, config "dir",
                        the nearest folder with .obsidian/ or .folio/,
                        ~/notes, or the current folder if it holds notes)
  --theme NAME          iron-gall, parchment, graphite or mocha
  --color MODE          auto, truecolor, 256, 16 or none (NO_COLOR is honoured)
  --ascii               ASCII glyphs only
  --bidi MODE           auto, on or off: let folio lay out right-to-left text

Examples
  folio add "call the plumber"          # one line into today's note
  folio add -t "renew passport due:2026-11-02"
  folio find 'tag:work cutover'         # grep-like, across the vault
  nvim "$(folio pick)"                  # choose a note, edit it elsewhere
  folio ./add.md                        # a note whose name is a verb

In a pipe, output is plain and stable (path:line:text, tab-separated
columns, --json); paths are relative to the current folder inside the vault,
absolute outside it. Exit status: 0 ok, 1 nothing found, 2 usage error,
130 cancelled. Config: ~/.config/folio/config (see 'folio doctor').`

// cmdHelp is `folio help [VERB]`.
func (a *app) cmdHelp(args []string) error {
	if len(args) > 1 {
		return usagef("help", "help takes one verb")
	}
	var txt string
	if len(args) == 0 {
		txt = overview
	} else {
		d, ok := docs[args[0]]
		if !ok {
			msg := "no help for " + quote(args[0])
			if v := nearestVerb(args[0]); v != "" {
				msg += "; did you mean " + quote(v) + "?"
			}
			return usagef("help", "%s", msg)
		}
		txt = "folio " + d.usage + "\n\n" + d.summary
		if d.body != "" {
			txt += "\n\n" + d.body
		}
	}
	a.printHelp(txt)
	return nil
}

func quote(s string) string { return `"` + s + `"` }

// printHelp prints help text, lightly styled in a terminal: section
// headings in heading ink, shell comments faint, the first line bold.
func (a *app) printHelp(txt string) {
	p := a.painter()
	lines := strings.Split(txt, "\n")
	for i, l := range lines {
		switch {
		case !p.on:
		case i == 0:
			l = p.heading(l)
		case l != "" && l[0] != ' ' && len(l) < 40 && !strings.HasSuffix(l, ".") && i > 0 && lines[i-1] == "":
			l = p.heading(l)
		case strings.HasPrefix(l, "  "):
			if j := strings.Index(l, "  # "); j > 0 {
				l = l[:j] + p.faint(l[j:])
			}
		}
		a.out("%s\n", l)
	}
}
