# Contributing to Astrolabe CLI

Thank you for looking. The tool is one static Go binary with no runtime dependencies, and keeping it that way is the first rule.

## Before you start

- Read `DESIGN.md` (the contract the code is built against) and `docs/design-decisions.md` (why it is built this way and what is out of scope).
- Read the [manual](https://zahakj.github.io/astrolabe-cli/manual/). The keymap and the shell reference there are the specification for behaviour.
- For anything larger than a fix, open an issue first.

## Setting up

```sh
git clone https://github.com/ZahakJ/astrolabe-cli.git && cd astrolabe-cli
go build ./cmd/astrolabe && ./astrolabe -C examples/vault     # the sample vault
go test ./...
```

`scripts/build.sh` cross-compiles the release binaries; `scripts/shots.sh` regenerates the captures from the sample vault in a private tmux server.

## Rules of the code

- No cgo, no runtime dependencies, no Nerd Font, no network. Everything degrades: no truecolour, no Unicode, a 40×10 window.
- Notes stay plain Markdown files; nothing is written into the vault except the note being edited, and writes are atomic.
- Rendering changes come with a golden-file test in `internal/render`; every key and verb that changes is reflected in `docs/keymap.md` or `docs/cli.md` and in `CHANGELOG.md`.
- Right-to-left text has three terminal behaviours (`docs/terminals.md`); a change to shaping or reordering needs a look in kitty and in a terminal that does its own bidi.

## Pull requests

One change per pull request, with the template filled in. Commit messages say what changed and why, in the imperative.

## Reporting problems

Bugs and feature requests go through the issue templates; paste the output of `astrolabe doctor`. Security problems go through [private vulnerability reporting](https://github.com/ZahakJ/astrolabe-cli/security/advisories/new); see `SECURITY.md`.
