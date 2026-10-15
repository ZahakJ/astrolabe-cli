# Internals

How Astrolabe CLI is put together. User-facing behaviour is documented in
[keymap.md](keymap.md), [cli.md](cli.md) and [configuration.md](configuration.md);
this page covers the model underneath. Section numbers are referenced from code
comments.

## 1. Principles

1. **The files are the only state.** No database, no index on disk, no sidecar
   files in the vault. A file is rewritten only when the user edits it, and an
   edit touches only the bytes that changed (line endings, trailing newline,
   frontmatter, BOM and unknown syntax are preserved). Writes are atomic (temp
   file in the same directory, then rename), keep the file mode, and refuse to
   overwrite a file that changed on disk since it was read.
2. **Nothing to configure.** Every option has a working default (§9).
3. **Fast to first paint.** The requested note is painted first and the vault
   is scanned concurrently afterwards.
4. **Typography first.** Hierarchy is built from weight, ink and space (§5).
5. **Degrade, never break.** No truecolour, no Unicode, a 60×16 window, SSH,
   tmux and a pipe are all supported (§6).

## 2. Build and layout

- Go, module `github.com/ZahakJ/astrolabe-cli`, `go 1.22`. Release builds use
  `CGO_ENABLED=0 -trimpath -ldflags "-s -w -X main.version=…"`: a static binary
  with no libc dependency. Targets: linux/amd64, linux/arm64, darwin/amd64,
  darwin/arm64. Size budget 6 MiB, checked in CI.
- Dependencies: the standard library, `golang.org/x/sys`, `golang.org/x/term`,
  `golang.org/x/text` (`unicode/bidi`, `width`, `unicode/norm`) and
  `github.com/rivo/uniseg`. The terminal layer, Markdown parser, renderer,
  highlighter and editor are in this repository.
- No Nerd Font glyphs, no network access at runtime.
- Files outside the vault: `$XDG_CONFIG_HOME/astrolabe-cli/config` (written
  only for `theme` and `dir`), `$XDG_STATE_HOME/astrolabe-cli/recent` (recent
  notes and last position) and `$XDG_STATE_HOME/astrolabe-cli/recovered/` (an
  unsaved buffer that could not be written when the process was signalled).
- `internal/render` has golden-file tests.
- Fixtures and captures use `examples/vault` only.

```
cmd/astrolabe/main.go    dispatch only
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
examples/vault           sample notes (test and capture fixture)
scripts/                 build and capture (tmux → text/SVG/PNG) helpers
install.sh               installer
```

## 3. Vault model

- **Root.** Tried in order: `-C DIR`/`--dir`, `$ASTROLABE_DIR`, the nearest
  ancestor of the working directory containing `.obsidian/` or `.astrolabe/`,
  the remembered vault (config `dir`; skipped with a warning when it no longer
  exists), `~/notes` if it exists, the working directory if it directly
  contains any `.md`, else `~/notes` (created on first write). `astrolabe
  doctor` and `astrolabe vault` report which rule chose.
- **Remembering.** A vault opened with `-C DIR` is written to the config as
  `dir = <absolute path>` (other lines and comments kept) and stderr says so
  once on a terminal. A vault found by its marker is remembered only while
  nothing is. Never remembered: `$ASTROLABE_DIR`, the `~/notes` default, a
  vault that failed to open, the vault of a single file opened from outside
  it, and runs of `doctor` and `vault`. A failed write is not fatal; doctor
  warns when the config is not writable.
- **First run.** `Welcome.md` is written and opened by a plain `astrolabe` only
  when nothing is remembered, the vault is a new, empty `~/notes`, and the
  state file `used` is absent. `astrolabe learn` opens it on purpose.
- A file opened from outside any vault belongs to the nearest ancestor with a
  marker, else to its own directory.
- The scan skips dot-directories, `node_modules` and globs in a top-level
  `.astrolabeignore`. File symlinks are followed, directory symlinks are not.
- **Identity** is the vault-relative path. The title is frontmatter `title`,
  else the first H1, else the file name without `.md`.
- **Frontmatter** is a leading `---` YAML block. Only `title`, `tags`,
  `aliases`, `date` and `created` are parsed; other keys are kept as raw lines
  for display. It is never re-serialised.
- **Links.** `[[Note]]`, `[[Note|label]]`, `[[Note#Heading]]`,
  `[[Note#^block]]`, `[[folder/Note]]`, embeds `![[Note]]` / `![[image.png]]`,
  and Markdown links. Resolution follows Obsidian: exact relative path, then
  the shortest matching path by basename (case-insensitive, `.md` optional),
  then aliases. Following an unresolved link offers to create the note.
