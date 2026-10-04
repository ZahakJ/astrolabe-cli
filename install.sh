#!/bin/sh
# folio installer — https://github.com/ZahakJ/folio
#
#   curl -fsSL https://raw.githubusercontent.com/ZahakJ/folio/main/install.sh | sh
#   wget -qO- https://raw.githubusercontent.com/ZahakJ/folio/main/install.sh | sh
#
# Installs the folio binary for this machine into ~/.local/bin (no root
# needed). Re-running it upgrades in place.
#
# Options (each has an environment variable, for `curl … | sh`):
#   --from FILE        install a binary you already have (offline); if a
#                      checksums.txt sits next to FILE it is verified
#   --version vX.Y.Z   install that release        (FOLIO_VERSION; default latest)
#   --bin-dir DIR      install into DIR            (FOLIO_BIN_DIR; default ~/.local/bin)
#   --uninstall        remove the installed binary
#   -h, --help         show this help
#
# FOLIO_BASE_URL downloads from a mirror instead of GitHub releases: the
# directory must hold folio-<os>-<arch> and checksums.txt (the layout of
# scripts/build.sh's dist/), e.g. FOLIO_BASE_URL=https://mirror.example/folio/v1.0.0
#
# Supported: Linux x86_64 and aarch64, macOS x86_64 and arm64.

set -eu

# Everything runs inside main, called on the last line, so that a download
# cut short by `curl … | sh` executes nothing at all.
main() {

REPO=ZahakJ/folio
prog=folio-install

say() { printf '%s\n' "$*"; }
warn() { printf '%s: warning: %s\n' "$prog" "$*" >&2; }
die() {
	printf '%s: error: %s\n' "$prog" "$*" >&2
	exit 1
}

usage() {
	cat <<'USAGE'
folio installer: puts the folio binary for this machine in ~/.local/bin.

usage: sh install.sh [--from FILE] [--version vX.Y.Z] [--bin-dir DIR] [--uninstall]

  --from FILE        install a binary you already have (offline); a
                     checksums.txt next to FILE is verified
  --version vX.Y.Z   install that release instead of the latest (FOLIO_VERSION)
  --bin-dir DIR      install into DIR (FOLIO_BIN_DIR; default ~/.local/bin)
  --uninstall        remove the installed binary
  FOLIO_BASE_URL     download from a mirror holding folio-<os>-<arch> and
                     checksums.txt, instead of GitHub releases
USAGE
}

have() { command -v "$1" >/dev/null 2>&1; }

# --- arguments ---------------------------------------------------------------

from=""
uninstall=0
version=${FOLIO_VERSION:-}
bin_dir=${FOLIO_BIN_DIR:-}

while [ $# -gt 0 ]; do
	case $1 in
	--from)
		[ $# -ge 2 ] || die "--from needs a file"
		from=$2
		shift 2
		;;
	--from=*)
		from=${1#--from=}
		shift
		;;
	--version)
		[ $# -ge 2 ] || die "--version needs a value such as v1.0.0"
		version=$2
		shift 2
		;;
	--version=*)
		version=${1#--version=}
		shift
		;;
	--bin-dir)
		[ $# -ge 2 ] || die "--bin-dir needs a directory"
		bin_dir=$2
		shift 2
		;;
	--bin-dir=*)
		bin_dir=${1#--bin-dir=}
		shift
		;;
	--uninstall)
		uninstall=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		die "unknown option: $1 (try --help)"
		;;
	esac
done

if [ -z "$bin_dir" ]; then
	[ -n "${HOME:-}" ] || die "HOME is not set; choose a directory with FOLIO_BIN_DIR=/path"
	bin_dir=$HOME/.local/bin
fi
# A literal "~/" (from FOLIO_BIN_DIR='~/bin') is expanded by hand.
# shellcheck disable=SC2088
case $bin_dir in
"~/"*) bin_dir=${HOME:-}/${bin_dir#"~/"} ;;
esac
target=$bin_dir/folio

# --- uninstall -----------------------------------------------------------------

if [ "$uninstall" = 1 ]; then
	if [ -e "$target" ]; then
		rm -f "$target" || die "could not remove $target"
		say "removed $target"
		say "(notes are untouched; settings, if any, are in ~/.config/folio and ~/.local/state/folio)"
	else
		say "folio is not installed in $bin_dir"
	fi
	exit 0
fi

# --- platform ------------------------------------------------------------------

case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) die "unsupported system: $(uname -s) (folio supports Linux and macOS)" ;;
esac

case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64 | armv8*) arch=arm64 ;;
*) die "unsupported architecture: $(uname -m) (folio supports x86_64 and arm64)" ;;
esac

# A shell running under Rosetta reports x86_64 on Apple silicon: prefer the
# native binary.
if [ "$os" = darwin ] && [ "$arch" = amd64 ]; then
	if [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
		arch=arm64
	fi
fi

asset=folio-$os-$arch

# --- helpers -------------------------------------------------------------------

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t folio) || die "cannot create a temporary directory"
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM

