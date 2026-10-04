# Using folio with the shell, tmux and Neovim

folio is meant to sit next to the tools you already use. Everything on this page was run against folio's sample vault with tmux 3.7, Neovim 0.12, bash and zsh. Where a snippet depends on a newer version of a tool, the text says so.

Three facts explain most of what follows:

- **Paths.** In a pipe folio prints `path:line:text` or tab-separated columns. Paths are relative to the current directory when you are inside the vault, and absolute otherwise. `new`, `today -p`, `path` and `pick` always print absolute paths.
- **The terminal.** Interactive parts (`folio`, `folio pick`, `folio new -o`) draw on `/dev/tty`. That is why `"$(folio pick)"` works: the picker draws on the terminal while the chosen path goes to stdout.
- **Which vault.** Without `-C`, `FOLIO_DIR` or the config key `dir`, folio picks the vault from the current directory. A command started by tmux, Neovim or a git hook may run in a directory you did not expect, so set `FOLIO_DIR` (or `dir`) once.

## Shell

### Aliases and functions

```sh
# ~/.bashrc or ~/.zshrc
export FOLIO_DIR="$HOME/notes"

alias n='folio add'                      # n "idea"
alias nt='folio add -t'                  # nt "reply to the vendor due:2026-10-09 !!"
alias nf='folio find'                    # nf 'tag:runbook rollback'
alias nl='folio ls -n 10'

# Pick a note and edit it in Neovim. If you cancel the picker (exit 130),
# Neovim does not open.
np() { f=$(folio pick "$@") && nvim "$f"; }

# Create a note and edit it straight away.
nn() { nvim "$(folio new "$@")"; }

# Today's daily note in your editor.
nd() { "${EDITOR:-vi}" "$(folio today -p)"; }
```

`folio new -e TITLE` and `folio new -o TITLE` do the same as `nn` with `$EDITOR` and with the built-in editor.

### Pipes

```sh
# Capture from anything that writes to stdout. Several lines become one
# bullet, with the following lines indented.
pbpaste | folio add                          # macOS clipboard
xclip -o -selection clipboard | folio add    # X11
wl-paste | folio add                         # Wayland

# Append under a heading in a specific note (created if missing).
folio add -n Inbox --at Reading "The Quiet Machine, part II"

# A note from command output.
git log --oneline -20 | folio new "Release notes draft"

# Every search hit in Neovim's quickfix list (path:line:text matches the
# default 'errorformat').
nvim -q <(folio find deploy)

# Paths can contain spaces: read them line by line.
folio ls -l --tag meeting | while IFS= read -r f; do grep -l rollback "$f"; done
folio find -l rollback | tr '\n' '\0' | xargs -0 wc -w  # any xargs

# JSON for scripts.
folio tasks --due --json | jq -r '.[] | select(.group=="overdue") | .text'
folio find --json -n 5 pager | jq -r '.[].abs'

# Render a note for a pager or a mail.
folio render "Lantern migration" | less -R
folio render -w 72 Backpressure > backpressure.txt
folio export Backpressure -o backpressure.html
```

### Exit codes in scripts

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | nothing found (`find`, `ls`, `tasks`), or no note matches a name |
| 2 | usage error |
| 130 | cancelled (`Esc` in `folio pick`) |

When nothing is found, the message goes to stderr only if stderr is a terminal, so a pipeline stays quiet:

```sh
if folio find -l 'tag:oncall pager' >/dev/null; then echo "on-call notes mention the pager"; fi
```

### A commit log in the daily note

Save this as `.git/hooks/post-commit` in a repository and make it executable (`chmod +x`). Each commit adds one line, such as `- 14:02 folio: Fix the retry budget`, to today's daily note:

```sh
#!/bin/sh
folio add "$(basename "$(git rev-parse --show-toplevel)"): $(git log -1 --format=%s)" >/dev/null
```

To use it in every repository, put it in a template directory (`git config --global init.templateDir ~/.git-template`, with the file at `~/.git-template/hooks/post-commit`) or in a global hooks directory (`git config --global core.hooksPath ~/.githooks`). The second replaces each repository's own hooks.

The hook runs inside the repository. Without `FOLIO_DIR` or `dir`, a repository that contains `.md` files would itself be taken as the vault.

### Cron or a systemd timer

`folio add` touches only today's daily note, so it is safe to run unattended:

```sh
# crontab: a line every weekday at 9:00 with the open overdue count
0 9 * * 1-5  FOLIO_DIR=$HOME/notes folio add "overdue: $(folio tasks --json | jq '[.[] | select(.group=="overdue")] | length')"
```

## tmux

### Popups

