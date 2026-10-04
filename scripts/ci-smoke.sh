#!/bin/sh
# Smoke test of a built astrolabe binary and install.sh, run inside a container
# as a non-root user (see .github/workflows/ci.yml):
#
#   docker run --rm --user 1000:1000 -e HOME=/tmp/home -v "$PWD:/src:ro" \
#     centos:7 sh /src/scripts/ci-smoke.sh
#
# Expects /src to hold the repository with dist/astrolabe-linux-amd64 and
# dist/checksums.txt (from scripts/build.sh). Verbs whose implementation is
# not linked in yet ("not built into this binary yet") are reported as SKIP;
# every other failure fails the test.
# shellcheck disable=SC2015,SC2016  # "check && pass || fail" is intended
set -eu

src=${SRC:-/src}
asset=${ASSET:-astrolabe-linux-amd64}
fails=0

pass() { printf 'ok    %s\n' "$*"; }
skip() { printf 'SKIP  %s\n' "$*"; }
fail() {
	printf 'FAIL  %s\n' "$*"
	fails=$((fails + 1))
}

# check NAME WANT_EXIT CMD...: run CMD, compare its exit status.
check() {
	name=$1
	want=$2
	shift 2
	set +e
	"$@" >"$HOME/out" 2>"$HOME/err"
	got=$?
	set -e
	if [ "$got" = "$want" ]; then
		pass "$name"
	elif grep -q "not built into this binary yet" "$HOME/err"; then
		skip "$name (not built yet)"
	else
		fail "$name: exit $got, want $want"
		sed 's/^/      /' "$HOME/out" "$HOME/err" | head -20
	fi
}

printf 'system: %s, user %s\n' "$(sed -n 's/^PRETTY_NAME=//p' /etc/os-release 2>/dev/null | tr -d '"')" "$(id -u)"
[ "$(id -u)" != 0 ] || {
	echo "must run as a non-root user"
	exit 1
}
mkdir -p "$HOME"
unset ASTROLABE_DIR || true
export LANG=C.UTF-8 TERM=xterm-256color

# The one-liner from the README, against a mirror of the release assets
# (ASTROLABE_SMOKE_URL, served by the workflow): curl where it exists, else
# wget, else the installer must explain the offline route.
if [ -n "${ASTROLABE_SMOKE_URL:-}" ]; then
	set +e
	if command -v curl >/dev/null 2>&1; then
		how="curl | sh"
		curl -fsSL "$ASTROLABE_SMOKE_URL/install.sh" | ASTROLABE_BASE_URL=$ASTROLABE_SMOKE_URL sh >"$HOME/out" 2>&1
	elif command -v wget >/dev/null 2>&1; then
		how="wget | sh"
		wget -qO- "$ASTROLABE_SMOKE_URL/install.sh" | ASTROLABE_BASE_URL=$ASTROLABE_SMOKE_URL sh >"$HOME/out" 2>&1
	else
		how="no downloader"
		! ASTROLABE_BASE_URL=$ASTROLABE_SMOKE_URL sh "$src/install.sh" >"$HOME/out" 2>&1 &&
			grep -q -- "--from" "$HOME/out"
	fi
	got=$?
	set -e
	if [ "$got" = 0 ]; then pass "install ($how)"; else
		fail "install ($how)"
		sed 's/^/      /' "$HOME/out"
	fi
	if [ "$how" != "no downloader" ]; then
		grep -q "checksum ok" "$HOME/out" && pass "download checksum verified" || fail "download checksum not verified"
		"$HOME/.local/bin/astrolabe" version >/dev/null && pass "downloaded binary runs" || fail "downloaded binary does not run"
	fi
fi

