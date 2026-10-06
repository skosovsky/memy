# Task02 performance comparison

This report compares 63 cases with three independent single-operation repetitions per case. It is measurement evidence; independent acceptance reports establish task completion.

Baseline production is commit `9f7b50fa354e6fd74a9def6886850bf50d3aec78`. Final SourceForget/Sweep samples replace the earlier compiled matrix for all 24 affected cases. The remaining 39 cases come from final-after-raw.txt. Input hashes are in performance-summary.json; production/harness source provenance is maintained separately with final verification artifacts.

See [performance-method.md](performance-method.md) for corpus, timing, counters and limitations.

## Completed observations

Time, allocation bytes and allocation counts below are medians of completed repetitions only. The completion column is before → after out of three. Ratios are after/before and are shown only when both sides completed all three repetitions; lower is less observed cost. A missing median means no completed sample, not zero cost. These three samples cannot substantiate tail latency.

| Case | Completed | Before ms | After ms | Time ratio | Before MB | After MB | Bytes ratio | Before allocs | After allocs | Allocs ratio |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| fixed/memory/1000/Get | 3/3 → 3/3 | 3.41 | 0.665 | 0.195 | 3.76 | 0.408 | 0.108 | 6.69e+03 | 4.65e+03 | 0.695 |
| fixed/memory/1000/Recall | 3/3 → 3/3 | 8.35 | 2.33 | 0.279 | 4.55 | 2.04 | 0.447 | 1.61e+04 | 2.32e+04 | 1.44 |
| fixed/memory/1000/Consolidate | 3/3 → 3/3 | 696 | 3.65 | 0.00524 | 238 | 2.73 | 0.0115 | 2.63e+06 | 3.07e+04 | 0.0117 |
| fixed/memory/1000/SourceForget | 3/3 → 3/3 | 1.26e+03 | 805 | 0.638 | 593 | 827 | 1.39 | 5.93e+06 | 9.5e+06 | 1.6 |
| fixed/memory/1000/Sweep | 3/3 → 3/3 | 501 | 200 | 0.399 | 189 | 209 | 1.1 | 1.99e+06 | 2.34e+06 | 1.18 |
| fixed/memory/10000/Get | 3/3 → 3/3 | 25.1 | 0.646 | 0.0258 | 33.3 | 0.408 | 0.0122 | 2.47e+04 | 4.65e+03 | 0.188 |
| fixed/memory/10000/Recall | 3/3 → 3/3 | 26.4 | 2.16 | 0.0821 | 33.8 | 2.04 | 0.0602 | 3.41e+04 | 2.32e+04 | 0.679 |
| fixed/memory/10000/Consolidate | 0/3 → 3/3 | — | 2.95 | — | — | 2.71 | — | — | 3.06e+04 | — |
| fixed/memory/10000/SourceForget | 0/3 → 0/3 | — | — | — | — | — | — | — | — | — |
| fixed/memory/10000/Sweep | 3/3 → 3/3 | 2.74e+03 | 1.84e+03 | 0.67 | 1.89e+03 | 2.08e+03 | 1.1 | 1.98e+07 | 2.33e+07 | 1.18 |
| fixed/memory/100000/Get | 3/3 → 3/3 | 165 | 0.583 | 0.00354 | 326 | 0.408 | 0.00125 | 2.05e+05 | 4.65e+03 | 0.0227 |
| fixed/memory/100000/Recall | 3/3 → 3/3 | 327 | 2.15 | 0.00657 | 323 | 2.04 | 0.0063 | 2.15e+05 | 2.32e+04 | 0.108 |
| fixed/memory/100000/Consolidate | 0/3 → 3/3 | — | 2.88 | — | — | 2.71 | — | — | 3.06e+04 | — |
| fixed/memory/100000/SourceForget | 0/3 → 0/3 | — | — | — | — | — | — | — | — | — |
| fixed/memory/100000/Sweep | 0/3 → 0/3 | — | — | — | — | — | — | — | — | — |
| fixed/sqlite/1000/Get | 3/3 → 3/3 | 14.9 | 1.22 | 0.0817 | 10.7 | 0.436 | 0.0407 | 2.08e+04 | 5e+03 | 0.24 |
| fixed/sqlite/1000/Recall | 3/3 → 3/3 | 13.2 | 4.2 | 0.318 | 11.5 | 2.12 | 0.184 | 3.02e+04 | 2.43e+04 | 0.806 |
| fixed/sqlite/1000/Consolidate | 3/3 → 3/3 | 344 | 5.56 | 0.0162 | 266 | 2.82 | 0.0106 | 2.68e+06 | 3.23e+04 | 0.0121 |
| fixed/sqlite/1000/SourceForget | 3/3 → 3/3 | 1.04e+03 | 1.4e+03 | 1.35 | 607 | 861 | 1.42 | 6e+06 | 1.01e+07 | 1.68 |
| fixed/sqlite/1000/Sweep | 3/3 → 3/3 | 224 | 958 | 4.28 | 203 | 222 | 1.09 | 2.01e+06 | 2.59e+06 | 1.28 |
| fixed/sqlite/10000/Get | 3/3 → 3/3 | 96.6 | 1.68 | 0.0174 | 102 | 0.429 | 0.0042 | 1.65e+05 | 4.98e+03 | 0.0302 |
| fixed/sqlite/10000/Recall | 3/3 → 3/3 | 95.7 | 4.37 | 0.0456 | 102 | 2.13 | 0.0207 | 1.74e+05 | 2.44e+04 | 0.14 |
| fixed/sqlite/10000/Consolidate | 2/3 → 3/3 | 3.16e+03 | 5.82 | — | 2.64e+03 | 2.83 | — | 2.66e+07 | 3.24e+04 | — |
| fixed/sqlite/10000/SourceForget | 0/3 → 0/3 | — | — | — | — | — | — | — | — | — |
| fixed/sqlite/10000/Sweep | 3/3 → 0/3 | 2.95e+03 | — | — | 2.03e+03 | — | — | 2.01e+07 | — | — |
| fixed/sqlite/100000/Get | 3/3 → 3/3 | 3.64e+03 | 1.27 | 0.00035 | 1e+03 | 0.431 | 0.000429 | 1.61e+06 | 4.99e+03 | 0.00311 |
| fixed/sqlite/100000/Recall | 3/3 → 3/3 | 2.2e+03 | 4.56 | 0.00208 | 1e+03 | 2.12 | 0.00211 | 1.62e+06 | 2.43e+04 | 0.015 |
| fixed/sqlite/100000/Consolidate | 0/3 → 3/3 | — | 5.54 | — | — | 2.82 | — | — | 3.23e+04 | — |
| fixed/sqlite/100000/SourceForget | 0/3 → 0/3 | — | — | — | — | — | — | — | — | — |
| fixed/sqlite/100000/Sweep | 0/3 → 0/3 | — | — | — | — | — | — | — | — | — |
| lineage/memory/10/Get | 3/3 → 3/3 | 6.74 | 7.18 | 1.07 | 6.18 | 6.17 | 0.998 | 6.79e+04 | 6.8e+04 | 1 |
| lineage/memory/10/Recall | 3/3 → 3/3 | 20.5 | 35.5 | 1.73 | 18.7 | 29.3 | 1.57 | 2.07e+05 | 3.24e+05 | 1.56 |
| lineage/memory/10/Consolidate | 3/3 → 3/3 | 20.8 | 22 | 1.06 | 20.2 | 18.3 | 0.903 | 2.24e+05 | 2.03e+05 | 0.905 |
| lineage/memory/10/SourceForget | 3/3 → 3/3 | 7.36 | 14.8 | 2.01 | 7.14 | 11.8 | 1.65 | 7.6e+04 | 1.39e+05 | 1.83 |
| lineage/memory/10/Sweep | 3/3 → 3/3 | 2.42 | 3.1 | 1.28 | 2.48 | 2.77 | 1.12 | 2.61e+04 | 3.13e+04 | 1.2 |
| lineage/memory/100/Get | 3/3 → 3/3 | 69 | 72.1 | 1.04 | 58.3 | 58.1 | 0.996 | 6.38e+05 | 6.38e+05 | 1 |
| lineage/memory/100/Recall | 3/3 → 3/3 | 205 | 341 | 1.67 | 177 | 275 | 1.56 | 1.95e+06 | 3.03e+06 | 1.55 |
| lineage/memory/100/Consolidate | 3/3 → 3/3 | 215 | 199 | 0.927 | 182 | 159 | 0.874 | 2e+06 | 1.75e+06 | 0.874 |
| lineage/memory/100/SourceForget | 3/3 → 3/3 | 68.2 | 126 | 1.84 | 59.5 | 101 | 1.7 | 6.24e+05 | 1.18e+06 | 1.88 |
| lineage/memory/100/Sweep | 3/3 → 3/3 | 23.2 | 26.2 | 1.13 | 20.1 | 22.1 | 1.1 | 2.1e+05 | 2.46e+05 | 1.18 |
| lineage/memory/1000/Get | 3/3 → 3/3 | 513 | 530 | 1.03 | 575 | 571 | 0.994 | 6.31e+06 | 6.31e+06 | 1 |
| lineage/memory/1000/Recall | 3/3 → 3/3 | 1.58e+03 | 2.51e+03 | 1.59 | 1.74e+03 | 2.7e+03 | 1.55 | 1.93e+07 | 3e+07 | 1.55 |
| lineage/memory/1000/Consolidate | 3/3 → 3/3 | 1.61e+03 | 1.44e+03 | 0.893 | 1.78e+03 | 1.55e+03 | 0.869 | 1.97e+07 | 1.71e+07 | 0.87 |
| lineage/memory/1000/SourceForget | 3/3 → 3/3 | 533 | 1.01e+03 | 1.89 | 606 | 992 | 1.64 | 6.08e+06 | 1.16e+07 | 1.91 |
| lineage/memory/1000/Sweep | 3/3 → 3/3 | 174 | 199 | 1.15 | 194 | 213 | 1.1 | 2.04e+06 | 2.38e+06 | 1.17 |
| lineage/sqlite/10/Get | 3/3 → 3/3 | 6.59 | 9.32 | 1.41 | 6.28 | 6.31 | 1.01 | 6.81e+04 | 7.04e+04 | 1.03 |
| lineage/sqlite/10/Recall | 3/3 → 3/3 | 19.9 | 42.8 | 2.15 | 18.8 | 30 | 1.59 | 2.08e+05 | 3.36e+05 | 1.62 |
| lineage/sqlite/10/Consolidate | 3/3 → 3/3 | 22.8 | 27 | 1.19 | 20.6 | 18.7 | 0.908 | 2.25e+05 | 2.1e+05 | 0.933 |
| lineage/sqlite/10/SourceForget | 3/3 → 3/3 | 9.58 | 21.5 | 2.24 | 7.32 | 12.4 | 1.69 | 7.71e+04 | 1.51e+05 | 1.96 |
| lineage/sqlite/10/Sweep | 3/3 → 3/3 | 2.78 | 7.66 | 2.76 | 2.68 | 2.96 | 1.11 | 2.66e+04 | 3.49e+04 | 1.31 |
| lineage/sqlite/100/Get | 3/3 → 3/3 | 68.3 | 81.1 | 1.19 | 59.2 | 59.4 | 1 | 6.4e+05 | 6.59e+05 | 1.03 |
| lineage/sqlite/100/Recall | 3/3 → 3/3 | 210 | 398 | 1.9 | 178 | 281 | 1.58 | 1.95e+06 | 3.14e+06 | 1.61 |
| lineage/sqlite/100/Consolidate | 3/3 → 3/3 | 229 | 231 | 1.01 | 185 | 162 | 0.878 | 2.01e+06 | 1.81e+06 | 0.901 |
| lineage/sqlite/100/SourceForget | 3/3 → 3/3 | 76.5 | 183 | 2.39 | 60.9 | 105 | 1.73 | 6.32e+05 | 1.27e+06 | 2.01 |
| lineage/sqlite/100/Sweep | 3/3 → 3/3 | 24.3 | 68.2 | 2.81 | 21.6 | 23.3 | 1.08 | 2.13e+05 | 2.71e+05 | 1.27 |
| lineage/sqlite/1000/Get | 3/3 → 3/3 | 546 | 932 | 1.71 | 582 | 591 | 1.02 | 6.33e+06 | 6.55e+06 | 1.03 |
| lineage/sqlite/1000/Recall | 3/3 → 3/3 | 1.59e+03 | 4.6e+03 | 2.89 | 1.75e+03 | 2.8e+03 | 1.6 | 1.93e+07 | 3.12e+07 | 1.62 |
| lineage/sqlite/1000/Consolidate | 3/3 → 3/3 | 1.65e+03 | 2.59e+03 | 1.57 | 1.81e+03 | 1.6e+03 | 0.886 | 1.97e+07 | 1.78e+07 | 0.902 |
| lineage/sqlite/1000/SourceForget | 3/3 → 3/3 | 625 | 1.99e+03 | 3.18 | 620 | 1.05e+03 | 1.69 | 6.15e+06 | 1.26e+07 | 2.04 |
| lineage/sqlite/1000/Sweep | 3/3 → 3/3 | 188 | 989 | 5.27 | 209 | 227 | 1.09 | 2.07e+06 | 2.64e+06 | 1.28 |
| reopen/sqlite/1000 | 3/3 → 3/3 | 8.94 | 1.19 | 0.133 | 10.7 | 0.428 | 0.0399 | 2.08e+04 | 5.04e+03 | 0.242 |
| reopen/sqlite/10000 | 3/3 → 3/3 | 82.3 | 1.35 | 0.0165 | 102 | 0.439 | 0.00431 | 1.65e+05 | 5.06e+03 | 0.0307 |
| reopen/sqlite/100000 | 3/3 → 3/3 | 806 | 1.38 | 0.00172 | 1e+03 | 0.434 | 0.000433 | 1.61e+06 | 5.06e+03 | 0.00315 |

