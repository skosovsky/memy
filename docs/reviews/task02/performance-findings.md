# Task02 interpretation

Addressed reads no longer allocate in proportion to unrelated scope history.
In the retained, unchanged Get paths, median allocation bytes across 1k/10k/100k
are 407,680 for memory and approximately 429–436k for SQLite. Baseline memory
allocations grew from 3.76 MB to 325.52 MB; baseline SQLite from 10.72 MB to
1,004.51 MB. Final canonical decoding remains two documents per addressed Get.
The structural 1k/10k tests also check exact bytes/value rows for fixed-input
Consolidate; descriptive timing alone is not the proof of addressed work.

Purge has a bounded continuation and durable fence, but short transactions,
semantic document validation and persistent progress are substantial work.
The five-second fixed-corpus matrix does not demonstrate completion of larger
purges or SQLite retention passes. Their interrupted samples must not be read
as complete-operation performance. The native graph and growing-lineage tests
supply separate completion evidence, while Limit=1 fan-in byte measurements
show that reused canonical roots remove repeated full child-body decoding.
They do not establish a universal latency bound for host callbacks.

Retention passes visit healthy records under current policy and persist progress.
The SQLite design pays a short write transaction per unit of maintenance work;
this can be slower than the old single full-scope read for an unexpired corpus.
The result is bounded, resumable maintenance with explicit host continuation,
not a claim that every maintenance workload is faster. SQLite physical writers
remain serialized. There is no distributed throughput or physical disk-cold claim.

Final comparison values, censored samples and contention quantiles are generated
in performance-report.md/performance-summary.json. Source provenance and method
are separate from these observations; both independent final reviews remain the
acceptance gate.
