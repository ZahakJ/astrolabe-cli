# Install

Astrolabe CLI is one static binary of about 5.5 MB with no runtime dependencies: no Go, no libc version to match and no Nerd Font. Installing it means putting that file on your `PATH`.

## The one-line install

```sh
curl -fsSL https://raw.githubusercontent.com/ZahakJ/astrolabe-cli/main/install.sh | sh
```

or, where there is no curl:

```sh
wget -qO- https://raw.githubusercontent.com/ZahakJ/astrolabe-cli/main/install.sh | sh
```

The script picks the binary for this machine (`astrolabe-linux-amd64`, `-linux-arm64`, `-darwin-amd64` or `-darwin-arm64`) from the latest release, checks it against the release's `checksums.txt`, runs it once to make sure it works here, and puts it in `~/.local/bin`. It needs no root. It also creates the short alias `ast` next to it, unless another `ast` is already on your `PATH` (it says so) or `ASTROLABE_NO_ALIAS=1` is set. At the end it tells you whether `~/.local/bin` is on your `PATH` and, if not, the line that adds it.

A real run, on a machine where another program already owns the name `ast`:

```console
$ curl -fsSL https://raw.githubusercontent.com/ZahakJ/astrolabe-cli/main/install.sh | sh
installed /home/you/.local/bin/astrolabe
  astrolabe v0.2.1 (linux/amd64, go1.22.12)
note: another `ast` is on your PATH (/home/you/.local/bin/ast); the short alias was not created

Try: astrolabe help
```

Options go after `sh -s --`, for example `… | sh -s -- --version v0.2.0`:

| Option | Effect |
|---|---|
| `--version vX.Y.Z` | install that release instead of the latest (also `ASTROLABE_VERSION`) |
| `--bin-dir DIR` | install into `DIR` instead of `~/.local/bin` (also `ASTROLABE_BIN_DIR`) |
| `--from FILE` | install a binary you already have, offline; a `checksums.txt` next to it is verified |
| `--uninstall` | remove the binary and its `ast` alias |
| `--remove-folio` | remove a `folio` binary (the former name) in the same directory |

`ASTROLABE_BASE_URL` points the script at a mirror that holds `astrolabe-<os>-<arch>` and `checksums.txt`, for machines that cannot reach GitHub.

## By hand

Download one file from the [releases page](https://github.com/ZahakJ/astrolabe-cli/releases), `chmod +x` it, and put it anywhere on your `PATH`. That is all the installer does.

## Check it

```sh
astrolabe version
astrolabe doctor
```

`doctor` reports what Astrolabe CLI detected about your terminal (colour depth, glyphs, the bidi mode for right-to-left text), which vault it chose and why, and your config file, and ends with a test card of colours and glyphs. If something looks wrong later, its output is the first thing to look at.

## Build from source

You need Go 1.22 or later.

```sh
git clone https://github.com/ZahakJ/astrolabe-cli && cd astrolabe-cli
make build          # ./astrolabe, static, stripped
make test           # go vet + go test ./...
make install        # into ~/.local/bin (PREFIX=... to change)
make dist           # all four release binaries and checksums.txt in dist/
```

`scripts/build.sh VERSION` is the release build: `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=VERSION"` for linux/amd64, linux/arm64, darwin/amd64 and darwin/arm64, then `dist/checksums.txt`.

## What was tested

The installer was run over HTTP (curl and wget) and offline with `--from` in fresh `centos:7`, `rockylinux:9`, `debian:stable-slim` and `alpine` containers as a non-root user. The macOS binaries have only run in CI and the arm64 binaries are cross-compiled and unrun; see [Verified on](verified.md).

## Uninstall

`sh install.sh --uninstall` (or `curl … | sh -s -- --uninstall`) removes the binary and the alias. Your notes are never touched. The config is in `~/.config/astrolabe-cli/` and the state (recent notes, recovered buffers) in `~/.local/state/astrolabe-cli/`; delete those folders to remove every trace.
