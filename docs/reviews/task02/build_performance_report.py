"""Build task02 comparison from terminal, complete benchmark matrices.

Run after the named final raw files have been saved; incomplete input is an error.
No tests or measurements are executed by this script.
"""
import argparse
import hashlib
import json
import re
import statistics
from pathlib import Path

from summarize import samples, summary


OPS = ("Get", "Recall", "Consolidate", "SourceForget", "Sweep")
ADAPTERS = ("memory", "sqlite")
COUNTERS = (
    "get-requests/op", "listed-entries/op", "returned-bytes/op",
    "decoded-docs/op", "decoded-bytes/op", "index-nodes/op",
    "memory-copy-bytes/op", "sql-metadata-rows/op", "sql-value-rows/op",
    "sql-value-bytes/op", "sql-returned-rows/op",
)


def expected_cases():
    fixed = [f"BenchmarkLifecycleCost/{a}/{n}/{op}"
             for a in ADAPTERS for n in (1000, 10000, 100000) for op in OPS]
    growing = [f"BenchmarkGrowingLineage/{a}/{n}/{op}"
               for a in ADAPTERS for n in (10, 100, 1000) for op in OPS]
    reopen = [f"BenchmarkSQLiteReopenGet/{n}" for n in (1000, 10000, 100000)]
    return fixed + growing + reopen


def read_matrix(directory, filename, expected, iterations=1):
    path = directory / filename
    source = path.read_text()
    if not re.search(r"^PASS$", source, re.MULTILINE) or re.search(r"^FAIL\b", source, re.MULTILINE):
        raise ValueError(f"{filename}: terminal PASS output required")
    grouped = samples(path)
    if set(grouped) != set(expected):
        raise ValueError(f"{filename}: missing={sorted(set(expected)-set(grouped))}, "
                         f"unexpected={sorted(set(grouped)-set(expected))}")
    for name, rows in grouped.items():
        if len(rows) != 3:
            raise ValueError(f"{filename}: {name} has {len(rows)} samples, expected 3")
        for row in rows:
            for metric in ("ns/op", "B/op", "allocs/op"):
                if metric not in row:
                    raise ValueError(f"{filename}: {name} missing {metric}")
            if row.get("censored/op", 0) not in (0, 1):
                raise ValueError(f"{filename}: {name} invalid censorship flag")
            if name.startswith("BenchmarkLifecycleCost/") and "censored/op" not in row:
                raise ValueError(f"{filename}: {name} missing explicit censorship flag")
    measured_lines = [line.split() for line in source.splitlines()
                      if line.startswith("Benchmark") and len(line.split()) >= 4
                      and line.split()[1].isdigit()]
    if any(int(fields[1]) != iterations for fields in measured_lines):
        raise ValueError(f"{filename}: expected b.N={iterations} per repetition")
    return grouped, {"file": filename, "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
                     "cases": len(grouped), "samples": sum(map(len, grouped.values())),
                     "iterations_per_sample": iterations}


def display(value, scale=1):
    return "—" if value is None else f"{value / scale:,.3g}"


def median(row, metric):
    return row["complete_median"].get(metric)


def ratio(before, after, metric):
    if before["completed_samples"] != 3 or after["completed_samples"] != 3:
        return "—"
    b, a = median(before, metric), median(after, metric)
    return "—" if b is None or a is None or b == 0 else f"{a/b:.3g}"


def label(name):
    return name.replace("BenchmarkLifecycleCost/", "fixed/").replace(
        "BenchmarkGrowingLineage/", "lineage/").replace("BenchmarkSQLiteReopenGet/", "reopen/sqlite/")


def table(headers, rows):
    return ["| " + " | ".join(headers) + " |",
            "| " + " | ".join("---" for _ in headers) + " |"] + [
                "| " + " | ".join(map(str, row)) + " |" for row in rows]