- **Tags**: `#tag` and `#nested/tag` in body text (not in code or URLs, not a
  bare number, not a heading marker) plus frontmatter `tags`.
- **Tasks**: `- [ ]`, `- [x]`, `[/]` in progress, `[-]` cancelled; due date as
  `📅 2026-10-05`, `due:2026-10-05` or `@due(2026-10-05)`; priority
  `!`/`!!`/`!!!` or `⏫ 🔼 🔽`.
- **Daily notes**: folder and format from `.obsidian/daily-notes.json`
  (Moment tokens `YYYY MM DD`), else `daily/YYYY-MM-DD.md`. A new daily note
  starts with `# <weekday>, <D Month YYYY>`.
- **Capture** appends one newline-terminated line to today's daily note
  (`- HH:MM text`, or `- [ ] text` for a task) and changes nothing else.
- **Search** scans the files concurrently on every query: smart-case
  substring, `re:` for a regexp, `tag:` `path:` `title:` filters, terms ANDed.
  Ranked title hits, then heading hits, then body.
- The link graph (links, backlinks with a context line, tags, tasks) is built
  in memory per run by a line scanner that respects code fences and does not
  need the full AST. Files over 2 MB are indexed by title only.

## 4. Shape

Two faces over the same vault: shell verbs that do one thing and exit, and the
TUI, which opens in the reader with the built-in editor one key away and
`$EDITOR` one further. Switching between reader and editor keeps the place:
the editor opens on the source line of the block under the reader's cursor,
and leaving it returns to the same block.

### 4.1 Shell verbs

The verbs and flags are listed in [cli.md](cli.md). Conventions: results on
stdout, messages on stderr; plain and stable output when stdout is not a
terminal; exit 0 success, 1 nothing found, 2 usage error, 130 cancelled.
Global flags go before or after the verb. Verb names are reserved and matched
exactly (no prefixes, since a note may be called `to`); `astrolabe ./add.md`
opens a file with a reserved name. An argument that resolves to no note is an
error listing the nearest matches.

### 4.2 TUI layout

```
┌ tree (opt) ┬──────────── page ────────────┬ context (opt) ┐
│ folders    │   centred measure ≤ 78 cols  │ outline       │
│ notes      │   the rendered note          │ backlinks     │
├────────────┴──────────────────────────────┴───────────────┤
│ ✦ folder › Note title            1,240 words · 5 min  READ │
└────────────────────────────────────────────────────────────┘
```

- The page gets its full measure first. The context panel opens by default at
  ≥ 124 columns, the tree only on request; toggled on in a narrow window
  either overlays rather than squeezing the page below 50 columns. Below 60
  columns the status bar keeps the note name and mode pill. Minimum 40×10;
  smaller shows "window too small".
- Status bar: `✦`, the vault's short name, the breadcrumb
  (`✦ my-notes › runbooks › Lantern cutover`; folders ellipsise first, then the
  vault, the note name last), a dirty dot `●`; on the right words · reading
  time (reader) or `line:col` (editor) and the mode pill. Messages replace the
  left side for 3 s. The `:` and `/` lines replace the bar while typing.
- Overlays (finder, search, leader palette, help, tags, agenda, prompts) are
  centred raised panels at most 80×24 with a one-line input on top and the
  selected row marked by an accent bar. `Esc` closes the topmost.
- Home screen: the mark in braille (ASCII with `--ascii`, a single `✦` when
  short), "The vault is open.", the note count, recent notes and a key legend.

### 4.3 Keys

The full keymap is in [keymap.md](keymap.md); `internal/tui/keymap.go` is the
reader's single table. The reader has a line cursor (an accent `▎` in the
gutter) and actions apply to the block under it. Pausing 300 ms after `Space`
shows the leader palette. The editor is a Vim subset: `j`/`k` move by display
line on wrapped lines, `Esc` in Normal mode saves a dirty buffer and returns
to the reader (`:q!` discards), `i`/`a` from the reader open it in Insert mode
at the start or end of the line, and `Ctrl-t` inserts a timestamp. Right-to-left
lines are displayed in visual order with the cursor mapped through the
reordering; editing stays in logical order.

## 5. Rendering

Terminals have one font size, so hierarchy comes from weight, ink, space,
rules and ornament. The measure is `min(78, width − margins)`, centred when
the pane is wider. Text wraps at word boundaries, unjustified, one blank line
between blocks. Widths are in cells per grapheme cluster.

- **Title block**: title in heading ink, bold; a muted chip line
  (`date · #tag #tag · N backlinks`); a hairline. Other frontmatter folds into
  `▸ properties · N`. A leading H1 equal to the title is not repeated.
