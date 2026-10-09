# Astrolabe CLI — design contract

`astrolabe` is a terminal Markdown notes tool: a beautiful reader, a small modal
editor, and a set of shell verbs for capture and search, in one static binary
over a folder of plain `.md` files. This document is normative. Implementers
build against it; where it is silent, choose what a careful person would and
note the choice in a code comment.

## 1. Principles (in priority order when they conflict)

1. **The files are the product's only state.** No database, no index on disk,
   no sidecar files in the vault. Astrolabe CLI never rewrites a file it was not asked
   to edit, and an edit touches only the bytes that changed (line endings,
   trailing newline, frontmatter, BOM and unknown syntax are preserved). All
   writes are atomic (temp file in the same directory + rename), keep the file
   mode, and refuse to clobber a file that changed on disk since it was read
   (offer reload / overwrite).
2. **Useful in the first minute, nothing to configure.** Every default must be
   right. Configuration exists (section 9) but nothing requires it.
3. **A couple of seconds, a handful of keys.** Capture, find and return to work.
   Cold start to first paint under 50 ms on a 1,500-note vault: paint the
   requested note first, scan the vault concurrently afterwards.
4. **The page is a manuscript.** Typographic care in the reader is what the
   owner judges first (section 5).
5. **Degrade, never break.** No truecolour, no Unicode, no Nerd Font, 60×16
   window, SSH, tmux, a pipe: all are first-class (section 6).

## 2. Implementation constraints

- Go (module `github.com/ZahakJ/astrolabe-cli`, `go 1.22` language level), built with
  `CGO_ENABLED=0 -trimpath -ldflags "-s -w -X main.version=…"`: a static binary
  with no libc dependency, so it runs on CentOS 7 era kernels and on macOS.
  Targets: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64.
- Dependencies allowed: the standard library, `golang.org/x/sys`,
  `golang.org/x/term`, `golang.org/x/text` (for `unicode/bidi`, `width`,
  `unicode/norm` only), `github.com/rivo/uniseg` (grapheme clusters and cell
  width). **Nothing else**: no TUI framework, no Markdown library, no syntax
  highlighting library. Target stripped binary size ≤ 6 MB.
- No Nerd Font glyphs anywhere. No network access at runtime, ever.
- Runtime files: optional `$XDG_CONFIG_HOME/astrolabe-cli/config` (read; written only
  when the user changes theme from inside the TUI) and
  `$XDG_STATE_HOME/astrolabe-cli/recent` (recent notes + last position; losing it loses
  nothing), plus `$XDG_STATE_HOME/astrolabe-cli/recovered/` for an unsaved buffer that
  could not be written to its note when the process was signalled. Neither lives in the vault.
- Tests: `go test ./...` must pass and be fast (< 20 s). Each package has unit
  tests; `internal/render` has golden-file tests.
- This repository is public. No real notes, hostnames, usernames or home paths
  in code, tests, fixtures, comments or docs. Sample content is written for the
  purpose (`examples/vault`).

### Package layout and ownership

```
cmd/astrolabe/main.go        dispatch only
internal/term            terminal: capabilities, raw mode, input decoding, cell screen, SGR
internal/text            width, graphemes, wrapping, bidi reordering, Arabic shaping, fuzzy match
internal/theme           palettes and semantic style tokens, glyph sets
internal/md              Markdown (Obsidian flavour) → block/inline AST with source positions
internal/vault           discovery, scan, link graph, tags, tasks, search, daily notes, capture, safe writes
internal/render          AST → styled lines for a given width; ANSI/plain serialisation
internal/export          AST → standalone HTML
internal/editor          modal editor component
internal/cli             non-interactive verbs
internal/tui             the interactive application
examples/vault           sample notes (also the test/demo fixture)
scripts/                 build, capture (tmux → text/SVG/PNG) helpers
install.sh               one-line installer
```

## 3. Vault model

