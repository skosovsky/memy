# Task02 baseline protocol

Status: initial corpus measurement complete; this is not task02 acceptance.

Production baseline: commit `9f7b50fa354e6fd74a9def6886850bf50d3aec78`.
The benchmark was compiled before production task02 edits. Added test helpers
accept testing.TB; the corpus harness is storage_cost_test.go.

Command:

```
GOCACHE=/tmp/memy-go-build GOPATH=/tmp/memy-gopath go test -run '^$' -bench '^BenchmarkLifecycleCost$' -benchtime=1x -count=3 -timeout=30m .
```

Environment: Apple M1 Max, darwin/arm64, macOS 27.0.1 (26A434), Go 1.27.1,
GOMAXPROCS default 10. Exact RAM size was not available from sandbox sysctl;
no memory-size claim is made. Other workstation activity is not controlled.
Samples are descriptive and never a CI wall-clock threshold.

Corpus: 1k/10k/100k independent active revisions, one revision per record,
fixed 128-byte payload value, shared live source, fixed two retrieval candidates
and two exact consolidation inputs. Fixture construction clones one normally
committed envelope through privileged Store access in one setup transaction.
This models canonical read/history cost; it does not measure host ingestion or
proposal history growth. Get/Recall/Consolidate all consume the same corpus.
SourceForget selects all corpus records. Sweep visits healthy nonexpired
revisions and checks current retention without generating artificial purges.

Each sample creates a fresh database/store before the timer, then measures one
warm operation with the corpus resident from setup. It records allocation bytes,
allocation count, elapsed time and the Engine's public Get requests, listed
entries and returned bytes. These counters are not private adapter SQL rows or
framework JSON decode counts. Exact row/decode instrumentation, growing lineage,
multi-scope callback contention and cold reopen measurements remain required
before final task02 acceptance.

Operation contexts expire after five seconds. `censored/op=1` means the operation
was interrupted, not completed in the displayed time. Legacy synchronous loops
can observe cancellation much later than the deadline: the measured interval
includes that delay. Setup and Close are excluded. Raw samples are preserved
without converting censored observations into successful latency percentiles.

Initial structural evidence: memory Get requests six keys and lists one matching
revision at all three corpus sizes, yet allocates approximately 3.8 MB, 33.3 MB
and 325.5 MB per operation. Thus stable public request counts alone do not prove
bounded adapter work. Consolidation on 100k revisions enumerates all 100k and
is censored. SourceForget on 100k observed its five-second cancellation only
after about 152 seconds and allocated about 18.7 GB in its first sample.

The complete raw output is baseline-raw.txt. It contains three samples per
operation/corpus/adapter and preserves censored observations. The original
harness is baseline-harness.go.txt; subsequent scan API edits do not rewrite
this historical source snapshot. Initial post-addressing diagnostics are in
addressed-interim-raw.txt and are not the final task02 performance report.
