#!/usr/bin/env python3
"""plugin_chart.py draws the plugin scenario chart, docs/assets/plugin-bench.en.png
and plugin-bench.zh.png, from the matrix's results: one JSON object per line,
the throughput runs (engine, scenario, workers, rep, rps) and the in-use
memory runs at 32 workers (engine, scenario, peak_mib). Every cell is the
median of its rounds. --tables prints the requests/s and memory tables of
docs/performance.md and docs/performance.zh_CN.md instead.

    uv run --with matplotlib bench/scripts/plugin_chart.py docs/assets/plugin-bench.jsonl
    uv run --with matplotlib bench/scripts/plugin_chart.py --tables docs/assets/plugin-bench.jsonl

The text uses Nimbus Sans and Noto Sans CJK SC. LABEL is the moejs version and
date the subtitle shows, MACHINE the machine it describes.
"""

import argparse
import json
import math
import os
import statistics
import sys
from collections import defaultdict

LABEL = {"en": "moejs v0.1.0-alpha.9, 2026-10-21", "zh": "moejs v0.1.0-alpha.9，2026-10-21"}
MACHINE = {
    "en": "Intel Xeon E5-2667 v4 pinned to 8 of 14 vCPUs (KVM guest), Debian 13, Go 1.26.6",
    "zh": "Intel Xeon E5-2667 v4，绑定 KVM 虚拟机 14 个 vCPU 中的 8 个，Debian 13，Go 1.26.6",
}

# Latin text in Nimbus Sans, Chinese in Noto Sans CJK SC.
FONT = ["Nimbus Sans", "Noto Sans CJK SC"]
INK = "#0f172a"
SUB = "#475569"
MUTED = "#64748b"
AXIS = "#94a3b8"
GRID = "#e2e8f0"
BAND = "#eef2f6"
MOEJS_LABEL = "#9a3412"
REF_LABEL = "#8a95a5"

# key, English name, Chinese name, colour
ENGINES = [
    ("moejs-nocopy", "moejs (no copy)", "moejs（不拷贝）", "#c2410c"),
    ("moejs", "moejs", "moejs", "#fdba8c"),
    ("sobek", "Sobek", "Sobek", "#d3d9e1"),
    ("modernc", "QuickJS pure Go (modernc)", "QuickJS 纯 Go（modernc）", "#cdbf9f"),
    ("quickjs-go", "quickjs-go (default)", "quickjs-go 默认", "#a7b1bf"),
    ("quickjs-go-nocheck", "quickjs-go (check off)", "quickjs-go 关检查", "#748196"),
]
# The C hosts are drawn white with hatching in the colour given.
REFS = [
    ("c-bellard", "QuickJS (C)", "QuickJS（C）", "#a7b1bf"),
    ("c-ng-o3", "quickjs-ng (C)", "quickjs-ng（C）", "#748196"),
]
WORKERS = [1, 8, 32]