- A vault is a directory tree of `.md` files. Resolution order for the root:
  `-C DIR`/`--dir`, `$ASTROLABE_DIR`, the nearest ancestor of the working
  directory containing `.obsidian/` or `.astrolabe/` (the vault you stand in),
  the **remembered vault** (config `dir`; skipped with a one-line warning when
  it no longer exists), `~/notes` if it exists, the working directory if it
  directly contains any `.md`, else `~/notes` (created on first write).
  `astrolabe doctor` and `astrolabe vault` say which rule chose, and name the
  remembered vault when the one you stand in won over it.
- **Remembering.** A tool for notes must know where the notes are. When a run
  opens its vault from `-C DIR`, and nothing is remembered or a different
  vault is, `dir = <absolute path>` is written to the config (created if
  absent, other lines and comments kept) and stderr says once, on a
  terminal: ``Using ~/my-notes from now on; `astrolabe vault` to change.``
  A vault found by its marker is remembered only while nothing is (walking
  into a project vault must not steal the remembered one). Never
  remembered: `$ASTROLABE_DIR` (a per-environment choice that still wins for
  that run), the new `~/notes` default, a vault that failed to open, the
  vault of a single file opened from outside it, and runs of `doctor` and
  `vault`. A failed write is soft: doctor warns when the config is not
  writable. `astrolabe vault [DIR | --forget]` shows, sets or clears it.
- **First run.** The Welcome note (`Welcome.md`, a one-minute tutorial) is
  written and opened by plain `astrolabe` only when nothing is remembered,
  the vault is the new `~/notes` default, it is empty, and the state file
  `used` (written whenever a vault in `~/notes` is opened) is absent; so it
  opens by itself at most once. `astrolabe learn` opens it on purpose
  (writing it only if no `Welcome.md` exists; `-p` prints it).
- `astrolabe some/file.md` outside any vault: the vault is the nearest ancestor
  with `.obsidian/`/`.astrolabe/`, else the file's own directory.
- Scan skips dot-directories, `node_modules`, and anything matched by a
  top-level `.astrolabeignore` (one glob per line). Symlinks to files are followed,
  directory symlinks are not.
- **Note identity** is the vault-relative path. The title is the frontmatter
  `title`, else the first H1, else the filename without `.md`.
- **Frontmatter**: a leading `---` YAML block. Parse only what is needed
  (`title`, `tags` as list or comma/space string, `aliases`, `date`,
  `created`); keep everything else as raw key/value lines for display. Never
  re-serialise it.
- **Wikilinks**: `[[Note]]`, `[[Note|label]]`, `[[Note#Heading]]`,
  `[[Note#^block]]`, `[[folder/Note]]`, embeds `![[Note]]` / `![[image.png]]`.
  Resolution follows Obsidian: exact relative path first, then shortest
  matching path by basename (case-insensitive, `.md` optional), then aliases.
  Markdown links `[text](relative/path.md)` and `[text](url)` are links too.
  Unresolved links are "broken" and following one offers to create the note.
- **Tags**: `#tag`, `#nested/tag` in body text (not in code, not in URLs, not
  a bare number, not a heading marker) plus frontmatter `tags`.
- **Tasks**: `- [ ]`, `- [x]`, also `[/]` in-progress and `[-]` cancelled,
  with an optional due date written as `📅 2026-10-05`, `due:2026-10-05` or
  `@due(2026-10-05)`, and priority `!`/`!!`/`!!!` or `⏫ 🔼 🔽`.
- **Daily notes**: folder and filename format are read from
  `.obsidian/daily-notes.json` when present (`folder`, `format` in Moment
  tokens `YYYY MM DD`), else `daily/YYYY-MM-DD.md`. A new daily note starts
  with `# <weekday>, <D Month YYYY>`.
- **Capture** appends to today's daily note (creating it), one line per
  capture: `- HH:MM text`, or `- [ ] text` for a task. Appending adds exactly
  one newline-terminated line and changes nothing else.
