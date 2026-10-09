# Configuration and themes

This page covers the six themes, every configuration key with its environment variable and flag, and the other environment variables Astrolabe CLI reads. Back to the [README](../README.md).

## Themes

There are six themes: `onyx` (the default: pure neutral greys with gold as the only accent, on the mark, bullets, links and bars), `iron-gall` (a warm near-black ground, ivory text and a gold accent), `parchment` (the light version of iron-gall), `graphite` (a neutral dark ground with the gold accent), `mocha` (Catppuccin) and `sidereal` (a deep blue-black night sky with violet and cyan; in 16 colours it uses magenta and cyan where the others use yellow). A test checks every theme for contrast: body text at least 7:1, muted text at least 4.5:1 and faint text at least 3:1. `Space T` cycles the themes, and `:theme NAME` picks one; both save the choice in the config file.

![Astrolabe CLI in the parchment theme](shots/parchment.svg)

![Astrolabe CLI in the iron-gall theme](shots/iron-gall.svg)

![Astrolabe CLI in the sidereal theme](shots/sidereal.svg)

![Astrolabe CLI in the mocha theme](shots/mocha.svg)

## Configuration

**Configuration is optional.** The file is `~/.config/astrolabe-cli/config` (`$XDG_CONFIG_HOME/astrolabe-cli/config`). It holds `key = value` lines, and `#` starts a comment. Every key can also be set by an environment variable `ASTROLABE_<KEY>`, which takes precedence over the file. `astrolabe doctor` warns once about unknown keys.

| Key | Values (default) | Effect | Override |
|---|---|---|---|
| `dir` | path | the remembered vault: written by the first `-C DIR` and by `astrolabe vault DIR`, used when there is no `-C`, `$ASTROLABE_DIR` or vault marker above the working directory | `ASTROLABE_DIR`, `-C DIR` |
| `theme` | `onyx` `iron-gall` `parchment` `graphite` `mocha` `sidereal` (`onyx`) | colours | `ASTROLABE_THEME`, `--theme` |
| `measure` | columns (`78`) | maximum width of the page text | `ASTROLABE_MEASURE`, `:set wrap=N` |
| `ground` | `on` `off` (`on`) | paint the theme's background at 256 colours and above; `off` keeps the terminal's own | `ASTROLABE_GROUND` |
| `bidi` | `auto` `on` `off` `runs` (`auto`) | how right-to-left text reaches the terminal: reordered and shaped (`on`), also pre-reversed for kitty (`runs`), or untouched (`off`) | `ASTROLABE_BIDI`, `--bidi` |
| `mouse` | `on` `off` (`on`) | mouse reporting in the TUI | `ASTROLABE_MOUSE` |
| `daily_dir` | folder (`daily`, or Obsidian's setting) | where daily notes live | `ASTROLABE_DAILY_DIR` |
| `daily_format` | Moment tokens (`YYYY-MM-DD`) | daily note file name; supports `YYYY YY MMMM MMM MM M DD D dddd ddd` | `ASTROLABE_DAILY_FORMAT` |
| `editor` | `builtin` `external` (`builtin`) | what `i`, `a` and `e` open | `ASTROLABE_EDITOR` |
| `ascii` | `on` `off` (`off`) | ASCII glyphs only | `ASTROLABE_ASCII`, `--ascii` |

Boolean values accept `on`/`off`, `true`/`false`, `yes`/`no` and `1`/`0`. `daily_dir` and `daily_format` override `.obsidian/daily-notes.json`. For example:

```ini
# ~/.config/astrolabe-cli/config
dir = ~/notes
theme = parchment
measure = 72
editor = external
```

**Other environment variables:**

- `NO_COLOR` turns colour off; `COLORTERM` and `TERM` are used to detect colour depth.
- `VISUAL`, then `EDITOR` (default `vi`), choose the external editor.
- `XDG_CONFIG_HOME` and `XDG_STATE_HOME` move the config and state files.
- `ASTROLABE_DEBUG=1` prints the first-frame time on exit and shows timings in the finder and search overlays.
- The installer reads `ASTROLABE_VERSION`, `ASTROLABE_BIN_DIR` and `ASTROLABE_BASE_URL`; see `sh install.sh --help`.
