#!/usr/bin/env python3
"""Aggregate BenchmarkHook (per-case) output into a per-plugin table: for
each engine, the mean over the plugin's cases of the per-case median ns/op
and allocs/op, plus the slowest case per plugin on moejs.

usage: hookcases.py results/hook_cases.txt [results/hook_cases_cgo.txt ...]
"""
import re
import statistics
import sys
from collections import defaultdict

per = defaultdict(lambda: defaultdict(list))  # (engine, plugin, case) -> unit -> values
for path in sys.argv[1:]:
    for line in open(path):
        if not line.startswith("BenchmarkHook/"):
            continue
        parts = line.split()
        name = re.sub(r"-\d+$", "", parts[0])[len("BenchmarkHook/"):]
        engine, plugin, case = name.split("/", 2)
        i = 2
        while i + 1 < len(parts):
            try:
                v = float(parts[i])
            except ValueError:
                i += 1
                continue
            per[(engine, plugin, case)][parts[i + 1]].append(v)
            i += 2

engines, plugins = [], []
for e, p, _ in per:
    if e not in engines:
        engines.append(e)
    if p not in plugins:
        plugins.append(p)
plugins.sort()

def agg(engine, plugin):
    ns, al, cases = [], [], 0
    for (e, p, c), m in per.items():
        if e != engine or p != plugin or "ns/op" not in m:
            continue
        ns.append(statistics.median(m["ns/op"]))
        al.append(statistics.median(m["allocs/op"]))
        cases += 1
    if not cases:
        return None
    return statistics.mean(ns), statistics.mean(al), cases

print("| plugin | cases | " + " | ".join(f"{e} µs / allocs" for e in engines) + " |")
print("|---|---|" + "---|" * len(engines))
for p in plugins:
    cells, n = [], 0
    for e in engines:
        a = agg(e, p)
        if a is None:
            cells.append("")
            continue
        cells.append(f"{a[0]/1000:.2f} / {a[1]:.0f}")
        n = max(n, a[2])
    print(f"| {p} | {n} | " + " | ".join(cells) + " |")

ref = "moejs" if "moejs" in engines else engines[0]
print(f"\nSlowest cases on {ref} (median ns/op):\n")
print("| plugin / case | " + " | ".join(f"{e} µs" for e in engines) + " |")
print("|---|" + "---|" * len(engines))
rows = []
for (e, p, c), m in per.items():
    if e == ref and "ns/op" in m:
        rows.append((statistics.median(m["ns/op"]), p, c))
rows.sort(reverse=True)
for ns, p, c in rows[:12]:
    cells = []
    for e in engines:
        m = per.get((e, p, c))
        cells.append(f"{statistics.median(m['ns/op'])/1000:.1f}" if m and "ns/op" in m else "")
    print(f"| {p} / {c} | " + " | ".join(cells) + " |")