- **Search** is a concurrent scan of the files on every query: smart-case
  substring by default, `re:` prefix for a regexp, `tag:x`, `path:x`,
  `title:x` filters, all terms ANDed. Must return within ~150 ms on 1,500
  notes / 10 MB. Results are ranked title hits, then heading hits, then body.
- The link graph (links, backlinks with a context line, tags, tasks) is built
  in memory per run by a light line scanner that respects code fences; it must
  not need the full AST. Large files (> 2 MB) are indexed by title only.

## 4. Shape of the tool

Two faces over the same vault: **shell verbs** that do one thing and exit, and
the **TUI**, which opens in the *reader*, with a built-in modal *editor* one
key away and `$EDITOR` one key further. Reader and editor are separate modes:
the rendered page is for reading and navigating, the source is for editing,
and switching keeps your place (the editor opens on the source line of the
block under the reader's cursor; leaving it returns to the same block).

### 4.1 Shell verbs

```
astrolabe                       open the TUI: today's daily note if it exists, else the last note, else the home screen
astrolabe NOTE                  open NOTE (path, or a title/basename/alias resolved like a wikilink; fuzzy-unique match accepted)
astrolabe -                     read Markdown on stdin and page it in the reader
astrolabe add TEXT…             capture a line to today's daily note   (stdin if no TEXT; -t task; -n NOTE target; --at HEADING)
astrolabe new TITLE…            create a note (prints its path); -o open in the TUI editor; -e open in $EDITOR; -d DIR; stdin becomes the body
astrolabe today                 open today's daily note (created if missing); -p print its path instead
astrolabe find QUERY…           full-text search → path:line:text      (exit 1 when nothing matches; -l paths only; --json)
astrolabe ls [QUERY]            list notes, most recently modified first (--tag T, --json, -l paths only)
astrolabe pick [QUERY]          interactive fuzzy picker; prints the chosen path  (exit 130 on cancel) — for `nvim "$(astrolabe pick)"`
astrolabe tasks                 open tasks grouped Overdue / Today / Upcoming / Undated (--all, --due, --json; exit 1 if none)
astrolabe tags                  tags with counts
astrolabe links NOTE            outgoing links;  astrolabe backlinks NOTE   incoming links with context
astrolabe render [FILE|-]       render Markdown to stdout: ANSI on a terminal, plain text in a pipe (-w COLS, --color=…)
astrolabe export NOTE           standalone HTML to stdout (-o FILE)
astrolabe path [NOTE]           print the vault root, or a note's absolute path
astrolabe vault [DIR|--forget]  show the vault a plain run uses, the rule and the remembered vault (path alone in a pipe); DIR remembers it; --forget clears it
astrolabe learn                 open the Welcome note (written if absent); -p print it
astrolabe doctor                show detected terminal capabilities, vault, config, and a glyph/colour test card
astrolabe help [VERB]  ·  astrolabe version
```

Rules: results on stdout, messages on stderr; machine-readable when stdout is
not a terminal (no colour, no decoration, stable `path:line:text` forms);
exit 0 success, 1 "nothing found", 2 usage error, 130 cancelled. Global flags
before or after the verb: `-C DIR` (remembered, §3), `--theme NAME`, `--color
auto|truecolor|256|16|none`, `--ascii`, `--bidi auto|on|off`. `NO_COLOR` is
honoured. Unknown first argument that resolves to no note → a clear error
with the nearest matches, exit 1. Unique verb prefixes are not accepted (a
note may be called `to`); the verbs above are reserved words and `Astrolabe CLI
./add.md` opens a file with a reserved name.

### 4.2 TUI layout

```
┌ tree (opt) ┬──────────── page ────────────┬ context (opt) ┐
│ folders    │   centred measure ≤ 78 cols  │ outline       │
│ notes      │   the rendered note          │ backlinks     │
├────────────┴──────────────────────────────┴───────────────┤
│ ✦ folder › Note title            1,240 words · 5 min  READ │
└────────────────────────────────────────────────────────────┘
```

- The page is always present and gets its full measure first. The context
  panel (outline above backlinks) opens by default only at ≥ 124 columns, the
  tree only on request; either can be toggled at any width, and when toggled
  on in a narrow window it overlays instead of squeezing the page below 50
  columns. No tab bar. Below 60 columns the status bar keeps only the note
  name and the mode pill. Minimum supported size 40×10; smaller shows a
  single "window too small" line rather than garbage.
- Status bar: left `✦`, the vault's short name (home-relative such as
  `~/notes` when that is short, else the folder's own name) and the
  breadcrumb, e.g. `✦ my-notes › runbooks › Lantern cutover` (vault and
  folders muted; folders are the first to ellipsise, then the vault name,
  note name last standing; the home screen shows the vault's home-relative
  path), dirty dot `●` in the accent when modified;
  right: words · reading time (reader) or `line:col` (editor), then a mode pill
  `READ` / `NORMAL` / `INSERT` / `VISUAL`. Transient messages replace the left
  side for 3 s. One line for `:` commands and `/` search replaces the bar while
  typing.
- Overlays (finder, search, palette of leader keys, help, tags, agenda,
  prompts) are centred raised panels with a hairline border, at most 80×24,
  a one-line input at the top, a selected row marked by an accent left bar on the
  soft-accent ground. `Esc` always closes the topmost overlay.
- Home screen (no note open): the Astrolabe mark drawn in braille (ASCII with `--ascii`; a single `✦` when the window is too short), the line "The vault is open.", the
  note count, then recent notes and a two-column key legend.

### 4.3 Keymap (normative)

Reader — a line cursor (an accent `▎` in the left gutter) moves over rendered
lines; actions apply to the block under it.

| Keys | Action |
|---|---|
| `j` `k` / arrows | cursor down / up (counts work: `5j`) |
| `Ctrl-d` `Ctrl-u`, `Ctrl-f` `Ctrl-b`, `PgDn` `PgUp` | half / full page |
| `gg` `G` | top / bottom |
| `]]` `[[`  (also `}` `{`) | next / previous heading |
| `Tab` `Shift-Tab` | next / previous link (highlights it) |
| `Enter`, `gd`, `Ctrl-]` | follow the highlighted link, else the first link on the cursor line; a URL is shown and copied (OSC 52), never opened silently |
| `Backspace`, `Ctrl-o`, `H` | back in history |
| `L` | forward in history |
| `za` `zc` `zo` `zM` `zR` | fold the section under the cursor / all / none |
| `x` | toggle the task under the cursor (rewrites one character in the file) |
| `i` `a` `e` | edit this block's source line in the built-in editor: `i` Insert at its start, `a` Insert at its end, `e` Normal mode |
| `E` | open `$EDITOR` (default `vi`) at that line; reload on return |
| `/` `n` `N` | search inside the note |
| `Ctrl-p`, `Space f` | find note (fuzzy on title, path, alias; empty query = recent) |
| `Space /` (also `Space s`) | search the vault's text, live results with context |
| `Space d` | today's daily note |
| `Space c` | capture: a one-line prompt appended to today's daily note without leaving the page |
| `Space n` | new note (prompt for title; opens in the editor) |
| `Space t` | agenda: open tasks across the vault by due date; `x` toggles, `Enter` jumps |
| `Space #` | tags → notes with that tag |
| `Space b` `Space o` | toggle context panel focus on backlinks / outline (`Enter` jumps) |
| `Space e` | toggle the tree |
| `Space r` | recent notes |
| `Space T` | cycle theme |
| `Space y` | copy this note's wikilink (OSC 52) |
| `Space q`, `q`, `ZZ` | quit (asks if there are unsaved edits) |
| `?` | help: this table, scrollable |
| `:` | command line: `:w :q :wq :x :e NOTE :new TITLE :today :theme NAME :export :set wrap=N :help` |

Pressing `Space` and pausing 300 ms shows the leader palette: every `Space`
binding with a one-word description (this is how the keymap is learned).

Editor — a deliberate Vim subset; everything here behaves as it does in Vim so
the skill transfers:

- Modes: Normal, Insert, Visual (`v`), Visual-line (`V`), command line (`:`).
- Motions: `h j k l`, `w b e W B E`, `0 ^ $`, `gg G` `{count}G`, `{ }`,
  `f F t T ; ,`, `%`, `H M L`, `Ctrl-d Ctrl-u Ctrl-f Ctrl-b`, counts. `j`/`k`
  move by display line when a line is soft-wrapped (as `gj`/`gk`), since prose
  lines are long.
- Operators `d c y > <` with motions, counts and text objects `iw aw ip ap
  i" a" i( a( i[ a[ i` a``; `dd cc yy D C x X s S r J ~ p P o O A I a i`;
  `u` / `Ctrl-r` undo tree as a linear stack with insert-session grouping; `.`
  repeat; `/ ? n N *`; `:s/a/b/[g]` and `:%s/a/b/[g]`; registers: unnamed and
  `"+` (OSC 52 copy; paste arrives as bracketed paste).
- Leaving: `:w`, `:q`, `:q!`, `:wq`, `:x`, `ZZ`, `Ctrl-s` saves. `Esc` in
  Normal mode with no pending keys returns to the reader **if the buffer is
  clean**; if dirty it saves first (autosave on leave is the default; `:q!`
  discards). So the common loop is `i` … type … `Esc Esc`.
- Markdown assistance in Insert mode: `Enter` continues a list item, numbered
  item, task or quote (and ends it when the item is empty); `Tab`/`Shift-Tab`
  on a list item indents/outdents it, inside a table moves between cells and
  re-aligns the table; typing `[[` opens a note completer (fuzzy, `Enter`
  inserts, `Esc` leaves the brackets); `Ctrl-t` inserts a timestamp.
- The source is shown styled, not rendered: heading lines in heading ink and
  bold, markers (`#`, `*`, `-`, `>`, backticks, brackets) faint, emphasis
  styled in place, links and tags in accent, code ground raised, frontmatter
  muted. Soft wrap to the page measure with a hanging indent that follows list
  markers. The cursor line has a soft ground. Relative line numbers are off;
  `:set nu` turns absolute numbers on.
- RTL lines are displayed in visual order with the cursor mapped through the
  reordering; editing is always in logical order.

## 5. The page: how a note is rendered

Terminals have one font size, so hierarchy is built from weight, ink, space,
rules and ornament. The measure is `min(78, width − margins)` columns, centred
when the pane is wider. Body text is wrapped at word boundaries, never
justified, with one blank line between blocks. All widths are in terminal
cells measured per grapheme cluster.

- **Title block.** If the note has frontmatter or a title: the title in
  heading ink, bold, followed by a muted line of properties as chips —
  `date · #tag #tag · N backlinks` — then a full-measure hairline. Remaining
  frontmatter keys are folded into a single `▸ properties · N` line that `za`
  opens. A leading H1 equal to the title is not printed twice.
- **Headings.** H1: blank line, text bold in heading ink, then a hairline `─`
  the width of the measure (the rule is reserved for H1). H2: two blank lines
  above, bold heading ink. H3: bold body ink. H4–H6: italic muted. Every
  heading carries a
  fold chevron in the gutter, visible at rest: `▾` open, `▸` folded (faint;
  folded sections show `… N lines` muted).
- **Emphasis.** Bold, italic, bold-italic, strikethrough (faint +
  strike), `==highlight==` (accent-soft ground), inline code (raised ground,
  one cell of padding when the ground is painted, otherwise accent-coloured
  backticks kept).
- **Links.** Wikilinks and Markdown links in accent, no brackets shown;
  external URLs get a faint `↗` after the label; broken wikilinks in danger
  ink with a dotted/curly underline where supported, plain underline
  otherwise. The highlighted link (Tab) is reverse-video accent. Bare URLs are
  shortened to the host + `…` when longer than half the measure. Where the
  terminal supports OSC 8, URLs are emitted as hyperlinks.
- **Lists.** Bullets by depth `•`, `◦`, `▪` in accent; ordered numbers in
  accent, right-aligned to the widest number; hanging indent; tight lists stay
  tight. Tasks: `☐` open, `☑` done (text faint + strike), `◐` in progress,
  `☒` cancelled; due dates as a chip, danger ink when overdue.
- **Block quotes.** A `▎` bar in accent at the left, text italic muted,
  nesting adds bars.
- **Callouts** (`> [!note] Title`, with `+`/`-` fold markers): a bar and a
  bold label line in the callout's hue with a glyph (`ℹ note`, `✎ abstract`,
  `✓ success`, `? question`, `! warning`, `✗ failure/danger`, `☞ tip`,
  `❝ quote`, `≡ example`, `※ bug`), body in body ink on the
  same bar. Unknown types use the note hue.