def build(directory):
    names = expected_cases()
    affected = [name for name in names if name.rsplit("/", 1)[-1] in ("SourceForget", "Sweep")]
    provenance = []

    def read(filename, cases, iterations=1):
        data, metadata = read_matrix(directory, filename, cases, iterations)
        provenance.append(metadata)
        return data

    before = read("baseline-raw.txt", names[:30])
    before.update(read("before-extended-raw.txt", names[30:]))
    after = read("final-after-raw.txt", names)
    after.update(read("final-affected-raw.txt", affected))
    contention_names = [f"BenchmarkCallbackContention/{a}" for a in ADAPTERS]
    cbefore = read("before-contention-raw.txt", contention_names, 20)
    cafter = read("final-contention-raw.txt", contention_names, 20)
    for filename, grouped in (("before-contention-raw.txt", cbefore), ("final-contention-raw.txt", cafter)):
        for name, rows in grouped.items():
            if any(row.get("held-callback-ns") != 50_000_000 or
                   "write-p50-ns" not in row or "write-p95-ns" not in row for row in rows):
                raise ValueError(f"{filename}: {name} must report 50ms hold and write p50/p95")
    b, a = summary(before), summary(after)
    bc, ac = summary(cbefore), summary(cafter)
    report = {"inputs": provenance, "replacement_cases": affected,
              "before": b, "after": a, "contention_before": bc, "contention_after": ac}
    lines = ["# Task02 performance comparison", "",
             "This report compares 63 cases with three independent single-operation repetitions per case. "
             "It is measurement evidence; independent acceptance reports establish task completion.", "",
             "Baseline production is commit `9f7b50fa354e6fd74a9def6886850bf50d3aec78`. "
             "Final SourceForget/Sweep samples replace the earlier compiled matrix for all 24 affected cases. "
             "The remaining 39 cases come from final-after-raw.txt. Input hashes are in performance-summary.json; "
             "production/harness source provenance is maintained separately with final verification artifacts.", "",
             "See [performance-method.md](performance-method.md) for corpus, timing, counters and limitations.", "",
             "## Completed observations", "",
             "Time, allocation bytes and allocation counts below are medians of completed repetitions only. "
             "The completion column is before → after out of three. Ratios are after/before and are shown only "
             "when both sides completed all three repetitions; lower is less observed cost. A missing median "
             "means no completed sample, not zero cost. These three samples cannot substantiate tail latency.", ""]
    rows = []
    for name in names:
        old, new = b[name], a[name]
        rows.append([label(name), f"{old['completed_samples']}/3 → {new['completed_samples']}/3",
                     display(median(old, "ns/op"), 1e6), display(median(new, "ns/op"), 1e6), ratio(old, new, "ns/op"),
                     display(median(old, "B/op"), 1e6), display(median(new, "B/op"), 1e6), ratio(old, new, "B/op"),
                     display(median(old, "allocs/op")), display(median(new, "allocs/op")), ratio(old, new, "allocs/op")])
    lines += table(["Case", "Completed", "Before ms", "After ms", "Time ratio",
                    "Before MB", "After MB", "Bytes ratio", "Before allocs", "After allocs", "Allocs ratio"], rows)
    lines += ["", "## Deadline-censored observations", "",
              "Fixed-corpus operations use a five-second context deadline. A censored sample returned "
              "context.DeadlineExceeded and did not finish the operation. Legacy loops may notice cancellation "
              "well after the deadline. Elapsed time and allocations describe interrupted work, not successful "
              "latency; no speedup ratio is computed from these samples.", ""]
    rows = []
    for side, data in (("before", before), ("after", after)):
        for name in names:
            censored = [row for row in data[name] if row.get("censored/op", 0) != 0]
            if censored:
                rows.append([side, label(name), len(censored),
                             "; ".join(display(row["ns/op"], 1e6) for row in censored),
                             "; ".join(display(row["B/op"], 1e6) for row in censored)])
    lines += table(["Side", "Case", "Censored / 3", "Observed ms", "Observed MB"], rows) if rows else ["No deadline-censored samples."]
    lines += ["", "## Storage work", "",
              "Baseline exposes only public requests and returned bytes for the fixed corpus. Private counters "
              "were added after the baseline; a missing before value is unavailable, not zero. Every entry below "
              "is a median of completed samples. Private counters describe the instrumented work only, with "
              "no claim about SQLite VM steps or disk I/O. Full raw samples and metrics remain in performance-summary.json.", ""]
    for metrics, title in ((COUNTERS[:3], "Public storage requests"), (COUNTERS[3:], "Private after counters")):
        lines += [f"### {title}", ""]
        eligible = [name for name in names if any(m in a[name]["complete_median"] or m in b[name]["complete_median"] for m in metrics)]
        rows = []
        for name in eligible:
            values = [label(name)]
            for metric in metrics:
                values.append(f"{display(median(b[name], metric))} → {display(median(a[name], metric))}" if metrics == COUNTERS[:3]
                              else display(median(a[name], metric)))
            rows.append(values)
        lines += table(["Case"] + [m.removesuffix("/op") for m in metrics], rows)
        lines += [""]
    lines += ["## Callback contention", "",
              "Each repetition records 20 pairs of writes to independent scopes B and C while a ranker callback "
              "is held in scope A. Both harnesses release it 50ms after callback entry. The write pair is timed; "
              "waiting for callback completion and store setup/close are excluded. Table values are medians "
              "of the three repetition-level p50/p95 measurements, not pooled quantiles of 60 observations. "
              "SQLite still serializes physical writers; the comparison concerns callback-held scope fencing.", ""]
    rows = []
    for name in contention_names:
        rows.append([name.rsplit("/", 1)[-1]] + [
            display(median(data[name], metric), 1e6)
            for metric in ("write-p50-ns", "write-p95-ns") for data in (bc, ac)])
    lines += table(["Adapter", "Before p50 ms", "After p50 ms", "Before p95 ms", "After p95 ms"], rows)
    lines += ["", "Local timing depends on uncontrolled workstation activity. Structural tests and query bounds "
              "support bounded-work claims; descriptive timing is neither a CI threshold nor a throughput guarantee.", ""]
    return report, "\n".join(lines)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", type=Path, default=Path(__file__).resolve().parent)
    args = parser.parse_args()
    result, markdown = build(args.directory)
    (args.directory / "performance-summary.json").write_text(json.dumps(result, indent=2) + "\n")
    (args.directory / "performance-report.md").write_text(markdown)
