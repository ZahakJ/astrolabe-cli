# Verified on

This page lists where and how Astrolabe CLI was tested, and what has not been verified. Back to the [README](../README.md).

Everything below was run on the real binary; captures were read by eye.

| Where | What | Result |
|---|---|---|
| Arch Linux host, private tmux server | Every reader and editor key in the keymap, finder, search, agenda, tags, capture, links, folds, task toggle, save, conflict prompt, `$EDITOR` round trip, suspend/resume, popup, stdin mode, `astrolabe pick`; sizes from 140×40 down to 40×10; truecolour, 256, 16 colours, `NO_COLOR`, `--ascii`; all four themes | pass |
| A 1,525-note Obsidian vault (long notes, wide tables, code, Arabic, Japanese) | Every note through `render` and `export`; TUI browse; checksum manifest before and after read-only use; task toggle, capture and new note on a writable copy | no failures; read-only use changed nothing; a task toggle changed one byte |
| `centos:7`, `rockylinux:9`, `debian:stable-slim`, `alpine` containers, fresh non-root user | `install.sh` over HTTP (curl and wget) and offline with `--from`; every verb; the TUI under a pty (open, follow a link, edit and save, capture, search) | pass |
| Real terminal windows on a Wayland desktop (screenshots read by eye, sample vault) | Arabic and mixed Arabic/English on the start page, reader, finder, vault search, in-note `/`, tags, agenda, tree, context panel, editor (Normal and Insert, typed Arabic, caret), `[[` completer, `astrolabe pick`, and CLI output — in **kitty 0.48** directly and inside tmux, and in **foot** directly and inside tmux | pass |
| Konsole (inside tmux) | The same surfaces with `bidi = off` | words read correctly, but Konsole's own whole-line bidi moves text across TUI columns; not recommended for right-to-left notes |
| CI | vet and tests on Linux and macOS; cross-build; the container smoke test | see the badge on the Actions tab |

Not verified: the macOS binaries have never been run by hand (the test suite
runs on a macOS CI runner, the pty tests are Linux-only); the arm64 binaries
are cross-compiled and unrun; containers share the host kernel, so CentOS 7's
3.10 kernel itself was not exercised. Right-to-left text was checked in real
kitty and foot windows only: WezTerm, Alacritty, Ghostty, iTerm2, GNOME
Terminal and Apple Terminal are untested, and a terminal that reverses
right-to-left runs like kitty but is not detected needs `--bidi runs`. In
kitty, punctuation touching an Arabic word can land on the wrong side when the
main font itself contains the Arabic glyphs, and a vowel mark on a lam-alef
ligature can render misplaced. The 256- and 16-colour looks of the new default
theme were checked in captures, not in real windows. Mouse clicks inside
overlays do nothing.