- **Code blocks.** A raised-ground block the width of the measure with one
  cell of inner padding, the language as a faint label on the top-right, a
  small built-in highlighter (comments, strings, numbers, keywords) for sh,
  python, go, js/ts, json, yaml, c/c++, rust, lua, sql, diff (added/removed
  lines) — unknown languages are unhighlighted. Lines longer than the block
  are not wrapped: they are clipped with a faint `…` and the block scrolls
  horizontally with `h`/`l` when the cursor is in it. With no painted ground
  the block is framed by a faint `│` left bar instead.
- **Tables.** Box-drawing hairlines (`─ │ ┼`), header bold with a rule under
  it, no outer vertical borders, alignment from the delimiter row, inline
  markup inside cells. Column widths: natural if it fits; otherwise shrink the
  widest columns and wrap cell text; if a column would fall under 6 cells the
  table switches to a stacked "record" layout (one `header: value` group per
  row). A table may exceed the measure up to the pane width before shrinking.
- **Rules.** `---` renders as a centred ornament `·  ✦  ·` in faint/accent.
- **Footnotes** as superscript-style `[1]` markers in accent, definitions
  gathered at the end under a short hairline. **Images/embeds**: `▣ alt text`
  with the file name muted (no image protocols in v1); note embeds
  `![[Note]]` render the first 12 lines of the target inside a quote bar with
  its title. **Math** (`$…$`, `$$…$$`) is shown verbatim in a distinct muted
  ink. **HTML** blocks are shown verbatim, faint; `<br>` breaks lines;
  comments `%% … %%` and `<!-- -->` are hidden. **Tags** render as chips:
  accent `#`, muted text.
