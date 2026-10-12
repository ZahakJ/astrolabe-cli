## What this changes

<!-- One paragraph. Link the issue if there is one. -->

## How it was verified

- [ ] `gofmt -l .` is empty, `go vet ./...` and `go test ./...` pass
- [ ] Rendering changes come with a golden file in `internal/render`
- [ ] Keys or verbs that changed are reflected in `docs/keymap.md` or `docs/cli.md`, and in `CHANGELOG.md`
- [ ] Captures regenerated with `scripts/shots.sh` if the look changed

## Terminals

<!-- Which terminals and colour depths you tried (truecolor, 256, 16, none; kitty, foot, tmux). Right-to-left text needs a look in kitty and in one terminal that does its own bidi. -->
