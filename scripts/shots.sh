#!/bin/sh
# shots.sh — regenerate the README captures in docs/shots from examples/vault.
#
#   scripts/shots.sh [NAME...]     (default: all of them)
#
# Each capture runs folio from a fresh build in a private tmux server through
# scripts/shot.sh, with a clean HOME and state directory, so nothing but the
# sample vault can appear. Only .svg and .png files are kept. The dates in the
# sample vault are relative to early October 2026, so agenda labels such as
# "overdue" and "tomorrow" depend on the day the captures are taken.
set -eu

repo=$(cd "$(dirname "$0")/.." && pwd)
vault=$repo/examples/vault
out=$repo/docs/shots
work=$(mktemp -d "${TMPDIR:-/tmp}/folio-shots.XXXXXX")
trap 'rm -rf "$work"' EXIT INT TERM
(cd "$repo" && CGO_ENABLED=0 go build -o "$work/folio" ./cmd/folio)
export FOLIO_BIN="$work/folio"
mkdir -p "$out" "$work/out"

ARGS="$*"
want() {
	[ -z "$ARGS" ] && return 0
	for w in $ARGS; do [ "$w" = "$1" ] && return 0; done
	return 1
}

# shot [shot.sh options] NAME [steps]: options come first, as for shot.sh.
shot() {
	"$repo/scripts/shot.sh" -o "$work/out" "$@" >/dev/null
	for a; do
		case $a in -*) ;; *)
			if [ -f "$work/out/$a.svg" ]; then
				mv "$work/out/$a.svg" "$out/"
				[ ! -f "$work/out/$a.png" ] || mv "$work/out/$a.png" "$out/"
				echo "docs/shots/$a.svg"
				return 0
			fi ;;
		esac
	done
}

open() { echo "folio -C '$vault' $*"; }

want reader && shot -c "$(open "'Lantern cutover'")" reader j j j j
want reader-links && shot -c "$(open Backpressure)" reader-links Space b
# The editor capture ends with unsaved edits; the tmux server is then killed,
# and folio keeps a dirty buffer on SIGHUP (it saves it), so it edits a copy.
if want editor; then
	cp -Rp "$vault" "$work/vault-editor"
	shot -C -c "folio -C '$work/vault-editor' 'Lantern migration'" editor \
		/ "type:that says" Enter a Enter \
		"type:Dead letters need a home: see [[" sleep:0.4 "type:idem" sleep:0.4
fi
want finder && shot -c "$(open Home)" finder C-p type:lant sleep:0.4
want search && shot -c "$(open Home)" search Space / type:retry sleep:0.8
want agenda && shot -c "$(open Home)" agenda Space t sleep:0.4
want arabic && shot -c "$(open Astrolabe)" arabic j j j
want narrow && shot -s 60x18 -c "$(open "'Lantern cutover'")" narrow j j j j j
want plain16 && shot -e COLORTERM= -e TERM=xterm -c "$(open "'Lantern cutover'")" plain16 j j j
want parchment && shot -c "$(open --theme parchment Typography)" parchment j j j j j
want mocha && shot -c "$(open --theme mocha Backpressure)" mocha C-d

# The shell session writes (folio add), so it runs on a copy of the vault too.
if want capture; then
	cp -Rp "$vault" "$work/vault"
	shot -d "$work/vault" -c sh -t sh capture \
		"type:folio add Ask the vendor about the Kestrel support contract" Enter \
		"type:folio add -t Review the Lantern dashboards" Enter \
		"type:folio find kestrel disk" Enter \
		"type:folio ls -n 4" Enter \
		"type:folio tasks --due | head -4" Enter sleep:0.4
fi