- **Right-to-left.** A paragraph's base direction is that of its first strong
  character. RTL blocks are right-aligned within the measure, with bullets,
  bars and markers mirrored to the right edge. Reordering and Arabic shaping
  are handled per section 6.

Every rendered line records the source line(s) it came from and the hit
regions on it (links, task box, fold chevron), so the reader's cursor,
editing, task toggling and following links all map back to the file.

## 6. Terminals

- **Colour**: detect truecolour (`COLORTERM`), 256 (`TERM` contains
  `256color`), 16, or none (`NO_COLOR`, `TERM=dumb`, not a tty). Themes are
  authored in 24-bit; 256 is nearest-colour from the cube and grey ramp; 16
  uses default foreground/background with ANSI yellow as accent, bright black
  as faint, and attributes (bold, italic, underline, reverse) doing the work —
  no painted grounds, so it is correct on dark and light terminals alike.
- **Ground**: in truecolour/256 the theme paints its own ground (the page is
  a "room", as in Astrolabe) — `ground=off` leaves the terminal's own.
- **Glyphs**: a Unicode set and an ASCII set chosen by locale (`LC_ALL`,
  `LC_CTYPE`, `LANG` containing UTF-8) or `--ascii`: `✦`→`*`, `•◦▪`→`* - +`,
  `☐☑`→`[ ] [x]`, `▎`→`|`, `─`→`-`, `▾▸`→`v >`, `↗`→`^`, `…`→`...`. Only
  glyphs present in common monospace fonts (DejaVu Sans Mono, Menlo, Consolas
  as the floor) may be used in the Unicode set; nothing from Nerd Fonts, no
  emoji.
