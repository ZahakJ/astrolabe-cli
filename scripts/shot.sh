#!/bin/sh
# shot.sh — capture astrolabe (or any command) in a private tmux server as text,
# SVG and PNG.
#
#   scripts/shot.sh [options] NAME [STEP...]
#
# Starts the command in a detached pane of a private tmux server
# (`tmux -L astrolabe-shot`, never your own), runs the scripted STEPs, then saves
#   OUT/NAME.txt   the pane as plain text
#   OUT/NAME.ansi  the pane with SGR attributes (capture-pane -e)
#   OUT/NAME.svg   a cell-exact SVG (scripts/shot), framed like a window
#   OUT/NAME.png   the SVG rasterised by rsvg-convert, if it is installed
#
# The command runs with a clean environment: HOME, XDG_CONFIG_HOME and
# XDG_STATE_HOME point into a throwaway directory, so no personal config,
# recent-notes list or shell prompt leaks into a capture. LANG is C.UTF-8,
# COLORTERM=truecolor and TERM is tmux's tmux-256color unless -e overrides.
#
# Options
#   -s COLSxROWS  pane size (default 110x32)
#   -e VAR=VALUE  environment for the command (repeatable; VAR= unsets)
#   -c COMMAND    command line to run (default: astrolabe -C examples/vault);
#                 the word "astrolabe" may be used, it is the freshly built binary
#   -d DIR        working directory for the command (default: repo root)
#   -o DIR        output directory (default: docs/shots)
#   -t TITLE      window title in the SVG frame (default: astrolabe)
#   -w SECONDS    wait before the first step (default: 0.8)
#   -C            draw the terminal cursor in the SVG
#   -l            light default terminal colours (for TERM=xterm captures)
#   -k            keep the tmux server afterwards (attach: tmux -L astrolabe-shot a)
#   -n            no PNG
#
# Steps (run in order, 0.15 s apart)
#   NAME...        tmux key names: Enter Escape Space Tab BTab Up C-p M-x j …
#   type:TEXT      literal text
#   paste:TEXT     bracketed paste ("\n" in TEXT becomes a newline)
#   sleep:SECONDS  pause
#   wait:TEXT      wait up to 5 s for TEXT to appear in the pane
#   resize:CxR     resize the pane
#   snap:SUFFIX    write an intermediate capture NAME-SUFFIX.{txt,ansi,svg,png}
#   sh:COMMAND     run a host shell command (e.g. to modify a file meanwhile)
#
# Environment: ASTROLABE_BIN uses an existing binary instead of building one;
# SHOT_SOCKET overrides the tmux socket name; TMPDIR is honoured.
#
# Example:
#   scripts/shot.sh -s 110x32 finder C-p type:runbook sleep:0.3
set -eu

repo=$(cd "$(dirname "$0")/.." && pwd)
size=110x32
out="$repo/docs/shots"
title=astrolabe
wait=0.8
dir=$repo
cmd=
keep=0
light=
cursor=0
png=1
envs=
socket=${SHOT_SOCKET:-astrolabe-shot}

usage() { sed -n '2,46p' "$0" | sed 's/^# \{0,1\}//'; exit "${1:-2}"; }

while getopts s:e:c:d:o:t:w:Clknh opt; do
	case $opt in
	s) size=$OPTARG ;;
	e) envs="$envs
$OPTARG" ;;
	c) cmd=$OPTARG ;;
	d) dir=$OPTARG ;;
	o) out=$OPTARG ;;
	t) title=$OPTARG ;;
	w) wait=$OPTARG ;;
	C) cursor=1 ;;
	l) light=-light ;;
	k) keep=1 ;;
	n) png=0 ;;
	h) usage 0 ;;
	*) usage ;;
	esac
done
shift $((OPTIND - 1))
[ $# -ge 1 ] || usage
name=$1
shift
case $name in -*) usage ;; esac
for step in "$@"; do
	case $step in -?*)
		echo "shot: options go before NAME (got step '$step')" >&2
		exit 2 ;;
	esac
done

cols=${size%x*}
rows=${size#*x}
case $cols$rows in *[!0-9]*|'') echo "shot: bad size $size" >&2; exit 2 ;; esac

command -v tmux >/dev/null || { echo "shot: tmux is required" >&2; exit 1; }
work=$(mktemp -d "${TMPDIR:-/tmp}/astrolabe-shot.XXXXXX")
mkdir -p "$out" "$work/home" "$work/config" "$work/state"

T() { tmux -L "$socket" -f /dev/null "$@"; }

cleanup() {
	if [ "$keep" = 0 ]; then
		T kill-server 2>/dev/null || true
	fi
	# astrolabe may still be writing its state on the way out (SIGHUP saves),
	# so retry briefly instead of failing on a directory that just filled.
	i=0
	until rm -rf "$work" 2>/dev/null || [ $i -ge 20 ]; do
		i=$((i + 1))
		sleep 0.1
	done
}
trap cleanup EXIT INT TERM