## Deadline-censored observations

Fixed-corpus operations use a five-second context deadline. A censored sample returned context.DeadlineExceeded and did not finish the operation. Legacy loops may notice cancellation well after the deadline. Elapsed time and allocations describe interrupted work, not successful latency; no speedup ratio is computed from these samples.

| Side | Case | Censored / 3 | Observed ms | Observed MB |
| --- | --- | --- | --- | --- |
| before | fixed/memory/10000/Consolidate | 3 | 5.01e+03; 4.99e+03; 5e+03 | 806; 1.52e+03; 2.28e+03 |
| before | fixed/memory/10000/SourceForget | 3 | 5.3e+03; 5.36e+03; 9.61e+03 | 3.69e+03; 3.69e+03; 3.69e+03 |
| before | fixed/memory/100000/Consolidate | 3 | 5.01e+03; 5e+03; 5.05e+03 | 2.94e+03; 3.89e+03; 3.02e+03 |
| before | fixed/memory/100000/SourceForget | 3 | 1.52e+05; 2.16e+04; 2.27e+04 | 1.87e+04; 1.87e+04; 1.87e+04 |
| before | fixed/memory/100000/Sweep | 3 | 5e+03; 5e+03; 5e+03 | 4.31e+03; 4.89e+03; 4.25e+03 |
| before | fixed/sqlite/10000/Consolidate | 1 | 5e+03 | 2.13e+03 |
| before | fixed/sqlite/10000/SourceForget | 3 | 5e+03; 5.81e+03; 5e+03 | 4.78e+03; 3.76e+03; 4.25e+03 |
| before | fixed/sqlite/100000/Consolidate | 3 | 5.21e+03; 5.01e+03; 5e+03 | 2e+03; 2.93e+03; 3.01e+03 |
| before | fixed/sqlite/100000/SourceForget | 3 | 2.52e+04; 1.94e+04; 1.79e+04 | 1.93e+04; 1.93e+04; 1.93e+04 |
| before | fixed/sqlite/100000/Sweep | 3 | 5e+03; 5e+03; 5e+03 | 5.45e+03; 5.43e+03; 5.43e+03 |
| after | fixed/memory/10000/SourceForget | 3 | 5e+03; 5e+03; 5e+03 | 5.51e+03; 5.48e+03; 5.69e+03 |
| after | fixed/memory/100000/SourceForget | 3 | 5e+03; 5e+03; 5e+03 | 5.84e+03; 5.76e+03; 5.71e+03 |
| after | fixed/memory/100000/Sweep | 3 | 5e+03; 5e+03; 5e+03 | 5.54e+03; 5.69e+03; 5.73e+03 |
| after | fixed/sqlite/10000/SourceForget | 3 | 5e+03; 5e+03; 5e+03 | 3.32e+03; 3.31e+03; 3.22e+03 |
| after | fixed/sqlite/10000/Sweep | 3 | 5e+03; 5e+03; 5e+03 | 604; 609; 621 |
| after | fixed/sqlite/100000/SourceForget | 3 | 5e+03; 5e+03; 5e+03 | 3.07e+03; 3.01e+03; 2.89e+03 |
| after | fixed/sqlite/100000/Sweep | 3 | 5e+03; 5e+03; 5e+03 | 597; 578; 604 |