- **Headings**: H1 bold with a full-measure `─` rule; H2 bold heading ink with
  two blank lines above; H3 bold body ink; H4–H6 italic muted. Each has a fold
  chevron in the gutter (`▾` / `▸`, folded sections show `… N lines`).
- **Inline**: bold, italic, strikethrough (faint + strike), `==highlight==`
  (accent-soft ground), inline code (raised ground with a cell of padding, or
  accent backticks without a ground).
- **Links** in accent without brackets; external URLs get a faint `↗`; broken
  links in danger ink, underlined; the highlighted link is reverse-video. Bare
  URLs longer than half the measure are shortened to host + `…`. OSC 8 where
  supported.
- **Lists**: bullets `•` `◦` `▪` by depth, ordered numbers right-aligned,
  hanging indent. Tasks `☐` `☑` `◐` `☒`; due dates as chips, danger when
  overdue.
- **Quotes**: accent `▎` bar, italic muted text, one bar per level.
- **Callouts** (`> [!type] Title`, `+`/`-` fold markers): bar and bold label
  in the callout hue with a glyph (`¶` note, `✎` abstract, `✓` success, `?`
  question, `!` warning, `✗` failure/danger, `☞` tip, `❝` quote, `≡` example,
  `✱` bug). Unknown types use the note hue.
- **Code**: raised block the width of the measure, language label top-right,
  a small highlighter for sh, python, go, js/ts, json, yaml, c/c++, rust, lua,
  sql and diff. Long lines are clipped with `…` and scroll with `h`/`l`.
  Without a ground the block gets a faint `│` bar.
- **Tables**: hairlines `─ │ ┼`, bold header with a rule, no outer verticals,
  alignment from the delimiter row. Natural widths if they fit; otherwise the
  widest columns shrink and wrap; if a column would fall under 6 cells the
  table switches to a stacked record layout. A table may use the pane width
  beyond the measure before shrinking.
- **Rules** render as `·  ✦  ·`. **Footnotes** as `[1]` markers with
  definitions at the end. **Images** as `▣ alt text`; note embeds show the
  first 12 lines of the target in a quote bar. **Math** and **HTML** blocks are
  shown verbatim; `<br>` breaks lines; `%% … %%` and `<!-- -->` are hidden.
- **Right-to-left**: a paragraph's direction is that of its first strong
  character. RTL blocks are right-aligned with bullets, bars and markers on the
  right edge.

Every rendered line records its source lines and hit regions (links, task box,
fold chevron), so cursor, editing, task toggling and links map back to the file.

## 6. Terminals

- **Colour**: truecolour (`COLORTERM`), 256 (`TERM` contains `256color`), 16,
  or none (`NO_COLOR`, `TERM=dumb`, not a tty). Themes are 24-bit; 256 maps to
  the nearest of the cube and grey ramp; 16 uses the terminal's own
  foreground/background with yellow as accent, bright black as faint, and
  attributes doing the rest, with no painted grounds.
- **Ground**: in truecolour and 256 the theme paints its own ground;
  `ground=off` keeps the terminal's.
- **Glyphs**: a Unicode set and an ASCII set chosen by locale or `--ascii`
  (`✦`→`*`, `•◦▪`→`* - +`, `☐☑`→`[ ] [x]`, `▎`→`|`, `─`→`-`, `▾▸`→`v >`,
  `↗`→`^`, `…`→`...`). The Unicode set uses only glyphs found in DejaVu Sans
  Mono, Menlo and Consolas.
- **Bidi**: three modes.
  - `on` (terminals with no bidi): the Unicode bidi algorithm per line
    (`x/text/unicode/bidi`), mirrored brackets, and Arabic shaped into
    Presentation Forms-B (isolated/initial/medial/final, lam-alef ligatures,
    harakat kept on their base cell).
  - `runs` (kitty): kitty does no bidi but shapes each run of same-font cells
    with HarfBuzz and lays out a run whose first script is Arabic or Hebrew
    right to left, reversing it. The line is emitted as for `on`, with every
    such run pre-reversed so kitty's reversal restores visual order. A run is a
    maximal stretch of non-blank cells that are all strong RTL letters (or all
    Arabic-script digits) with the same bold/italic face; only the text moves,
    styles stay on their cells. The transform (`text.RTLRuns`) is applied last:
    in `Screen`'s flush on the composed rows, in `Encoder.AppendSpans` for
    `astrolabe render`, and per line on CLI output (`term.RunsLine`). With a
    main font that has the Arabic forms itself, kitty folds adjacent
    punctuation into the run.
  - `off` (terminals that implement bidi): logical order, untouched.
  - `auto` picks `runs` for kitty (`TERM` containing `kitty`,
    `KITTY_WINDOW_ID`, `TERM_PROGRAM`/`TERMINAL_EMULATOR=kitty`; a `TERM`
    naming another terminal overrides inherited kitty variables; inside tmux,
    with `TMUX` set and `TERM` tmux* or screen*, the client's
    `#{client_termname}` decides, asked with a 150 ms timeout), `off` for
    `VTE_VERSION`, `KONSOLE_VERSION`, `TERM_PROGRAM=Apple_Terminal` or
    `TERM=mlterm*`, else `on`. No XTVERSION query is sent, since a late answer
    over SSH would land in the input. Piped output is never reordered or shaped.
