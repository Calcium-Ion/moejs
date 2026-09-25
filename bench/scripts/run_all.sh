#!/usr/bin/env bash
# Runs the bench measurements (benchmarks, the footprint table, an allocation
# profile and the PGO comparison) and writes the raw output to bench/results/
# (git-ignored). Sequential on purpose: concurrent benchmark processes would
# distort each other.
set -euo pipefail
cd "$(dirname "$0")/.."
testdata/plugins/fetch.sh
mkdir -p results
export GOMAXPROCS="${GOMAXPROCS:-18}"
go version | tee results/env.txt
sysctl -n machdep.cpu.brand_string 2>/dev/null | tee -a results/env.txt || true
echo "GOMAXPROCS=$GOMAXPROCS" | tee -a results/env.txt
date -u +%Y-%m-%dT%H:%M:%SZ | tee -a results/env.txt

run() { local name=$1; shift; echo "== $name"; "$@" 2>&1 | tee "results/$name.txt" | grep -E 'Benchmark|^(ok|FAIL|---)' || true; }

run newruntime  go test -run xxx -bench 'BenchmarkNewRuntime' -benchmem -count 5 .
run compile     go test -run xxx -bench 'BenchmarkCompile' -benchmem -count 5 .
run instantiate go test -run xxx -bench 'BenchmarkInstantiate' -benchmem -count 5 .
run hooksuite   go test -run xxx -bench 'BenchmarkHookSuite$' -benchmem -count 5 .
run parallel    go test -run xxx -bench 'BenchmarkHookSuiteParallel' -benchmem -count 3 -cpu 1,4,18 .
run parallel_v8 env V8_PARALLEL=1 go test -run xxx -bench 'BenchmarkHookSuiteParallel/v8go' -benchmem -count 3 -cpu 1,4,18 .
run micro       go test -run xxx -bench 'BenchmarkMicro' -benchmem -count 5 .
run phases      go test -run xxx -bench 'BenchmarkHookPhases' -benchmem -count 3 -benchtime 300ms .
run hook_cases  go test -run xxx -bench 'BenchmarkHook$' -benchmem -count 3 -benchtime 100ms .
run footprint   go test -run TestFootprint -v .

echo "== memprofile (allocation attribution, moejs hook suite)"
go test -run xxx -bench 'BenchmarkHookSuite$/moejs$' -benchmem -memprofile results/moejs.mem.prof -memprofilerate 1 -o results/bench.test . | tee results/memprofile.txt
go tool pprof -sample_index=alloc_objects -top -nodecount=25 results/bench.test results/moejs.mem.prof | tee results/alloc_objects_top.txt
go tool pprof -sample_index=alloc_space -top -nodecount=25 results/bench.test results/moejs.mem.prof | tee results/alloc_space_top.txt

echo "== pgo"
go run ./cmd/pgo -duration 30s | tee results/pgo_profile.txt
run hooksuite_pgo_off go test -run xxx -bench 'BenchmarkHookSuite$/moejs' -benchmem -count 5 -pgo=off .
run hooksuite_pgo_on  go test -run xxx -bench 'BenchmarkHookSuite$/moejs' -benchmem -count 5 -pgo=../default.pgo .
echo "== done"
