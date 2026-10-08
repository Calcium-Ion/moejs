#!/usr/bin/env bash
# Shallow-clones the commits pinned in REVISIONS into
# ${MOEJS_TS_CORPUS:-$HOME/.cache/moejs/ts-corpus}/<name>, the TypeScript
# corpus of TestTSDifferential and TestPiExtensions. Re-running it after
# REVISIONS changes moves each checkout to the new commit.
#
# Usage: bench/tscorpus/fetch.sh
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="${MOEJS_TS_CORPUS:-$HOME/.cache/moejs/ts-corpus}"

grep -v '^#' "$HERE/REVISIONS" | while read -r name rev url sub; do
	dir="$ROOT/$name"
	if [ -d "$dir/.git" ] && [ "$(git -C "$dir" rev-parse HEAD 2>/dev/null)" = "$rev" ]; then
		echo "$name $rev already checked out in $dir"
		continue
	fi
	mkdir -p "$dir"
	git -C "$dir" init -q
	git -C "$dir" remote remove origin 2>/dev/null || true
	git -C "$dir" remote add origin "$url"
	if [ -n "${sub:-}" ]; then
		git -C "$dir" sparse-checkout set "$sub"
	fi
	git -C "$dir" fetch -q --depth 1 origin "$rev"
	git -C "$dir" checkout -q --detach FETCH_HEAD
	echo "$name $rev checked out in $dir"
done