- **Search folding**: `text.Fold` of query and text (NFC, case folding, Arabic
  harakat/tatweel/controls dropped, alef forms, ى/ي, ة/ه, ؤ/و, ئ/ي, ک/ك, ی/ي
  unified, presentation forms to letters, Arabic-Indic and Persian digits to
  ASCII) with an offset map back to the original for highlighting. Fuzzy
  ranking treats the position after the article and attached particles as a
  word start. Reordered lines are searched in logical order and matches mapped
  to visual cells.
- **Input**: raw mode; CSI/SS3 decoding, kitty/xterm modified keys, bracketed
  paste, `Esc` vs Alt with a 25 ms timeout, SIGWINCH, `Ctrl-z` and resume.
  Mouse reporting only in the TUI (`mouse=off` disables it). The alternate
  screen is restored on every exit path, including panic.
- **Output**: a cell buffer diffed against the previous frame, flushed in one
  write inside synchronized-update marks. Wide and zero-width graphemes take
  the right number of cells.
- Works inside tmux without passthrough, and in `tmux display-popup`.

## 7. Themes

Semantic tokens: `ground`, `raised`, `hover`, `text`, `muted`, `faint`,
`heading`, `accent`, `accentSoft`, `border`, `danger`, `ok`, `link`, `code*`
(comment, string, number, keyword), callout hues.

- `onyx` (default): neutral greys (R=G=B): ground `#111111`, raised
  `#1a1a1a`, hover `#232323`, text `#e4e4e4`, muted `#9c9c9c`, faint
  `#686868`, heading `#f6f6f6`; gold `#d4a72c` as the only accent. Callouts
  and code use blue, green, teal and red. A test checks the greys.
- `sidereal`: ground `#0b0e17`, raised `#121726`, text `#d8def0`, accent
  violet `#a394ff`, links cyan `#6fd3f7`, amber `#ffc777` for warnings and
  near due dates. In 16 colours the accent is magenta.
- `iron-gall`: warm ground `#16130e`, raised `#1e1a13`, ivory text
  `#eae2d0`, gold accent `#c9a227`, brown hairlines.
- `parchment` (light): paper `#f2ebda`, ink `#33291a`, accent `#7a5f14`.
- `graphite`: `#0d1117`-family ground with the gold accent.
- `mocha`: Catppuccin Mocha (ground `#1e1e2e`, text `#cdd6f4`, accent
  `#cba6f7`).
- Contrast floors on ground, enforced by tests: text ≥ 7:1, muted ≥ 4.5:1,
  faint ≥ 3:1.

## 8. Export

`astrolabe export NOTE` writes one self-contained HTML file (inline CSS, no
scripts, no external requests): serif body, gold accents, the same callouts,
tables and tasks, `dir="auto"` on blocks, light and dark through
`prefers-color-scheme`. Wikilinks become relative `.html` links.

## 9. Configuration

`$XDG_CONFIG_HOME/astrolabe-cli/config`, `key = value` lines, `#` comments:
`dir`, `theme`, `measure` (78), `ground`, `bidi`, `mouse`, `daily_dir`,
`daily_format`, `editor` (`builtin` | `external`), `ascii`. Each has an
`ASTROLABE_<KEY>` environment override and most have a flag. `astrolabe
doctor` warns about unknown keys.

## 10. Distribution

- `install.sh`: POSIX sh, no root. Detects OS/arch, downloads the release
  binary with `curl` or `wget` to `${ASTROLABE_BIN_DIR:-$HOME/.local/bin}`,
  verifies it against `checksums.txt` when a SHA-256 tool exists, and prints
  the `PATH` line if needed. `ASTROLABE_VERSION=vX.Y.Z` pins a release;
  `--from FILE` installs offline. Re-running upgrades in place.
- Release assets: raw binaries `astrolabe-<os>-<arch>` and `checksums.txt`.
- CI: vet and tests on Linux and macOS; cross-build; the Linux binary and
  `install.sh` run as a non-root user in `centos:7`, `rockylinux:9`,
  `debian:stable-slim` and `alpine`; a `v*` tag publishes the release.
