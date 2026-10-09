# Changelog

## v0.2.1

### It remembers where your notes are

- `astrolabe -C ~/my-notes` once is enough: the vault is written to the config key `dir` and stderr says once ``Using ~/my-notes from now on; `astrolabe vault` to change.`` A plain `astrolabe` then opens it from any directory, instead of falling through to an empty `~/notes` and the tutorial. A later `-C` with another folder replaces it; a folder found by its `.obsidian/`/`.astrolabe/` marker is remembered only while nothing is. `$ASTROLABE_DIR` still wins for its environment and is never remembered.
- New resolution order: `-C`, `$ASTROLABE_DIR`, the vault you stand in (marker above the working directory), the remembered vault, `~/notes`, the working directory if it holds notes, a new `~/notes`. The remembered vault now beats `~/notes` and the working-directory guess (before, config `dir` came before markers). A remembered folder that no longer exists is skipped with a one-line warning.
- The Welcome note opens by itself only on the very first run: nothing remembered, and `~/notes` new and empty. A state file `used` keeps it from coming back. `astrolabe learn` opens it on purpose (`-p` prints it).
- `astrolabe vault [DIR | --forget]`: the vault a plain run uses, the rule that chose it and the remembered vault (only the path in a pipe); `DIR` remembers another folder; `--forget` clears it.
- `astrolabe doctor` shows the remembered vault, says when the vault you stand in won over it, and warns when the config cannot be written or the remembered folder is gone.
- The status bar names the vault before the breadcrumb, `✦ my-notes › runbooks › Lantern cutover`, and the home screen shows it as a home-relative path (`~/notes`).
- `vault` and `learn` are now reserved words; a note with one of those names opens with `astrolabe ./vault.md`.

## v0.2.0

### Renamed: folio is now Astrolabe CLI

- The command is `astrolabe` (the installer adds the alias `ast`; `ASTROLABE_NO_ALIAS=1` skips it), the module and repository `github.com/ZahakJ/astrolabe-cli`, the release assets `astrolabe-<os>-<arch>`.
- Environment `ASTROLABE_*`, config `$XDG_CONFIG_HOME/astrolabe-cli/config`, state `$XDG_STATE_HOME/astrolabe-cli/`, vault marker `.astrolabe/`, ignore file `.astrolabeignore`.
- For existing users of the former name: the old `folio` config and state directories are read when the new ones are absent (nothing is moved), `FOLIO_DIR` still names the vault, `.folio/` still marks a vault and `.folioignore` is read when `.astrolabeignore` is absent. The installer reports an old `folio` binary and removes it only with `--remove-folio`.

### Right-to-left text in kitty

- Arabic was unreadable in kitty: letters mirrored inside every word. kitty
  does no bidi, but it lays out each run of right-to-left letters right to
  left while shaping, which reversed the words it had already put in
  visual order. A third bidi behaviour, `runs`, emits each such run
  pre-reversed so kitty's own reversal restores it; colours, highlights,
  selections and the cursor stay on their cells. It is applied last, on the
  finished screen rows (TUI), on `astrolabe render` output and on the CLI's
  human output.
- `--bidi auto|on|off|runs` (config `bidi`, env `ASTROLABE_BIDI`). `auto` picks
  `runs` in kitty (`TERM`, `KITTY_WINDOW_ID`, `TERM_PROGRAM`; inside tmux it
  asks tmux which terminal is attached), `off` in terminals that do bidi
  themselves, `on` elsewhere. `astrolabe doctor` prints the mode and why.
- Hamza is shaped into its presentation form, so a shaped word draws all its
  letters from one font.
- Input fields (finder, search, prompts, `:` and `/`) show Arabic shaped and
  in visual order, with the caret on the right cell.
- Right-to-left titles are truncated at their logical end, with the
  ellipsis on the visual left; Arabic folder names on the home screen,
  tags, completion menus and status messages are shaped and ordered.

### Search that understands Arabic

- One search folding everywhere (finder, `astrolabe pick`, vault search,
  in-note `/`, tags filter, `[[` and `:e` completion, `astrolabe find`,
  `astrolabe ls`, note resolution): harakat, tatweel and invisible controls are
  ignored; أ إ آ ٱ match ا, ى matches ي, ة matches ه, ؤ and ئ match و and ي,
  Persian letters match Arabic ones, presentation forms match letters,
  Arabic-Indic digits match ASCII digits; Unicode NFC and case folding.
  Highlights land on the original letters. `re:` stays literal.
- Fuzzy ranking treats the word after the article and attached particles
  (ال، وال، بال، لل، و، ب، ل، ف، ك) as a word start.
- In-note `/` search finds words on reordered right-to-left lines and
  highlights their visual cells.

### Look

- New default theme `onyx`: pure neutral greys with gold as the only
  accent. `sidereal` (deep night sky, violet and cyan) is new too;
  `iron-gall`, `parchment`, `graphite` and `mocha` remain.
- The Astrolabe mark on the home screen, drawn in braille (ASCII with
  `--ascii`), and in the README header (`docs/logo.svg`).
- Tasks due today or tomorrow show their date in the warning hue.