## Storage work

Baseline exposes only public requests and returned bytes for the fixed corpus. Private counters were added after the baseline; a missing before value is unavailable, not zero. Every entry below is a median of completed samples. Private counters describe the instrumented work only, with no claim about SQLite VM steps or disk I/O. Full raw samples and metrics remain in performance-summary.json.

### Public storage requests

| Case | get-requests | listed-entries | returned-bytes |
| --- | --- | --- | --- |
| fixed/memory/1000/Get | 6 → 8 | 1 → 1 | 2.86e+03 → 2.86e+03 |
| fixed/memory/1000/Recall | 18 → 36 | 0 → 0 | 8.59e+03 → 1.43e+04 |
| fixed/memory/1000/Consolidate | 5.03e+03 → 52 | 1e+03 → 0 | 1.45e+06 → 1.87e+04 |
| fixed/memory/1000/SourceForget | 7 → 1.22e+04 | 4e+03 → 3e+03 | 5.76e+06 → 7.66e+06 |
| fixed/memory/1000/Sweep | 0 → 3.01e+03 | 1e+03 → 1e+03 | 1.43e+06 → 1.96e+06 |
| fixed/memory/10000/Get | 6 → 8 | 1 → 1 | 2.86e+03 → 2.86e+03 |
| fixed/memory/10000/Recall | 18 → 36 | 0 → 0 | 8.59e+03 → 1.43e+04 |
| fixed/memory/10000/Consolidate | — → 52 | — → 0 | — → 1.87e+04 |
| fixed/memory/10000/Sweep | 0 → 3e+04 | 1e+04 → 1e+04 | 1.43e+07 → 1.96e+07 |
| fixed/memory/100000/Get | 6 → 8 | 1 → 1 | 2.86e+03 → 2.86e+03 |
| fixed/memory/100000/Recall | 18 → 36 | 0 → 0 | 8.59e+03 → 1.43e+04 |
| fixed/memory/100000/Consolidate | — → 52 | — → 0 | — → 1.87e+04 |
| fixed/sqlite/1000/Get | 6 → 8 | 1 → 1 | 2.86e+03 → 2.86e+03 |
| fixed/sqlite/1000/Recall | 18 → 36 | 0 → 0 | 8.59e+03 → 1.43e+04 |
| fixed/sqlite/1000/Consolidate | 5.03e+03 → 52 | 1e+03 → 0 | 1.45e+06 → 1.87e+04 |
| fixed/sqlite/1000/SourceForget | 7 → 1.22e+04 | 4e+03 → 3e+03 | 5.76e+06 → 7.66e+06 |
| fixed/sqlite/1000/Sweep | 0 → 3.01e+03 | 1e+03 → 1e+03 | 1.43e+06 → 1.96e+06 |
| fixed/sqlite/10000/Get | 6 → 8 | 1 → 1 | 2.86e+03 → 2.86e+03 |
| fixed/sqlite/10000/Recall | 18 → 36 | 0 → 0 | 8.59e+03 → 1.43e+04 |
| fixed/sqlite/10000/Consolidate | 5e+04 → 52 | 1e+04 → 0 | 1.44e+07 → 1.87e+04 |
| fixed/sqlite/10000/Sweep | 0 → — | 1e+04 → — | 1.43e+07 → — |
| fixed/sqlite/100000/Get | 6 → 8 | 1 → 1 | 2.86e+03 → 2.86e+03 |
| fixed/sqlite/100000/Recall | 18 → 36 | 0 → 0 | 8.59e+03 → 1.43e+04 |
| fixed/sqlite/100000/Consolidate | — → 52 | — → 0 | — → 1.87e+04 |

