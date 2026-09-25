#!/usr/bin/env bash
# Shallow-clones the tc39/test262 commit pinned in REVISION into
# ${MOEJS_TEST262_DIR:-$HOME/.cache/moejs/test262}. Re-running
# it after REVISION changes moves the checkout to the new commit.
#
# Usage: bench/test262/fetch.sh
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REV="$(awk 'NR == 1 { print $1 }' "$HERE/REVISION")"
DIR="${MOEJS_TEST262_DIR:-$HOME/.cache/moejs/test262}"

if [ -d "$DIR/.git" ] && [ "$(git -C "$DIR" rev-parse HEAD 2>/dev/null)" = "$REV" ]; then
	echo "test262 $REV already checked out in $DIR"
	exit 0
fi

mkdir -p "$DIR"
git -C "$DIR" init -q
git -C "$DIR" remote remove origin 2>/dev/null || true
git -C "$DIR" remote add origin https://github.com/tc39/test262.git
git -C "$DIR" fetch -q --depth 1 origin "$REV"
git -C "$DIR" checkout -q --detach FETCH_HEAD
echo "test262 $REV checked out in $DIR"
