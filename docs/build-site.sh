#!/bin/sh
# Builds the GitHub Pages site into OUT (default .scratch/site): the landing page from
# docs/site/ at the root, the captures under shots/, the banner under media/, and the manual
# (mdBook) under manual/, which gets its own copy of the captures its chapters show.
# The same script runs locally and in .github/workflows/pages.yml.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
out=${1:-$here/../.scratch/site}
case $out in /*) ;; *) out=$PWD/$out ;; esac
rm -rf "$out"
mkdir -p "$out/shots" "$out/media"
cp -R "$here/site/." "$out/"
cp -R "$here/shots/." "$out/shots/"
cp -R "$here/media/." "$out/media/"
mdbook build "$here/manual" -d "$out/manual"
mkdir -p "$out/manual/shots"
cp "$here"/shots/*.svg "$here/shots/kitty-hero.png" "$here/shots/kitty-arabic.png" "$out/manual/shots/"
echo "site built in $out"