TEXT = {
    "en": {
        "title": "moejs plugin benchmark",
        "subtitle": "{machine}, {label}. One runtime per worker, JSON bytes in and out, median of 3 rounds.",
        "embed": "Engines a Go program can embed",
        "ref": "Reference: called from C, no Go",
        "band": "C reference",
        "workers": "{} worker",
        "workers_pl": "{} workers",
        "rps": "requests/s",
        "panels": [
            ("Small requests", "269 real calls to new-api's 10 task plugins, about 700 B each. Higher is better"),
            ("Agent requests, 100k tokens", "Coding-agent sessions (OpenAI Chat) rewritten by the plugin, 350 KB in and out. Higher is better"),
            ("Large requests: one 8 MiB image", "25 MB in, 17 MB out per request. Higher is better"),
            ("Memory in use, large requests, 32 workers", "Bytes still referenced once every call has written its output. Lower is better"),
        ],
        "mem_groups": ["one 8 MiB image", "eight 1 MiB images"],
        "foot": [
            "moejs (no copy) reads the caller's string in place with ParseJSONString; moejs copies it first with ParseJSON. "
            "quickjs-go v0.7.7 turns its goroutine check on by default. Eight 1 MiB images run within {multi} % of one 8 MiB image.",
            "modernc allocates through its own allocator, which gives no in-use figure, so it is not in the memory chart.",
        ],
    },
    "zh": {
        "title": "moejs 插件场景基准",
        "subtitle": "{machine}，{label}。每个 worker 一个运行时，JSON 字节进出，3 轮取中位数。",
        "embed": "Go 程序里能嵌入的引擎",
        "ref": "参考：C 直接调用，不经过 Go",
        "band": "C 参考",
        "workers": "{} 并发",
        "workers_pl": "{} 并发",
        "rps": "次/秒",
        "panels": [
            ("小请求吞吐", "new-api 10 个任务插件的 269 次真实调用，每次约 700 B。越高越好"),
            ("10 万 token agent 请求吞吐", "编码 agent 会话（OpenAI Chat 格式），插件改写后发往上游，进出各约 350 KB。越高越好"),
            ("大请求吞吐：单张 8 MiB 图", "每次 25 MB 进、17 MB 出。越高越好"),
            ("大请求 32 并发真正占用的内存", "所有调用写完输出时仍被引用的字节。越低越好"),
        ],
        "mem_groups": ["单张 8 MiB 图", "8 张 1 MiB 图"],
        "foot": [
            "moejs（不拷贝）用 ParseJSONString 直接读调用方的字符串，moejs 用 ParseJSON 先拷贝一份。"
            "quickjs-go v0.7.7 默认开启 goroutine 检查。8 张 1 MiB 图的吞吐和单张 8 MiB 图相差不到 {multi}%。",
            "modernc 用自己的分配器，统计不到正在用的字节，内存图里没有它。",
        ],
    },
}

TABLE = {
    "en": {
        "rps_head": ["Scenario", "Workers"],
        "mem_head": ["Scenario"],
        "scenarios": {
            "small": "Small requests",
            "agent100k": "Agent requests, 100k tokens",
            "big8m": "One 8 MiB image",
            "multi8x1m": "Eight 1 MiB images",
        },
    },
    "zh": {
        "rps_head": ["场景", "并发"],
        "mem_head": ["场景"],
        "scenarios": {
            "small": "小请求",
            "agent100k": "10 万 token agent 请求",
            "big8m": "单张 8 MiB 图",
            "multi8x1m": "8 张 1 MiB 图",
        },
    },
}


def load(paths):
    """load returns the median requests/s per (engine, scenario, workers) and
    the in-use MiB per (engine, scenario)."""
    runs = defaultdict(list)
    mem = defaultdict(list)
    for path in paths:
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                r = json.loads(line)
                if r.get("failed"):
                    continue
                if "rps" in r:
                    runs[r["engine"], r["scenario"], r["workers"]].append(r["rps"])
                elif "peak_mib" in r:
                    mem[r["engine"], r["scenario"]].append(r["peak_mib"])
    rps = {k: statistics.median(v) for k, v in runs.items()}
    inuse = {k: statistics.median(v) for k, v in mem.items()}
    return rps, inuse


def multi_within(rps):
    """multi_within returns by how many percent, rounded up, eight 1 MiB
    images differ at most from one 8 MiB image."""
    worst = 0.0
    for key, _, _, _ in ENGINES + REFS:
        for w in WORKERS:
            a, b = rps.get((key, "big8m", w)), rps.get((key, "multi8x1m", w))
            if a and b:
                worst = max(worst, abs(b / a - 1))
    return math.ceil(worst * 100)


def fmt_small(v, lang):
    if lang == "zh":
        return f"{v / 1e4:.1f}万" if v >= 1e4 else f"{v:,.0f}"
    if v >= 1e4:
        return f"{v / 1e3:.0f}k"
    if v >= 1e3:
        return f"{v / 1e3:.1f}k"
    return f"{v:.0f}"


def fmt_large(v, lang):
    return f"{v:.1f}" if v < 100 else f"{v:,.0f}"


def fmt_int(v, lang):
    return f"{v:,.0f}"


def tick_small(v, lang):
    if v == 0:
        return "0"
    return f"{v / 1e4:g}万" if lang == "zh" else f"{v / 1e3:g}k"


def cell(v, decimals):
    if v is None:
        return "—"
    if decimals and v < 100:
        return f"{v:.1f}"
    return f"{v:,.0f}"


