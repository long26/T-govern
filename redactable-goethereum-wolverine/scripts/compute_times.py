#!/usr/bin/env python3
"""统计 nodelogs 目录中 node*.log 里 verifyTime、adaptationTime（支持 adaptionTime 拼写）与 hashTime 的平均值，
并统一把所有时间转换为 ms 输出。

用法：
    python3 scripts/compute_times.py [日志目录]
默认目录为 `nodelogs`。
"""
import argparse
import glob
import os
import re
import sys


def parse_file(path, stats):
    # 支持多种键名与单位：adaptionTime/adaptationTime/verifyTime/hashTime
    # 单位支持: s, ms, us, µs（若无单位，默认视为 ms）
    pattern = re.compile(
        r"(verifyTime|adaptionTime|adaptationTime|hashTime=|cost)\s*[:=]\s*([0-9]+(?:\.[0-9]+)?)(µs|us|ms|s)?",
        re.IGNORECASE,
    )
    try:
        with open(path, "r", errors="ignore") as f:
            for line in f:
                for m in pattern.finditer(line):
                    raw_key = m.group(1)
                    unit = m.group(3)
                    # 规范键名
                    lk = raw_key.lower()
                    if lk in ("adaptiontime", "adaptationtime"):
                        key = "adaptationTime"
                    elif lk == "verifytime":
                        key = "verifyTime"
                    elif lk == "hashtime=":
                        key = "hashTime"
                    elif lk == "cost":
                        key = "cost"
                    else:
                        key = raw_key

                    val = float(m.group(2))
                    # 转换到 ms
                    if unit is None:
                        factor = 1.0
                    else:
                        u = unit.lower()
                        if u == "s":
                            factor = 1000.0
                        elif u == "ms":
                            factor = 1.0
                        elif u in ("us", "µs"):
                            factor = 0.001
                        else:
                            factor = 1.0

                    val_ms = val * factor
                    rec = stats.setdefault(key, {"sum_ms": 0.0, "count": 0})
                    rec["sum_ms"] += val_ms
                    rec["count"] += 1
    except Exception as e:
        print(f"无法读取文件 {path}: {e}", file=sys.stderr)


def main():
    parser = argparse.ArgumentParser(description="统计 nodelogs 目录中 verifyTime/adaptationTime/hashTime 的平均值，统一输出为 ms")
    parser.add_argument("dir", nargs="?", default="nodelogs", help="日志目录（默认：nodelogs）")
    args = parser.parse_args()

    logdir = args.dir
    if not os.path.isdir(logdir):
        print(f"目录不存在: {logdir}", file=sys.stderr)
        sys.exit(1)

    stats = {}
    files = sorted(glob.glob(os.path.join(logdir, "node*.log")))
    if not files:
        print(f"在目录 {logdir} 中未找到 node*.log 文件。", file=sys.stderr)
        sys.exit(1)

    for p in files:
        parse_file(p, stats)

    # 输出均以 ms 为单位
    for key in ("verifyTime", "adaptationTime", "hashTime","cost"):
        rec = stats.get(key)
        if rec and rec["count"] > 0:
            avg_ms = rec["sum_ms"] / rec["count"]
            print(f"{key} 平均值: {avg_ms:.6f} ms (count={rec['count']})")
        else:
            print(f"{key} 未找到任何值")


if __name__ == "__main__":
    main()
