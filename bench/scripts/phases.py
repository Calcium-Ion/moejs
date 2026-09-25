#!/usr/bin/env python3
"""Aggregate BenchmarkHookPhases output: per engine, per hook family, the
median-summed cost of the fromgo / call / togo phases and the measured total.

usage: phases.py results/phases.txt
"""
import re
import statistics
import sys
from collections import defaultdict

FAMILIES = {"decode": "responses_decode_", "build": "build_submit_", "parse": "parse_task_"}

def family(case):
    if "rejects" in case or "missing" in case:
        return "throw"
    for name, prefix in FAMILIES.items():
        if case.startswith(prefix):
            return name
    return "other"

samples = defaultdict(lambda: defaultdict(list))  # (engine, family, phase) -> metric -> values
cases = defaultdict(set)
for line in open(sys.argv[1]):
    if not line.startswith("BenchmarkHookPhases/"):
        continue
    parts = line.split()
    name = re.sub(r"-\d+$", "", parts[0])[len("BenchmarkHookPhases/"):]
    engine, plugin, case, phase = name.split("/")
    key = (engine, family(case), phase)
    cases[(engine, family(case))].add(plugin + "/" + case)
    i = 2
    while i + 1 < len(parts):
        try:
            v = float(parts[i])
        except ValueError:
            i += 1
            continue
        samples[key][parts[i + 1]].append(v)
        i += 2

# per (engine, family, phase): median per case, then mean across cases
per_case = defaultdict(lambda: defaultdict(lambda: defaultdict(list)))
for line in open(sys.argv[1]):
    if not line.startswith("BenchmarkHookPhases/"):
        continue
    parts = line.split()
    name = re.sub(r"-\d+$", "", parts[0])[len("BenchmarkHookPhases/"):]
    engine, plugin, case, phase = name.split("/")
    i = 2
    while i + 1 < len(parts):
        try:
            v = float(parts[i])
        except ValueError:
            i += 1
            continue
        per_case[(engine, family(case), phase)][plugin + "/" + case][parts[i + 1]].append(v)
        i += 2

engines = []
for (e, f, p) in per_case:
    if e not in engines:
        engines.append(e)
print("| engine | family | cases | fromgo | call | togo | sum | total (measured) | allocs fromgo / call / togo / total |")
print("|---|---|---|---|---|---|---|---|---|")
for e in engines:
    for f in ["decode", "build", "parse", "throw"]:
        row = {}
        for p in ["fromgo", "call", "togo", "total"]:
            d = per_case.get((e, f, p))
            if not d:
                row[p] = (0.0, 0.0)
                continue
            ns = statistics.mean(statistics.median(m["ns/op"]) for m in d.values())
            al = statistics.mean(statistics.median(m["allocs/op"]) for m in d.values())
            row[p] = (ns, al)
        n = len(per_case.get((e, f, "total"), {}))
        if n == 0:
            continue
        s = row["fromgo"][0] + row["call"][0] + row["togo"][0]
        us = lambda v: f"{v/1000:.2f}"
        print(f"| {e} | {f} | {n} | {us(row['fromgo'][0])} | {us(row['call'][0])} | {us(row['togo'][0])} | {us(s)} | {us(row['total'][0])} | {row['fromgo'][1]:.0f} / {row['call'][1]:.0f} / {row['togo'][1]:.0f} / {row['total'][1]:.0f} |")