def tables(rps, inuse):
    out = []
    for lang in ("en", "zh"):
        t = TABLE[lang]
        names = [e[1] if lang == "en" else e[2] for e in ENGINES + REFS]
        head = t["rps_head"] + names
        out.append("| " + " | ".join(head) + " |")
        out.append("|---|--:|" + "--:|" * len(names))
        for s in ("small", "agent100k", "big8m", "multi8x1m"):
            for w in WORKERS:
                vals = [cell(rps.get((e[0], s, w)), s in ("big8m", "multi8x1m")) for e in ENGINES + REFS]
                out.append(f"| {t['scenarios'][s]} | {w} | " + " | ".join(vals) + " |")
        out.append("")
        out.append("| " + " | ".join(t["mem_head"] + names) + " |")
        out.append("|---|" + "--:|" * len(names))
        for s in ("big8m", "multi8x1m"):
            vals = [cell(inuse.get((e[0], s)), False) for e in ENGINES + REFS]
            out.append(f"| {t['scenarios'][s]} | " + " | ".join(vals) + " |")
        out.append("")
    return "\n".join(out)


# The figure is laid out in pixels of the 3060 x 1890 image.
W, H, DPI = 3060, 1890, 150
BAR = 0.9  # bar width; a bar slot is 1
REF_AT = 6.8  # left edge of the first C bar in a group
GROUP = 10.0
BAND_PAD = (0.35, 0.1)