### Private after counters

| Case | decoded-docs | decoded-bytes | index-nodes | memory-copy-bytes | sql-metadata-rows | sql-value-rows | sql-value-bytes | sql-returned-rows |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| fixed/memory/1000/Get | 2 | 2.86e+03 | 18 | 2.86e+03 | 0 | 0 | 0 | — |
| fixed/memory/1000/Recall | 10 | 1.43e+04 | 0 | 1.43e+04 | 0 | 0 | 0 | — |
| fixed/memory/1000/Consolidate | 17 | 1.87e+04 | 0 | 1.87e+04 | 0 | 0 | 0 | — |
| fixed/memory/1000/SourceForget | 1.11e+04 | 7.18e+06 | 4.44e+04 | 7.66e+06 | 0 | 0 | 0 | — |
| fixed/memory/1000/Sweep | 3.01e+03 | 1.96e+06 | 1.29e+04 | 1.96e+06 | 0 | 0 | 0 | — |
| fixed/memory/10000/Get | 2 | 2.86e+03 | 24 | 2.86e+03 | 0 | 0 | 0 | — |
| fixed/memory/10000/Recall | 10 | 1.43e+04 | 0 | 1.43e+04 | 0 | 0 | 0 | — |
| fixed/memory/10000/Consolidate | 17 | 1.87e+04 | 0 | 1.87e+04 | 0 | 0 | 0 | — |
| fixed/memory/10000/Sweep | 3e+04 | 1.96e+07 | 1.62e+05 | 1.96e+07 | 0 | 0 | 0 | — |
| fixed/memory/100000/Get | 2 | 2.86e+03 | 29 | 2.86e+03 | 0 | 0 | 0 | — |
| fixed/memory/100000/Recall | 10 | 1.43e+04 | 0 | 1.43e+04 | 0 | 0 | 0 | — |
| fixed/memory/100000/Consolidate | 17 | 1.87e+04 | 0 | 1.87e+04 | 0 | 0 | 0 | — |
| fixed/sqlite/1000/Get | 2 | 2.86e+03 | 0 | 0 | 1 | 2 | 2.86e+03 | — |
| fixed/sqlite/1000/Recall | 10 | 1.43e+04 | 0 | 0 | 0 | 10 | 1.43e+04 | — |
| fixed/sqlite/1000/Consolidate | 17 | 1.87e+04 | 0 | 0 | 0 | 17 | 1.87e+04 | — |
| fixed/sqlite/1000/SourceForget | 1.11e+04 | 7.18e+06 | 0 | 0 | 3.01e+03 | 1.31e+04 | 7.66e+06 | — |
| fixed/sqlite/1000/Sweep | 3.01e+03 | 1.96e+06 | 0 | 0 | 2e+03 | 3.01e+03 | 1.96e+06 | — |
| fixed/sqlite/10000/Get | 2 | 2.86e+03 | 0 | 0 | 1 | 2 | 2.86e+03 | — |
| fixed/sqlite/10000/Recall | 10 | 1.43e+04 | 0 | 0 | 0 | 10 | 1.43e+04 | — |
| fixed/sqlite/10000/Consolidate | 17 | 1.87e+04 | 0 | 0 | 0 | 17 | 1.87e+04 | — |
| fixed/sqlite/100000/Get | 2 | 2.86e+03 | 0 | 0 | 1 | 2 | 2.86e+03 | — |
| fixed/sqlite/100000/Recall | 10 | 1.43e+04 | 0 | 0 | 0 | 10 | 1.43e+04 | — |
| fixed/sqlite/100000/Consolidate | 17 | 1.87e+04 | 0 | 0 | 0 | 17 | 1.87e+04 | — |
| lineage/memory/10/Get | 32 | 4.65e+04 | 7 | — | — | — | — | 0 |
| lineage/memory/10/Recall | 150 | 2.18e+05 | 0 | — | — | — | — | 0 |
| lineage/memory/10/Consolidate | 97 | 1.37e+05 | 0 | — | — | — | — | 0 |
| lineage/memory/10/SourceForget | 207 | 1.2e+05 | 384 | — | — | — | — | 0 |
| lineage/memory/10/Sweep | 43 | 2.65e+04 | 101 | — | — | — | — | 0 |
| lineage/memory/100/Get | 302 | 4.4e+05 | 13 | — | — | — | — | 0 |
| lineage/memory/100/Recall | 1.41e+03 | 2.06e+06 | 0 | — | — | — | — | 0 |
| lineage/memory/100/Consolidate | 817 | 1.21e+06 | 0 | — | — | — | — | 0 |
| lineage/memory/100/SourceForget | 1.75e+03 | 1.1e+06 | 4.36e+03 | — | — | — | — | 0 |
| lineage/memory/100/Sweep | 313 | 2.09e+05 | 1.14e+03 | — | — | — | — | 0 |
| lineage/memory/1000/Get | 3e+03 | 4.38e+06 | 19 | — | — | — | — | 0 |
| lineage/memory/1000/Recall | 1.4e+04 | 2.05e+07 | 0 | — | — | — | — | 0 |
| lineage/memory/1000/Consolidate | 8.02e+03 | 1.19e+07 | 0 | — | — | — | — | 0 |
| lineage/memory/1000/SourceForget | 1.72e+04 | 1.17e+07 | 5.59e+04 | — | — | — | — | 0 |
| lineage/memory/1000/Sweep | 3.01e+03 | 2.04e+06 | 1.39e+04 | — | — | — | — | 0 |
| lineage/sqlite/10/Get | 32 | 4.65e+04 | 0 | — | — | — | — | 33 |
| lineage/sqlite/10/Recall | 150 | 2.18e+05 | 0 | — | — | — | — | 150 |
| lineage/sqlite/10/Consolidate | 97 | 1.37e+05 | 0 | — | — | — | — | 97 |
| lineage/sqlite/10/SourceForget | 207 | 1.2e+05 | 0 | — | — | — | — | 353 |
| lineage/sqlite/10/Sweep | 43 | 2.65e+04 | 0 | — | — | — | — | 67 |
| lineage/sqlite/100/Get | 302 | 4.4e+05 | 0 | — | — | — | — | 303 |
| lineage/sqlite/100/Recall | 1.41e+03 | 2.06e+06 | 0 | — | — | — | — | 1.41e+03 |
| lineage/sqlite/100/Consolidate | 817 | 1.21e+06 | 0 | — | — | — | — | 817 |
| lineage/sqlite/100/SourceForget | 1.75e+03 | 1.1e+06 | 0 | — | — | — | — | 3.07e+03 |
| lineage/sqlite/100/Sweep | 313 | 2.09e+05 | 0 | — | — | — | — | 517 |
| lineage/sqlite/1000/Get | 3e+03 | 4.38e+06 | 0 | — | — | — | — | 3e+03 |
| lineage/sqlite/1000/Recall | 1.4e+04 | 2.05e+07 | 0 | — | — | — | — | 1.4e+04 |
| lineage/sqlite/1000/Consolidate | 8.02e+03 | 1.19e+07 | 0 | — | — | — | — | 8.02e+03 |
| lineage/sqlite/1000/SourceForget | 1.72e+04 | 1.17e+07 | 0 | — | — | — | — | 3.03e+04 |
| lineage/sqlite/1000/Sweep | 3.01e+03 | 2.04e+06 | 0 | — | — | — | — | 5.02e+03 |

## Callback contention

Each repetition records 20 pairs of writes to independent scopes B and C while a ranker callback is held in scope A. Both harnesses release it 50ms after callback entry. The write pair is timed; waiting for callback completion and store setup/close are excluded. Table values are medians of the three repetition-level p50/p95 measurements, not pooled quantiles of 60 observations. SQLite still serializes physical writers; the comparison concerns callback-held scope fencing.

| Adapter | Before p50 ms | After p50 ms | Before p95 ms | After p95 ms |
| --- | --- | --- | --- | --- |
| memory | 51.6 | 0.00854 | 52.3 | 0.0167 |
| sqlite | 55.2 | 0.659 | 59.8 | 0.846 |

Local timing depends on uncontrolled workstation activity. Structural tests and query bounds support bounded-work claims; descriptive timing is neither a CI threshold nor a throughput guarantee.