```tmux
# ~/.tmux.conf

# prefix N: folio in a popup, in the current pane's directory.
bind-key N display-popup -E -w 90% -h 90% -d '#{pane_current_path}' folio

# prefix a / A: capture a line or a task without leaving what you are doing.
bind-key a display-popup -E -w 60% -h 3 -d '#{pane_current_path}' 'printf "capture: "; read -r line && [ -n "$line" ] && folio add -- "$line" >/dev/null'
bind-key A display-popup -E -w 60% -h 3 -d '#{pane_current_path}' 'printf "task: "; read -r line && [ -n "$line" ] && folio add -t -- "$line" >/dev/null'

# prefix F: the finder alone; the chosen note opens in a new window in Neovim.
bind-key F display-popup -E -w 80% -h 80% -d '#{pane_current_path}' 'f=$(folio pick) && tmux new-window nvim "$f"'
```

Notes on these bindings:

- **Use single quotes.** Inside double quotes tmux expands `$line` itself while it reads the config, so the capture would always be empty.
- **`--` ends folio's options**, so a capture that starts with `-` is still text.
- **Popups use tmux's global environment**, not your interactive shell's. If `folio` is in `~/.local/bin` and the popup cannot find it, add `set-environment -g PATH "$HOME/.local/bin:$PATH"` (and `set-environment -g FOLIO_DIR "$HOME/notes"` if you use it), or write the full path to the binary.
- **Default bindings.** `N`, `a`, `A` and `F` have no default binding in tmux 3.7. `C` (customize mode) and `T` are worth avoiding if you use them.
- **The `-E` flag** closes the popup when folio exits.

### Clipboard