# download URL FILE: fetch with curl or wget; returns non-zero on failure.
download() {
	if have curl; then
		curl -fsSL --retry 3 --connect-timeout 15 -o "$2" "$1"
	elif have wget; then
		wget -q -T 30 -t 3 -O "$2" "$1"
	else
		die "neither curl nor wget is available; fetch $1 with a browser, then run: sh install.sh --from ./$asset"
	fi
}

# sha256 FILE: print the file's SHA-256, or nothing if no tool exists.
sha256() {
	if have sha256sum; then
		sha256sum "$1" | cut -d' ' -f1
	elif have shasum; then
		shasum -a 256 "$1" | cut -d' ' -f1
	elif have openssl; then
		openssl dgst -sha256 "$1" | sed 's/^.*= *//'
	fi
}

# verify FILE CHECKSUMS NAME: check FILE against NAME's line in CHECKSUMS.
verify() {
	want=$(awk -v n="$3" '{ f = $2; sub(/^\*/, "", f); if (f == n) { print $1; exit } }' "$2")
	if [ -z "$want" ]; then
		warn "checksums.txt has no entry for $3; not verified"
		return 0
	fi
	got=$(sha256 "$1")
	if [ -z "$got" ]; then
		warn "no sha256sum, shasum or openssl found; checksum not verified"
		return 0
	fi
	[ "$got" = "$want" ] || die "checksum mismatch for $3: expected $want, got $got (download corrupted or tampered with)"
	say "checksum ok ($3)"
}

# --- fetch ---------------------------------------------------------------------

bin=$tmp/folio
if [ -n "$from" ]; then
	[ -f "$from" ] || die "no such file: $from"
	cp "$from" "$bin" || die "cannot read $from"
	sums=$(dirname "$from")/checksums.txt
	if [ -f "$sums" ]; then
		verify "$bin" "$sums" "$(basename "$from")"
	fi
	source_desc=$from
else
	if [ -n "${FOLIO_BASE_URL:-}" ]; then
		base=${FOLIO_BASE_URL%/}
	elif [ -n "$version" ]; then
		case $version in
		v*) ;;
		*) version=v$version ;;
		esac
		base=https://github.com/$REPO/releases/download/$version
	else
		base=https://github.com/$REPO/releases/latest/download
	fi
	say "downloading $base/$asset"
	download "$base/$asset" "$bin" ||
		die "download failed: $base/$asset
  (no network? fetch that file with a browser, then: sh install.sh --from ./$asset)"
	if download "$base/checksums.txt" "$tmp/checksums.txt"; then
		verify "$bin" "$tmp/checksums.txt" "$asset"
	else
		warn "could not download checksums.txt; checksum not verified"
	fi
	source_desc=$base/$asset
fi

chmod 0755 "$bin"
# </dev/null: under `curl … | sh` stdin is the rest of this script.
new_version=$("$bin" version </dev/null 2>/dev/null) ||
	die "$source_desc does not run on this machine ($os/$arch); is it the right file?"

# --- install -------------------------------------------------------------------

old_version=""
if [ -x "$target" ]; then
	old_version=$("$target" version </dev/null 2>/dev/null || true)
fi

mkdir -p "$bin_dir" || die "cannot create $bin_dir (choose another with FOLIO_BIN_DIR=...)"
# Copy next to the target, then rename: atomic, and safe while folio runs.
staged=$bin_dir/.folio.$$
cp "$bin" "$staged" || die "cannot write to $bin_dir"
chmod 0755 "$staged"
mv -f "$staged" "$target" || {
	rm -f "$staged"
	die "cannot install $target"
}

if [ -z "$old_version" ]; then
	say "installed $target"
elif [ "$old_version" = "$new_version" ]; then
	say "reinstalled $target"
else
	say "upgraded $target"
	say "  was: $old_version"
fi
say "  $new_version"

# --- PATH hint -----------------------------------------------------------------

case ":${PATH:-}:" in
*":$bin_dir:"* | *":$bin_dir/:"*) ;;
*)
	say ""
	say "$bin_dir is not on your PATH. Add it with:"
	shell=${SHELL:-sh}
	case ${shell##*/} in
	zsh) say "  echo 'export PATH=\"$bin_dir:\$PATH\"' >> ~/.zshrc && . ~/.zshrc" ;;
	bash) say "  echo 'export PATH=\"$bin_dir:\$PATH\"' >> ~/.bashrc && . ~/.bashrc" ;;
	fish) say "  fish_add_path $bin_dir" ;;
	*) say "  echo 'export PATH=\"$bin_dir:\$PATH\"' >> ~/.profile   # then log in again" ;;
	esac
	;;
esac

say ""
say "Try: folio help"
}

main "$@"