- **Bidirectional text**: three behaviours, one per kind of terminal.
  `bidi=on` (terminals with no bidi, most of them): Astrolabe CLI applies the
  Unicode bidi algorithm per line (`x/text/unicode/bidi`), mirrors brackets,
  and shapes Arabic into Presentation Forms-B (isolated / initial / medial /
  final, lam-alef ligatures, hamza in its isolated form, harakat kept on
  their base cell) so it reads joined and right-to-left in any terminal with
  the glyphs. `bidi=runs` (kitty): kitty does no bidi but shapes each run of
  same-font cells with HarfBuzz and lays out a run whose first real script is
  Arabic or Hebrew right to left, reversing it in place; Astrolabe CLI emits the line
  as for `on` but with the text of every such run pre-reversed, so kitty's
  reversal restores visual order. A terminal run here is a maximal stretch of
  non-blank cells that are all strong right-to-left letters (or all
  Arabic-script digits) with the same bold/italic face; only the cells' text
  moves, styles stay on their cells. The transform (`text.RTLRuns`) is
  applied last: in `Screen`'s flush on the final composed rows (the diff
  compares what was emitted), in `Encoder.AppendSpans` for `astrolabe render`,
  and line by line on the CLI's human output (`term.RunsLine`). Known limit:
  with a main font that itself has the Arabic forms, kitty folds adjacent
  punctuation into the run and it may land on the other side of the word.
  `bidi=off` (terminals that implement bidi): logical order is emitted
  untouched. `auto` (default): `runs` for kitty (`TERM` containing `kitty`,
  `KITTY_WINDOW_ID`, `TERM_PROGRAM`/`TERMINAL_EMULATOR=kitty`; a `TERM` that
  names another terminal overrides inherited kitty variables; inside tmux
  (`TMUX` set and `TERM` tmux* or screen*, so a `TMUX` inherited by a
  terminal started from a pane is ignored) the attached client's
  `#{client_termname}` decides, asked with a 150 ms timeout); `off` when `VTE_VERSION`, `KONSOLE_VERSION`,
  `TERM_PROGRAM=Apple_Terminal` or `TERM=mlterm*`; else `on`. No terminal
  query (XTVERSION) is sent: over SSH a late answer would leak into the
  input. `astrolabe doctor` prints the mode and the reason. Piped output is never
  reordered or shaped.
