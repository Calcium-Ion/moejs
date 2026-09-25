#!/usr/bin/env python3
"""Summarize `go test -bench` output as median-per-metric Markdown tables.

usage: summarize.py [-k regex] [-strip prefix] file...

Every benchmark line ("BenchmarkX-18  N  v unit  v unit ...") contributes one
sample per metric; the table reports the median over the -count runs and the
relative spread (max-min)/median of ns/op so variance above 10% is visible.
"""
import re
import statistics
import sys

def parse(paths):
    rows = {}
    for path in paths:
        for line in open(path):
            if not line.startswith("Benchmark"):
                continue
            parts = line.split()
            name = re.sub(r"-\d+$", "", parts[0])
            cpu = re.search(r"-(\d+)$", parts[0])
            key = (name, cpu.group(1) if cpu else "")
            metrics = rows.setdefault(key, {})
            i = 2
            while i + 1 < len(parts):
                try:
                    value = float(parts[i])
                except ValueError:
                    i += 1
                    continue
                unit = parts[i + 1]
                metrics.setdefault(unit, []).append(value)
                i += 2
    return rows

def fmt(unit, v):
    if unit == "ns/op":
        return f"{v/1000:.2f} µs" if v >= 1000 else f"{v:.0f} ns"
    if unit in ("p50-ns", "p99-ns"):
        return f"{v/1000:.1f} µs"
    if unit == "B/op":
        return f"{v/1024:.1f} KB" if v >= 1024 else f"{v:.0f} B"
    if unit == "hooks/s":
        return f"{v/1000:.0f}k"
    if unit in ("allocs/op", "gc-cycles"):
        return f"{v:.0f}"
    if unit == "MB/s":
        return f"{v:.1f}"
    return f"{v:.2f}"

def main(argv):
    pattern, strip, paths = None, "", []
    it = iter(argv)
    for a in it:
        if a == "-k":
            pattern = re.compile(next(it))
        elif a == "-strip":
            strip = next(it)
        else:
            paths.append(a)
    rows = parse(paths)
    units = []
    for m in rows.values():
        for u in m:
            if u not in units:
                units.append(u)
    order = ["ns/op", "B/op", "allocs/op", "hooks/s", "p50-ns", "p99-ns", "gc-cpu-pct", "gc-cycles", "MB/s", "ballast-MiB"]
    units.sort(key=lambda u: order.index(u) if u in order else 99)
    print("| benchmark | cpu | " + " | ".join(units) + " | runs | spread |")
    print("|---|---|" + "---|" * len(units) + "---|---|")
    for (name, cpu), m in rows.items():
        if pattern and not pattern.search(name):
            continue
        label = name[len(strip):] if name.startswith(strip) else name
        cells = []
        for u in units:
            vals = m.get(u)
            cells.append(fmt(u, statistics.median(vals)) if vals else "")
        ns = m.get("ns/op", [])
        spread = f"{(max(ns)-min(ns))/statistics.median(ns)*100:.0f}%" if len(ns) > 1 and statistics.median(ns) else ""
        print(f"| {label} | {cpu} | " + " | ".join(cells) + f" | {len(ns)} | {spread} |")

if __name__ == "__main__":
    main(sys.argv[1:])
