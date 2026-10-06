# Remediation acceptance — 2026-10-06

Scope: F01–F10 and explicit decisions D01–D70 from the review of
`e12c8fc0d6fc8346e7b67e5a6ecce4c569b932a2`. The ten implementation stages run
sequentially; each requires separate completeness and correctness acceptance,
then a short local commit. No push or real release is part of this work.
Historical 85/85 reports remain scoped to their original frozen manifests.

## Delivered stages

| Stage | Change | Commit | Independent acceptance |
|---|---|---|---|
| 1 | Contract and migration decisions before source changes | e0dd446 | 100%; ACCEPT |
| 2 | Isolated exact-tag release and failure recovery | 4454c69 | 100%; ACCEPT |
| 3 | Exact generic numbers and strict Unicode | 319fdfe | 100%; ACCEPT |
| 4 | ExactDedup expiry follows its actual lineage | f60f78b | 100%; ACCEPT |
| 5 | Bounded authenticated cursor v2 | 2649735 | 100%; ACCEPT |
| 6 | Effective snapshot projection and cache identity | 6a8494d | 100%; ACCEPT |
| 7 | Confirmed partial Sweep progress on errors | 7fe9b5d | 100%; ACCEPT |
| 8 | Applied quality boundaries and conformance | bd1212d | 100%; ACCEPT |
| 9 | All 70 decisions, admission/schema/API cleanup | 2a04dc9 | 100%; ACCEPT |
| 10 | Docs, migration, examples, final validation | This document's commit | 100%; ACCEPT |

[Contracts](../remediation-contracts.md), [decisions](../remediation-decisions.md),
[API recovery](../api-recovery.md), [migration](../migration.md) and
[baseline runtime evidence](remediation-baseline.md) record the admitted domain,
identity breaks, recovery rules and reproduced old behavior. Regression tests
cover number/Unicode/ownership, lineage retention, cursor boundaries, effective
current/historical projections, rollback/unknown outcomes, partial progress,
actual grader/budget execution and isolated local Git publication. The feature
spec in ai-libs is synchronized separately; it is outside this repository.

## Final verification

Fresh `make validate > /tmp/memy-final-validation.log 2>&1` completed with exit 0.
Actual test invocation was independently inspectable as
`go test -v -race -count=1 -timeout=30m ./...`; no cached pass was substituted.
Format and vet passed, strict lint reported 0 issues. Root race suite passed in
590.823s; internal/quality 75.869s; SQLite 3.493s; KV 2.289s; all other tested
packages passed. Four executable examples and all 10 local-only release
fixtures passed (16.006s). Codec/number/identity/cursor fuzz seed cases and
executable schemas/conformance ran within the fresh suite; this is seed
validation, not a long fuzz campaign.

[Full terminal output](remediation-validation.txt) preserves all package,
example and local-release results. The earlier attempt stopped on two lint
issues in the new example before tests; both were fixed, and the fresh complete
rerun above is authoritative. Separate reviewers independently ran lifecycle,
schema/race and touched fuzz-seed checks and reproduced the quality CLI report
byte-identically.

[Source snapshot](remediation-source-hashes.json) binds 189 source/non-review
documentation files, aggregate SHA256
`c31215356383f16bfa6854cff615cdf1736c1f9efa5b2fefc4d637b679aa97ca`.
Both reviewers independently verified every hash/byte count and the external
feature spec. Review/evidence files and local task metadata are excluded to
permit terminal result recording. The external feature spec is saved outside
this commit; ai-libs is not a Git repository.

Independent final [completeness](remediation-completeness.md): **100% (8/8 DoD)**;
separate coverage lenses F01–F10 10/10, D01–D70 70/70 and documentation 9/9.
Independent final [correctness](remediation-correctness.md): **ACCEPT**, no open
confirmed errors. Reviewers did not read each other's report before their verdicts.
The historical quality-evidence attribution issue was fixed to `e68eaf3`, and
both reviewers independently verified all 127 frozen historical hashes afterward.
The final local commit contains this report and both terminal acceptances.

Measured optimization decisions are in [cost evidence](remediation-costs.md).
The 10,000-candidate packing sample is censored at its deadline and is not a
completed-selection timing or peak-memory measurement.

## Limits

Local Go 1.27.1 darwin/arm64 with CGO, temporary SQLite databases and scripted
synthetic providers. No live LLM calls, remote release, Linux execution,
long-duration fuzz campaign, remote backend durability, production quality or
forensic erasure is certified. Unknown commit outcomes do not become known
success or exactly-once progress. Independent acceptance means no open confirmed
error within the stated scope, not absence of all unknown bugs.