folio copies with OSC 52 when you press `Space y` (the note's wikilink), follow a URL, or yank to `"+` in the editor. tmux accepts OSC 52 from applications only with:

```tmux
set -g set-clipboard on
```

With that setting, tmux stores the copy in a paste buffer and passes it on to your terminal if the terminal supports OSC 52. `folio doctor` run inside tmux prints the same reminder.

### Colour and hyperlinks

folio detects truecolour from `COLORTERM`. tmux passes it through if your terminal sets it. If `folio doctor` inside tmux shows 256 colours while the terminal itself has truecolour, add:

```tmux
set -as terminal-features ',*:RGB'
```

Hyperlinks (OSC 8) are emitted inside tmux 3.4 and later.

## Neovim

### Neovim as folio's external editor

When you press `E` in the reader, folio runs `$VISUAL` (else `$EDITOR`, else `vi`) through `sh` with `+LINE file`, so Neovim opens on the line under the reader's cursor. When Neovim exits, folio reloads the note.

To make `i` (and `e`, `a`) open Neovim as well:

```ini
# ~/.config/folio/config
editor = external
```

With `editor = external`, `folio new -e` and `E` behave the same way. The built-in editor is still used for `folio new -o`.

Editors that take a line number:

| Editor | Arguments folio passes |
|---|---|
| vi, vim, nvim, nano, emacs, emacsclient, micro, kak, and others | `+LINE file` |
| hx (Helix) | `file:LINE` |
| code, codium, subl, zed | `-g file:LINE` |
| anything else | `file` |

A value with arguments, such as `EDITOR="emacsclient -t"`, works because it is run through `sh`.

### folio inside Neovim

Save this as `~/.config/nvim/lua/folio.lua` and add `require("folio")` to `init.lua`. It needs Neovim 0.11 or later. On older versions, replace `vim.fn.jobstart(cmd, { term = true, … })` with `vim.fn.termopen(cmd, { … })`.

```lua
-- folio inside Neovim: a floating terminal, a picker that edits the chosen
-- note here, and :Capture.
local function float(cmd, on_exit)
  local w = math.floor(vim.o.columns * 0.9)
  local h = math.floor(vim.o.lines * 0.9)
  local buf = vim.api.nvim_create_buf(false, true)
  local win = vim.api.nvim_open_win(buf, true, {
    relative = "editor", style = "minimal", border = "rounded",
    width = w, height = h,
    row = math.floor((vim.o.lines - h) / 2), col = math.floor((vim.o.columns - w) / 2),
  })
  vim.fn.jobstart(cmd, {
    term = true,
    on_exit = function(_, code)
      if vim.api.nvim_win_is_valid(win) then vim.api.nvim_win_close(win, true) end
      if vim.api.nvim_buf_is_valid(buf) then vim.api.nvim_buf_delete(buf, { force = true }) end
      if on_exit then on_exit(code) end
    end,
  })
  vim.cmd.startinsert()
end

-- <leader>nn: the folio reader in a float.
vim.keymap.set("n", "<leader>nn", function() float({ "folio" }) end, { desc = "folio" })

-- <leader>np: pick a note with folio's finder and edit it here.
vim.keymap.set("n", "<leader>np", function()
  local out = vim.fn.tempname()
  float({ "sh", "-c", "folio pick > " .. vim.fn.shellescape(out) }, function(code)
    local path = code == 0 and vim.fn.readfile(out)[1] or nil
    vim.fn.delete(out)
    if path and path ~= "" then
      vim.schedule(function() vim.cmd.edit(vim.fn.fnameescape(path)) end)
    end
  end)
end, { desc = "folio pick" })

-- :Capture some text   appends a line to today's daily note.
vim.api.nvim_create_user_command("Capture", function(o)
  local r = vim.system({ "folio", "add", "--", o.args }, { text = true }):wait()
  vim.notify(r.code == 0 and ("captured " .. vim.trim(r.stdout)) or vim.trim(r.stderr))
end, { nargs = "+" })
```

How the pieces work:

- **The picker** writes the chosen path to a temporary file. Inside a terminal buffer, stdout is the terminal itself, so the path could not be read from it.
- **`:Capture`** runs `folio add` as a plain process. In a pipe, folio prints `path:line`.
- **Nested Neovim.** If `EDITOR=nvim`, pressing `E` in the floating reader starts a second Neovim inside the float. That works, but you may prefer `i` and the built-in editor there.

Some more mappings you might add:

```lua
-- Today's daily note in the current window.
vim.keymap.set("n", "<leader>nd", function()
  vim.cmd.edit(vim.fn.fnameescape(vim.trim(vim.fn.system({ "folio", "today", "-p" }))))
end, { desc = "folio today" })

-- Search the vault into the quickfix list.
vim.api.nvim_create_user_command("Notes", function(o)
  vim.fn.setqflist({}, " ", { title = "folio find " .. o.args,
    lines = vim.fn.systemlist({ "folio", "find", o.args }) })
  vim.cmd.copen()
end, { nargs = "+" })
```

`:Notes` relies on `'errorformat'`, which matches `path:line:text` by default. If you run it from outside the vault, folio prints absolute paths, which also open.

### Matching a Catppuccin setup

```ini
# ~/.config/folio/config
theme = mocha
```

`mocha` uses the Catppuccin Mocha colours: ground `#1e1e2e`, text `#cdd6f4`, accent mauve `#cba6f7`. If you run Neovim and tmux with a transparent background and want folio to match, add `ground = off`. folio then leaves the terminal's background alone and draws only the text colours.

## rihla

[rihla](https://github.com/ZahakJ/rihla) is a portable Neovim and tmux setup. These instructions are written for that repository. They assume only that it keeps a tmux config file, a Neovim config directory and some bootstrap or install script. Adapt the paths to its layout.

1. **Install folio during bootstrap.** The installer needs no root, puts `folio` in `~/.local/bin`, verifies the release checksum, and upgrades in place when run again:

   ```sh
   if ! command -v folio >/dev/null 2>&1; then
     curl -fsSL https://raw.githubusercontent.com/ZahakJ/folio/main/install.sh | sh \
       || wget -qO- https://raw.githubusercontent.com/ZahakJ/folio/main/install.sh | sh
   fi
   ```

   For a machine without internet access, ship the binary for its platform next to rihla and run `sh install.sh --from ./folio-linux-amd64`. To pin a release, set `FOLIO_VERSION=vX.Y.Z`, and to install elsewhere, set `FOLIO_BIN_DIR=DIR`.

2. **tmux.** Append the popup bindings from [Popups](#popups) to rihla's tmux config. If rihla does not already put `~/.local/bin` in tmux's global environment, add `set-environment -g PATH "$HOME/.local/bin:$PATH"`. Add `set -g set-clipboard on` if rihla does not set it.

3. **Neovim.** Copy `folio.lua` (above) into rihla's Lua directory and require it from its `init.lua`. Before choosing `<leader>n…`, check rihla's existing leader mappings.

4. **folio config.** Write a matching config only when the user has none, so a later personal change survives a re-run of the bootstrap:

   ```sh
   cfg="${XDG_CONFIG_HOME:-$HOME/.config}/folio/config"
   if [ ! -f "$cfg" ]; then
     mkdir -p "$(dirname "$cfg")"
     printf '%s\n' 'theme = mocha' 'editor = external' > "$cfg"
   fi
   ```

   folio rewrites the `theme =` line when the user changes the theme with `Space T` or `:theme`, keeping any other lines and comments. Do not manage this file with a symlink into rihla's repository unless that change should land in the repository.

5. **The vault.** If the notes are not in `~/notes`, export `FOLIO_DIR` from rihla's shell profile. The tmux popups and Neovim mappings then work from any directory.

6. **Check.** Run `folio doctor` inside tmux. It should show truecolour (or 256), Unicode glyphs, the vault you expect, and a config file with no warnings.