# install.sh: offline install verified against checksums.txt, idempotent
# re-run, uninstall, and install again.
check "install.sh --from" 0 sh "$src/install.sh" --from "$src/dist/$asset"
grep -q "checksum ok" "$HOME/out" && pass "checksum verified" || fail "checksum not verified"
check "install.sh again (upgrade in place)" 0 sh "$src/install.sh" --from "$src/dist/$asset"
check "install.sh --uninstall" 0 sh "$src/install.sh" --uninstall
[ ! -e "$HOME/.local/bin/astrolabe" ] && pass "uninstalled" || fail "binary still present"
check "install.sh custom dir" 0 env ASTROLABE_BIN_DIR="$HOME/bin" sh "$src/install.sh" --from "$src/dist/$asset"
grep -q "not on your PATH" "$HOME/out" && pass "PATH hint" || fail "no PATH hint"
export PATH="$HOME/bin:$PATH"

check "astrolabe version" 0 astrolabe version
cat "$HOME/out"

# A writable copy of the example vault.
cp -R "$src/examples/vault" "$HOME/vault"
cd "$HOME/vault"

check "astrolabe add" 0 astrolabe add "smoke test capture from $(uname -s)"
check "astrolabe add -t (stdin)" 0 sh -c 'echo "a task from stdin" | astrolabe add -t'
check "astrolabe find" 0 astrolabe find "smoke test capture"
grep -q '^daily/.*:[0-9]*:- [0-9][0-9]:[0-9][0-9] smoke test capture' "$HOME/out" && pass "find output form" || fail "find output form"
check "astrolabe find (no match exits 1)" 1 astrolabe find "zzz-no-such-text"
check "astrolabe ls" 0 astrolabe ls
check "astrolabe ls --tag" 0 astrolabe ls --tag project
check "astrolabe tasks" 0 astrolabe tasks
check "astrolabe tasks --json" 0 astrolabe tasks --json
check "astrolabe tags" 0 astrolabe tags
check "astrolabe links" 0 astrolabe links "Lantern migration"
check "astrolabe backlinks" 0 astrolabe backlinks Backpressure
check "astrolabe path" 0 astrolabe path Astrolabe
check "astrolabe today -p" 0 astrolabe today -p
check "astrolabe new" 0 sh -c 'echo body | astrolabe new "Smoke note"'
check "astrolabe doctor" 0 astrolabe doctor
check "astrolabe help find" 0 astrolabe help find
check "unknown verb-like note (exit 1)" 1 astrolabe fnd
check "bad flag (exit 2)" 2 astrolabe ls --bogus
check "astrolabe -C" 0 sh -c 'cd / && astrolabe -C "$HOME/vault" ls -l'

for note in "Lantern migration" "Lantern cutover" "Celestial navigation" "الأسطرلاب"; do
	check "astrolabe render $note" 0 astrolabe render -w 80 "$note"
done
check "astrolabe export" 0 astrolabe export Home -o "$HOME/home.html"

# The reader under a pseudo-terminal, quitting with q.
if command -v script >/dev/null 2>&1 && command -v timeout >/dev/null 2>&1; then
	set +e
	# Keep stdin open for a while: some versions of script(1) stop
	# forwarding the child's output as soon as their stdin reaches EOF.
	(
		sleep 2
		printf 'jjq'
		sleep 3
	) | timeout 20 script -qec "astrolabe Home" /dev/null >"$HOME/tui.out" 2>&1
	code=$?
	set -e
	if grep -q "not built into this binary yet" "$HOME/tui.out"; then
		skip "TUI under script (not built yet)"
	elif [ "$code" = 0 ] && grep -q "front door of this vault" "$HOME/tui.out"; then
		pass "TUI under script"
	else
		fail "TUI under script: exit $code (or the page was not drawn)"
		head -c 2000 "$HOME/tui.out" | cat -v
	fi
else
	skip "TUI smoke (no script/timeout in this image)"
fi

if [ "$fails" -gt 0 ]; then
	printf '\n%d check(s) failed\n' "$fails"
	exit 1
fi
printf '\nall checks passed\n'
