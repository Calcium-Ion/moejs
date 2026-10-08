#!/bin/sh
# gatebuild.sh <tree> <outdir> <name>: the test binaries of the plugin-
# workload gate (docs/DESIGN.md §14.2) for the checkout <tree>, as
# <outdir>/{engine,root,bench}-<name>.test, for gateab.sh.
#
# They link with -funcalign=64. By default amd64 aligns functions to 32
# bytes, so a change anywhere before a hot function moves it by 32 within
# its 64-byte line, and that alone moves some rows by up to 10 % (docs/
# NOTES.md "LoopSum and the alignment of (*Realm).run"). With every
# function on a 64-byte boundary a function's placement depends on its own
# code only. The flag is the linker's, so any commit builds this way.
set -e
T=$1 O=$2 N=$3
mkdir -p "$O"
O=$(cd "$O" && pwd)
cd "$T"
go test -c -ldflags=-funcalign=64 -o "$O/engine-$N.test" ./engine
go test -c -ldflags=-funcalign=64 -o "$O/root-$N.test" .
cd bench && go test -c -ldflags=-funcalign=64 -o "$O/bench-$N.test" .