- **Search folding**: every search compares `text.Fold` of the query and the
  text (NFC, case folding, Arabic harakat/tatweel/controls dropped, alef
  forms, ى/ي, ة/ه, ؤ/و, ئ/ي, ک/ك, ی/ي unified, presentation forms to letters,
  Arabic-Indic and Persian digits to ASCII), with an offset map back to the
  original for highlighting. Fuzzy ranking treats the position after the
  article and attached particles as a word start. Reordered lines are
  searched in logical order and matches mapped to their visual cells.
- **Input**: raw mode; decode CSI/SS3 sequences, kitty/xterm modified keys
  where present, bracketed paste, `Esc` vs Alt with a 25 ms timeout, resize
  (SIGWINCH), suspend (`Ctrl-z`) and resume restoring the screen. Mouse: wheel
  scrolls and a click places the cursor/follows a link; mouse reporting is on
  only in the TUI and can be disabled (`mouse=off`) so terminal selection
  works. The alternate screen is always restored, including on panic.
- **Output**: a cell buffer diffed against the previous frame, flushed in one
  write, wrapped in synchronized-update marks; no flicker over SSH. Wide and
  zero-width graphemes occupy the right number of cells.
- Works inside tmux (no reliance on passthrough) and `tmux display-popup`.

## 7. Themes

Semantic tokens: `ground`, `raised`, `hover` (soft-accent ground), `text`,
`muted`, `faint`, `heading`, `accent`, `accentSoft`, `border`, `danger`,
`ok`, `link`, `code*` (comment, string, number, keyword), callout hues.

