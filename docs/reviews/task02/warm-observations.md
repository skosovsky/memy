# Warm corpus stage observations

All 30 cases have three samples. Censored cases did not complete within the
five-second context and are excluded from completed latency statistics. Setup
and close are outside timing. This is stage evidence, not task acceptance.

| Adapter / revisions / operation | Completed samples | Before median ms | After median ms | Before MiB/op | After MiB/op | Decoded docs |
| --- | --- | --- | --- | --- | --- | --- |
| memory/1000/Get | 3/3 | 3.407 | 0.562 | 3.588 | 0.395 | 2.0 |
| memory/1000/Recall | 3/3 | 8.350 | 2.173 | 4.343 | 1.948 | 10.0 |
| memory/1000/Consolidate | 3/3 | 696.015 | 3.065 | 226.863 | 2.594 | 17.0 |
| memory/1000/SourceForget | 3/3 | 1261.621 | 851.473 | 565.953 | 778.700 | 11119.0 |
| memory/1000/Sweep | 3/3 | 500.594 | 217.861 | 180.702 | 197.936 | 3007.0 |
| memory/10000/Get | 3/3 | 25.098 | 0.583 | 31.769 | 0.406 | 2.0 |
| memory/10000/Recall | 3/3 | 26.375 | 2.096 | 32.242 | 1.948 | 10.0 |
| memory/10000/Consolidate | 3/3 | censored | 2.379 | censored | 2.589 | 17.0 |
| memory/10000/SourceForget | 0/3 | censored | censored | censored | censored | censored |
| memory/10000/Sweep | 3/3 | 2738.165 | 1955.633 | 1802.161 | 1970.227 | 30007.0 |
| memory/100000/Get | 3/3 | 164.618 | 0.672 | 310.442 | 0.395 | 2.0 |
| memory/100000/Recall | 3/3 | 327.340 | 2.264 | 308.173 | 1.948 | 10.0 |
| memory/100000/Consolidate | 3/3 | censored | 2.771 | censored | 2.590 | 17.0 |
| memory/100000/SourceForget | 0/3 | censored | censored | censored | censored | censored |
| memory/100000/Sweep | 0/3 | censored | censored | censored | censored | censored |
| sqlite/1000/Get | 3/3 | 14.913 | 1.103 | 10.228 | 0.418 | 2.0 |
| sqlite/1000/Recall | 3/3 | 13.208 | 3.579 | 10.982 | 2.027 | 10.0 |
| sqlite/1000/Consolidate | 3/3 | 343.861 | 5.465 | 253.421 | 2.706 | 17.0 |
| sqlite/1000/SourceForget | 3/3 | 1040.773 | 1495.746 | 578.959 | 811.062 | 11119.0 |
| sqlite/1000/Sweep | 3/3 | 223.707 | 1220.189 | 194.033 | 210.784 | 3007.0 |
| sqlite/10000/Get | 3/3 | 96.623 | 1.071 | 97.229 | 0.419 | 2.0 |
| sqlite/10000/Recall | 3/3 | 95.712 | 4.268 | 97.702 | 2.027 | 10.0 |
| sqlite/10000/Consolidate | 3/3 | 3155.791 | 5.725 | 2515.289 | 2.713 | 17.0 |
| sqlite/10000/SourceForget | 0/3 | censored | censored | censored | censored | censored |
| sqlite/10000/Sweep | 0/3 | 2953.252 | censored | 1933.088 | censored | censored |
| sqlite/100000/Get | 3/3 | 3639.378 | 1.161 | 957.975 | 0.413 | 2.0 |
| sqlite/100000/Recall | 3/3 | 2195.894 | 4.107 | 955.707 | 2.027 | 10.0 |
| sqlite/100000/Consolidate | 3/3 | censored | 6.023 | censored | 2.693 | 17.0 |
| sqlite/100000/SourceForget | 0/3 | censored | censored | censored | censored | censored |
| sqlite/100000/Sweep | 0/3 | censored | censored | censored | censored | censored |

Private counters show constant addressed work for Get/Recall/Consolidate across
scope sizes. Get decodes two canonical documents; the added guards issue exact
absent-pointer lookups. The full Sweep pass currently opens a short transaction
for each revision and persists its durable suffix. This raises its complete-pass
cost, especially SQLite: 10k/100k Sweep samples are censored. It preserves bounded
per-call work and timely cancellation, but is not a full-pass throughput improvement.

The root decoder still performs strict shape/canonical JSON validation. The
benchmark describes its actual cost; it does not replace correctness validation
with an unsafe fast path. Indexed iteration is bounded; decoded blob sizes and
required lineage remain input-dependent. SQLite physically serializes writes and
has no distributed-throughput claim.

Three completed samples support descriptive medians only; they do not establish
a reliable p95 tail. Separate extended and contention measurements remain pending.
