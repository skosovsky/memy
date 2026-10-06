# Private work measurements

This is stage evidence, not independent task acceptance. Lifecycle warm samples
completed with three repetitions of each memory/SQLite, 1k/10k/100k, five-operation
case. Deadline-censored samples remain incomplete observations. The performance
report must not treat them as successful completion or include them in completed
operation latency quantiles.

Opt-in internal/workcost counters measure decodeDocument invocations and input
bytes, actual detached memory Get/Scan copies, AVL range nodes visited, SQLite
metadata rows returned by bounded scan SQL and value rows/bytes returned by exact
Get SQL. No public Engine observer/host port was added. The process-wide session
is explicitly serialized; increments are atomic for callback contention work.
Inactive production calls only observe a nil atomic pointer. These counts do not
claim SQLite VM steps, page-cache misses, disk I/O, or all JSON calls inside host
code. SQL query plans and indexed bounds substantiate the row-selection contract.

TestAddressedOperationsDoNotDecodeOrCopyUnrelatedHistory compares 1k and 10k
fixtures for Get and Consolidate on both adapters. It requires equal canonical
decode count/bytes and exact value copy/read counts, with a structural AVL seek
bound and at most two SQLite scan metadata rows. It passed. This verifies private
addressed work, beyond the historical public request counters.

The growing-lineage fixture adds 10/100/1000 canonical reconciliation parent refs
to each of two selected roots. Parents are independent with fixed 128-byte values;
canonical membership indexes are updated consistently through privileged fixture
setup. It measures all five operations and asserts successful source deletion of
every fixture record. Large InputBytes/OutputBytes budgets are explicit so a host
budget rejection is not mistaken for lineage performance. This is fan-in growth,
not evidence of long-chain or historical-branch purge correctness.

SQLite reopen Get closes the prepared database and opens a fresh handle before
timing the addressed read. The OS cache remains warm: this is handle-cold, not
physical disk-cold. Memory has no reopen/persistent-cache claim. Callback contention
holds a trusted ranker by channels while independent scopes B and C commit writes,
then maintains the hold for an additional 50ms before release. Report independent
write latency distribution separately from callback delay; structural race tests
establish ordering instead of imposing a noisy wall-clock CI threshold.

Each benchmark with benchtime=1x reports only one observed latency; its p50/p95
labels are equal and do not establish a tail distribution. Final aggregation must
use repeated raw samples and state sample count, with small-sample limitations.
The dedicated contention run should use a larger fixed repetition count.

The warm run is terminal and preserved in warm-raw.txt (90 samples / 30 cases).
The raw baseline is unchanged. warm-summary.json and warm-observations.md exclude
21 deadline-censored observations from successful medians. The launch harness is
preserved in final-warm-harness.go.txt; its seed helper accepted *testing.B.
Subsequently that helper was widened to testing.TB for structural/native graph
fixtures, without changing measured production operations. warm-source-hashes.json
records the launch harness and production instrumentation files. Extended growing
lineage/reopen samples are now running, with three repetitions and complete-purge
assertions; callback contention and fresh race verification follow them.
