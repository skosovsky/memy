# Task02 performance method

The comparison uses the task01 commit `9f7b50fa354e6fd74a9def6886850bf50d3aec78` as production baseline. Historical baseline sources and raw observations are preserved. The final matrix must contain every case below, without substituting incomplete maintenance calls for completed operations. Report generation requires terminal PASS output and exactly three samples per case.

Environment: Apple M1 Max, darwin/arm64, macOS 27.0.1 (26A434), Go 1.27.1, default GOMAXPROCS 10. RAM size was unavailable through sandbox sysctl. Other workstation activity is uncontrolled. Runs use separate fresh fixture stores, not a controlled dedicated performance host; timings are descriptive and do not establish CI thresholds.

## Corpus and timing

The fixed corpus contains 1k, 10k or 100k independent active records, one revision each, a fixed 128-byte payload value and a shared live source. Get addresses one record; Recall returns two fixed candidates; Consolidate consumes the same two exact revision refs. SourceForget selects every corpus record. Sweep visits healthy, unexpired records and checks current retention. Privileged fixture setup clones a normally committed canonical envelope and, in the final implementation, builds matching membership indexes. Setup, database open and Close are excluded from measured time and allocation counters. This corpus measures storage/lifecycle work, not host ingestion or proposal-history growth.

The lineage corpus contains 10, 100 or 1000 independent parent records plus two roots, each referencing all those parents. Payload size stays fixed. It exercises the same five operations, with explicit 16 MiB consolidation input/output budgets. SourceForget asserts that all n+2 records were purged. This is growing fan-in; long chains, cycles and historical branches are covered by separate native graph correctness tests, not inferred from this benchmark.

The fixed and lineage corpora each contribute 30 cases: two adapters × three sizes × five operations. SQLite reopen Get adds three cases, for 63 total. Reopen means closing the prepared database and opening a fresh handle before timing Get. The OS page cache remains warm; this is handle-cold and makes no physical disk-cold claim. Other cases are warm operations immediately following setup.

Each of those 63 cases uses `-benchtime=1x -count=3`. The report gives medians of completed observations and the completed count. Three isolated operations are insufficient to infer tail latency, throughput, statistical significance or a stable latency distribution. Fixed-harness p50/p95 labels from a single iteration are not tail evidence and are not promoted as such in the comparison.

Fixed-corpus operations receive a five-second context deadline. `censored/op=1` means cancellation interrupted the operation. Legacy loops may observe the deadline much later; the recorded duration includes that delay. Censored latency and allocation values remain visible separately. They are excluded from successful medians, and ratios are calculated only when all three observations completed on both sides. Growing-lineage and reopen cases do not use this censoring deadline; they must return successfully or the Go benchmark fails.

## Callback contention

The contention matrix uses `-benchtime=20x -count=3`, separately for memory and SQLite. A trusted ranker signals entry and remains held until a timer releases it 50ms after entry. During that interval, two writes commit sequentially to independent scopes B and C. The timer condition is identical before and after; the timed quantity is the pair of writes, not callback completion. Setup, waiting for callback completion and Close are excluded.

Each repetition reports its 20-pair p50 and p95 using the harness's sorted indices; the report shows medians of the three reported quantiles, not pooled 60-observation quantiles. SQLite continues to serialize physical writers. These observations examine whether a held callback retains a fence that blocks unrelated scope writes; structural concurrency tests establish ordering without relying on wall-clock thresholds.

## Counters and provenance

The baseline fixed harness counts public Get requests, listed entries and returned bytes. The final harness retains the historical `listed-entries/op` label for entries returned by Scan. Public request counts alone cannot prove bounded adapter work.

Final opt-in `internal/workcost` counters record canonical decodeDocument calls and their input bytes, detached memory Get/Scan copies, AVL range nodes visited, SQLite metadata rows returned by bounded scan SQL, and value rows/bytes returned by exact Get SQL. Lineage reports the sum of returned SQL metadata/value rows. These are not SQLite VM steps, rows internally inspected by the optimizer, cache misses, physical I/O, or every JSON call inside host code. Inactive counters only observe a nil atomic pointer; active sessions are serialized and increments atomic. No public observer port is introduced. Missing baseline private counters mean unavailable, never zero.

`build_performance_report.py` reads baseline-raw.txt plus before-extended-raw.txt, and final-after-raw.txt with all SourceForget/Sweep cases replaced by final-affected-raw.txt. It requires all 24 replacement cases, each with three observations. This replacement reflects lifecycle validation changes after the original final-matrix binary was compiled. Parent-maintained source hashes and verification artifacts identify which production and harness state each run measured; raw input hashes are embedded in performance-summary.json. The generator also checks the two contention matrices have three 20-iteration samples and a 50ms callback hold.

Run the generator only after final raw files are saved:

```sh
python3 docs/reviews/task02/build_performance_report.py
```

The output is performance-report.md and performance-summary.json. Generation does not run tests or benchmarks, certify source identity, or replace independent final acceptance. Stage measurements remain historical evidence and are not merged into the final comparison.

Production provenance: `final-measurement-source.json` identifies the original
63-case matrix; `final-corrected-source.json` identifies the corrected production,
schemas and harness used by the replacement cases, contention and final checks.
`final-measurement-delta.json` shows that only purge_state.go production code
changed between the two matrices. Its readPurge/persistSinkResult callers belong
to Forget/Sweep; the 39 retained cases do not execute them. The added regression
is excluded by `-run '^$'`. Harness bytes are retained in final-corrected-*.txt.