if [ -n "${ASTROLABE_BIN:-}" ]; then
	bin=$ASTROLABE_BIN
else
	bin=$work/astrolabe
	(cd "$repo" && CGO_ENABLED=0 go build -o "$bin" ./cmd/astrolabe)
fi
svgtool=$work/shotsvg
(cd "$repo" && go build -o "$svgtool" ./scripts/shot)
mkdir -p "$work/bin"
ln -s "$bin" "$work/bin/astrolabe"

[ -n "$cmd" ] || cmd="astrolabe -C '$repo/examples/vault'"

# The pane's environment: clean, then -e overrides (VAR= unsets).
envfile=$work/env
: >"$envfile"
printf '%s\n' "PATH=$work/bin:/usr/local/bin:/usr/bin:/bin" "HOME=$work/home" \
	"XDG_CONFIG_HOME=$work/config" "XDG_STATE_HOME=$work/state" \
	"LANG=C.UTF-8" "COLORTERM=truecolor" "PS1=\$ " >"$envfile"
printf '%s\n' "$envs" | while IFS= read -r kv; do
	[ -n "$kv" ] || continue
	k=${kv%%=*}
	grep -v "^$k=" "$envfile" >"$envfile.tmp" || true
	mv "$envfile.tmp" "$envfile"
	case $kv in *=) printf '%s\n' "$k" >>"$envfile.unset" ;; *) printf '%s\n' "$kv" >>"$envfile" ;; esac
done
touch "$envfile.unset"

# The pane runs a tiny launcher so the environment and directory are exact
# and the pane stays open (for the capture) after the command exits.
launcher=$work/run.sh
{
	echo '#!/bin/sh'
	while IFS= read -r kv; do
		k=${kv%%=*}
		v=${kv#*=}
		printf "export %s='%s'\n" "$k" "$(printf '%s' "$v" | sed "s/'/'\\\\''/g")"
	done <"$envfile"
	grep -q '^TERM=' "$envfile" || echo 'export TERM=tmux-256color'
	# tmux itself exports COLORTERM=truecolor into panes; honour VAR= unsets.
	while IFS= read -r k; do printf 'unset %s\n' "$k"; done <"$envfile.unset"
	printf "cd '%s' || exit 1\n" "$dir"
	printf '%s\n' "$cmd"
	echo 'exec sleep 3600'
} >"$launcher"
chmod +x "$launcher"

T kill-server 2>/dev/null || true
env -i PATH=/usr/local/bin:/usr/bin:/bin HOME="$work/home" TMPDIR="${TMPDIR:-/tmp}" \
	tmux -L "$socket" -f /dev/null new-session -d -s shot -x "$cols" -y "$rows" "$launcher"
T set -g status off
T set -g window-size manual
T set -g escape-time 0
T set -g history-limit 0
T resize-window -t shot -x "$cols" -y "$rows" 2>/dev/null || true
sleep "$wait"

capture() { # capture NAME
	T capture-pane -p -t shot >"$out/$1.txt"
	T capture-pane -p -e -N -t shot >"$out/$1.ansi"
	cur=
	if [ "$cursor" = 1 ]; then
		cur=$(T display -p -t shot '#{cursor_x} #{cursor_y} #{cursor_shape} #{cursor_flag}')
		case $cur in *" 0") cur= ;; esac
	fi
	pc=$(T display -p -t shot '#{pane_width}')
	pr=$(T display -p -t shot '#{pane_height}')
	"$svgtool" -in "$out/$1.ansi" -cols "$pc" -rows "$pr" -title "$title" $light \
		${cur:+-cursor "$cur"} -svg "$out/$1.svg"
	if [ "$png" = 1 ] && command -v rsvg-convert >/dev/null; then
		rsvg-convert -z 1.5 "$out/$1.svg" -o "$out/$1.png"
	fi
}

for step in "$@"; do
	case $step in
	type:*) T send-keys -t shot -l -- "${step#type:}" ;;
	paste:*)
		T set-buffer -b shot -- "$(printf '%b' "${step#paste:}")"
		T paste-buffer -p -d -b shot -t shot
		;;
	sleep:*) sleep "${step#sleep:}" ;;
	wait:*)
		i=0
		while ! T capture-pane -p -t shot | grep -qF -- "${step#wait:}"; do
			i=$((i + 1))
			[ $i -lt 50 ] || { echo "shot: timed out waiting for '${step#wait:}'" >&2; break; }
			sleep 0.1
		done
		;;
	resize:*)
		r=${step#resize:}
		T resize-window -t shot -x "${r%x*}" -y "${r#*x}"
		;;
	snap:*) sleep 0.3; capture "$name-${step#snap:}" ;;
	sh:*) (cd "$dir" && sh -c "${step#sh:}") ;;
	*) T send-keys -t shot -- "$step" ;;
	esac
	sleep 0.15
done
sleep 0.4
capture "$name"
echo "$out/$name.svg"