- `onyx` (default): strictly neutral — every ground and ink a pure grey
  (R=G=B): ground `#111111`, raised `#1a1a1a`, hover `#232323`, text
  `#e4e4e4`, muted `#9c9c9c`, faint `#686868`, heading `#f6f6f6`. Gold
  `#d4a72c` is the accent only (mark, bullets, quote bar, chevrons, tag `#`,
  links, legend keys, selection bar, cursor bar, READ pill, dirty dot);
  callouts and code use a calm blue, green, teal and red, never purple. A
  test checks that its greys are grey.
- `sidereal`: a deep night-sky room — ground `#0b0e17`, raised
  `#121726`, cool text `#d8def0`, accent violet `#a394ff` (bullets, bars,
  chevrons, title), links cyan `#6fd3f7`, amber `#ffc777` only for warnings
  and tasks due today or tomorrow. In 16 colours its accent is magenta and
  its links cyan.
- `iron-gall`: Astrolabe's original warm room — warm near-black ground
  `#16130e`, raised `#1e1a13`, ivory text `#eae2d0`, gold-leaf accent
  `#c9a227`, heading a warm gold-cream, borders a brown hairline.
- `parchment`: the light room — paper `#f2ebda`, ink `#33291a`, accent
  `#7a5f14`.
- `graphite`: neutral dark `#0d1117`-family ground with the gold accent.
- `mocha`: matches a Catppuccin-Mocha Neovim/tmux setup (ground `#1e1e2e`,
  text `#cdd6f4`, accent mauve `#cba6f7`) so Astrolabe CLI sits inside rihla.
- Body text ≥ 7:1 on ground, muted ≥ 4.5:1, faint ≥ 3:1; a test enforces it.

## 8. Export

`astrolabe export NOTE` writes one self-contained HTML file (inline CSS, no
scripts, no external requests) in the manuscript style: serif body, gold
accents, the same callouts/tables/tasks, `dir="auto"` on blocks, light and
dark via `prefers-color-scheme`. Wikilinks become relative `.html` links.
`astrolabe render` covers plain-text and ANSI export.

## 9. Configuration (all optional)

`$XDG_CONFIG_HOME/astrolabe-cli/config`, `key = value` lines, `#` comments:
`dir`, `theme`, `measure` (default 78), `ground` (on/off), `bidi`, `mouse`,
`daily_dir`, `daily_format`, `editor` (`builtin` | `external`: which one `i`
opens), `ascii`. Each has an environment override `ASTROLABE_<KEY>` and most a
flag. Unknown keys are ignored with one warning from `astrolabe doctor`.

## 10. Distribution

- `install.sh`: POSIX sh, no root; detects OS/arch, downloads the release
  binary with `curl` or `wget` to `${ASTROLABE_BIN_DIR:-$HOME/.local/bin}/astrolabe`,
  verifies the SHA-256 against the release's `checksums.txt` when a sha tool
  exists, says how to add the directory to `PATH` if it is not there, and
  supports `ASTROLABE_VERSION=vX.Y.Z` and an offline mode
  (`install.sh --from ./astrolabe-linux-amd64`). Re-running upgrades in place.
- Release assets: `astrolabe-<os>-<arch>` (raw binaries, so a locked-down machine
  can fetch one file with a browser and `chmod +x` it) and `checksums.txt`.
- CI (GitHub Actions): vet + tests on Linux and macOS; cross-build all four
  targets; run the Linux binary and `install.sh` as a non-root user in
  `centos:7`, `rockylinux:9`, `debian:stable-slim` and `alpine` containers; on
  a `v*` tag, build and publish the release assets.
- Licence: MIT.

## 11. Out of scope for v1 (say so in the README)

Graph view, image display, sync, plugins or scripting, Org-style clocking and
recurring tasks, a real table/spreadsheet editor, publishing, LaTeX rendering,
multiple windows/splits in the editor, macros and marks.