def draw(lang, rps, inuse, out):
    import matplotlib

    matplotlib.use("Agg")
    import matplotlib.pyplot as plt
    from matplotlib import patheffects
    from matplotlib.patches import Rectangle
    from matplotlib.ticker import FuncFormatter, MaxNLocator

    plt.rcParams.update({
        "font.family": FONT,
        "hatch.linewidth": 1.0,
        "axes.unicode_minus": False,
    })
    tx = TEXT[lang]
    name = 1 if lang == "en" else 2
    fig = plt.figure(figsize=(W / DPI, H / DPI), dpi=DPI)
    fig.patch.set_facecolor("white")

    def at(x, y):
        return x / W, 1 - y / H

    def text(x, y, s, **kw):
        kw.setdefault("va", "baseline")
        return fig.text(*at(x, y), s, **kw)

    text(153, 89, tx["title"], fontsize=29.5, fontweight="bold", color=INK)
    text(153, 145, tx["subtitle"].format(machine=MACHINE[lang], label=LABEL[lang]), fontsize=14, color=SUB)

    # The legend: two groups of swatches, laid out left to right.
    renderer = fig.canvas.get_renderer()

    def legend(x, header, items, header_color, text_color):
        text(x, 283, header, fontsize=14, fontweight="bold", color=header_color)
        for label, color, hatch in items:
            sx, sy, sw, sh = x, 300, 42, 24
            fig.patches.append(Rectangle(at(sx, sy + sh), sw / W, sh / H, transform=fig.transFigure,
                                         facecolor="white" if hatch else color,
                                         edgecolor=color if hatch else "none",
                                         hatch="////" if hatch else None, linewidth=1.2))
            t = text(sx + sw + 22, 322, label, fontsize=15, color=text_color)
            width = t.get_window_extent(renderer).width
            x = sx + sw + 22 + width + 40

    legend(162, tx["embed"], [(e[name], e[3], False) for e in ENGINES], INK, "black")
    legend(2192, tx["ref"], [(e[name], e[3], True) for e in REFS], MUTED, MUTED)

    panels = [
        (150, 1510, 520, 978, "small", fmt_small, True),
        (1670, 3030, 520, 978, "agent100k", fmt_int, True),
        (150, 1510, 1244, 1702, "big8m", fmt_large, True),
        (1670, 3030, 1244, 1702, "memory", fmt_int, False),
    ]
    for i, (x0, x1, y0, y1, scn, fmt, rotated) in enumerate(panels):
        title, sub = tx["panels"][i]
        text(x0 + 3, y0 - 58, title, fontsize=18.8, fontweight="bold", color=INK)
        text(x0 + 3, y0 - 21, sub, fontsize=12.75, color=MUTED)
        ax = fig.add_axes([x0 / W, 1 - y1 / H, (x1 - x0) / W, (y1 - y0) / H])
        if scn == "memory":
            groups = tx["mem_groups"]
            values = [[inuse.get((e[0], s)) for e in ENGINES + REFS] for s in ("big8m", "multi8x1m")]
        else:
            groups = [(tx["workers"] if w == 1 else tx["workers_pl"]).format(w) for w in WORKERS]
            values = [[rps.get((e[0], scn, w)) for e in ENGINES + REFS] for w in WORKERS]
        top = max(v for g in values for v in g if v is not None)
        ax.set_ylim(0, top * (1.3 if rotated else 1.17))
        ax.set_xlim(-0.76, (len(groups) - 1) * GROUP + REF_AT + 1 + BAR + BAND_PAD[1] + 0.4)
        for g, vals in enumerate(values):
            base = g * GROUP
            ax.axvspan(base + REF_AT - BAND_PAD[0], base + REF_AT + 1 + BAR + BAND_PAD[1],
                       color=BAND, zorder=0, linewidth=0)
            ax.text(base + REF_AT + (1 + BAR) / 2, 0.985, tx["band"], transform=ax.get_xaxis_transform(),
                    ha="center", va="top", fontsize=10.8, color=MUTED)
            for j, v in enumerate(vals):
                if v is None:
                    continue
                ref = j >= len(ENGINES)
                left = base + (REF_AT + j - len(ENGINES) if ref else j)
                e = (ENGINES + REFS)[j]
                if ref:
                    ax.bar(left, v, BAR, align="edge", facecolor="white", edgecolor=e[3],
                           hatch="///", linewidth=0.9, zorder=2)
                else:
                    ax.bar(left, v, BAR, align="edge", color=e[3], linewidth=0, zorder=2)
                moejs = e[0].startswith("moejs")
                color = MOEJS_LABEL if moejs else REF_LABEL if ref else SUB
                pad = top * 0.012
                ax.text(left + BAR / 2, v + pad, fmt(v, lang), rotation=90 if rotated else 0,
                        ha="center", va="bottom", fontsize=13 if moejs else 10.8,
                        fontweight="bold" if moejs else "normal", color=color, zorder=3,
                        path_effects=[patheffects.withStroke(linewidth=3, foreground="white")])
        ax.set_xticks([g * GROUP + (REF_AT + 1 + BAR) / 2 for g in range(len(groups))])
        ax.set_xticklabels(groups, fontsize=15, color="black")
        ax.tick_params(axis="x", length=0, pad=4)
        ax.yaxis.set_major_locator(MaxNLocator(nbins=8, steps=[1, 2, 5, 10]))
        if scn == "small":
            ax.yaxis.set_major_formatter(FuncFormatter(lambda v, _: tick_small(v, lang)))
        else:
            ax.yaxis.set_major_formatter(FuncFormatter(lambda v, _: f"{v:,.0f}"))
        ax.tick_params(axis="y", labelsize=12, labelcolor=MUTED, color=AXIS, length=5, width=1)
        ax.set_ylabel("MiB" if scn == "memory" else tx["rps"], fontsize=12.5, color=SUB, labelpad=3)
        ax.grid(axis="y", color=GRID, linewidth=1, zorder=0.5)
        ax.set_axisbelow(True)
        for side in ("top", "right", "left"):
            ax.spines[side].set_visible(False)
        ax.spines["bottom"].set_color(AXIS)
        ax.spines["bottom"].set_linewidth(1)
        ax.set_facecolor("none")

    for k, line in enumerate(tx["foot"]):
        text(153, 1829 + 33 * k, line.format(multi=multi_within(rps)), fontsize=12.6, color=MUTED)

    fig.savefig(out, dpi=DPI, facecolor="white")
    plt.close(fig)


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    assets = os.path.join(here, "..", "..", "docs", "assets")
    p = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    p.add_argument("results", nargs="*", default=[os.path.join(assets, "plugin-bench.jsonl")])
    p.add_argument("-o", default=assets, help="directory for plugin-bench.en.png and plugin-bench.zh.png")
    p.add_argument("--tables", action="store_true", help="print the tables of docs/performance.md")
    a = p.parse_args()
    rps, inuse = load(a.results)
    if a.tables:
        sys.stdout.write(tables(rps, inuse))
        return
    for lang in ("en", "zh"):
        draw(lang, rps, inuse, os.path.join(a.o, f"plugin-bench.{lang}.png"))


if __name__ == "__main__":
    main()
