"""Summarize repeated Go benchmark observations without accepting censored runs."""
import json
import re
import statistics
import sys
from pathlib import Path


def samples(path):
    grouped = {}
    for line in Path(path).read_text().splitlines():
        fields = line.split()
        if not fields or not fields[0].startswith("Benchmark") or len(fields) < 4:
            continue
        if not fields[1].isdigit():
            continue
        name = re.sub(r"-\d+$", "", fields[0])
        metrics = {}
        for offset in range(2, len(fields) - 1, 2):
            metrics[fields[offset + 1]] = float(fields[offset])
        grouped.setdefault(name, []).append(metrics)
    return grouped


def summary(grouped):
    result = {}
    for name, rows in grouped.items():
        complete = [row for row in rows if row.get("censored/op", 0) == 0]
        result[name] = {
            "samples": len(rows),
            "completed_samples": len(complete),
            "censored_samples": len(rows) - len(complete),
            "completed_latency_ns": [row["ns/op"] for row in complete],
            "complete_median": {
                metric: statistics.median(row[metric] for row in complete)
                for metric in sorted(set.intersection(*(set(row) for row in complete)))
            } if complete else {},
            "all_samples": rows,
        }
    return result


if __name__ == "__main__":
    source, destination = sys.argv[1:]
    Path(destination).write_text(json.dumps(summary(samples(source)), indent=2) + "\n")
