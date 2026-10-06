# Task04 independent completeness acceptance

Reviewer: `/root/task04_completeness`, independent of implementation agents.
Snapshot: frozen final source/artifact hashes in `source-hashes.json` (127 files), independently recomputed after completed whole-repository validation; no drift.

**Accepted: 100% (10 of 10 acceptance rows fully evidenced).**
Rows are equally weighted. All required work items, scenario groups and validation gates are evidenced; no completeness item remains open.

## Acceptance matrix

| Row | Evidence inspected and independently exercised | Status |
| --- | --- | --- |
| QH04-01 | `corpus.go`: closed registered plans, strict decoder, versions/groups/positive bounded budgets. `quality.go`: repeat dispatch and derived seed. Actual deterministic reruns and varying insertion/payload inputs tested. | Complete |
| QH04-02 | Six report stages and final aggregation in `report.go`; ordinary lifecycle apply/reject in preference suite. Semantic candidate quality fails descriptively while explicit rejection and actual unchanged baseline pass. Exact payload/revision comparisons after apply. | Complete |
| QH04-03 | Consumer-owned `Preference` plus `ProcedureObservation`, typed `DocumentRef`, `ProcedureInput`, `ProcedureQuery`; no added root evaluation type/dependency. Final independent compiled CLI executes 16 plans × 2 repeats = 32 scenarios. All eight groups are covered. | Complete |
| QH04-04 | Actual valid/recorded time, current historical revoke/retention, pending purge/retry, late-write callback denial, stale index and durable source revoke checked. Actual second tenant in the same store: own-principal foreign Get/Snapshot denied, unauthorized candidate filtered from Recall and RecallProjected; independent exact foreign snapshot preserved before and after memory/SQLite Forget. | Complete |
| QH04-05 | Procedure search relevance/distractors, duplicated backend hits, stale/unseen revisions and bounded result; real RecallProjected full-body JSON packing selects one exact revision with an explicit omission. Metrics distinguish payload/context and explicitly unavailable storage/real cost. | Complete |
| QH04-06 | Guarded/permissive fixture host versions; false instruction-bearing knowledge is rejected/accepted through ordinary lifecycle, preserved as Trust=data, cannot grant intruder read authority. No dispatcher exists. Permissive factual failure remains visible. | Complete |
| QH04-07 | Real baseline observations and separate oracle; detached mutations preserve count but alter payload/revision/lineage; late actual Forget leaves retained delivered context as a detected leak. Actual outage independently invalidates fabricated healthy-empty. Independent compiled CLI exits 1 for all five known-negative probes. | Complete |
| QH04-08 | Mandatory evidence/check IDs enforced; unknown precedence, checker/adapter error probes exit 2, provider/grader failures and fixed ErrorClass diagnostics; marker tests cover nested reports and raw errors. No private payload/reference/query/error printed. | Complete |
| QH04-09 | Compiled subprocess exit 0 for normal corpus (including expected semantic rejection), 1 for five known-negative probes, 2 for checker/adapter errors. CLI tests prove saved JSON before exit. Independently ran quality-integration: pass with custom typed extractor/search/grader and unchanged exact/canonical/full-context oracles. | Complete |
| QH04-10 | Current quality/design/migration docs and executable v2 schemas inspected. Historical acceptance references identify current counterparts and explicitly retain only historical statuses. Saved v2 report and byte-identical independent rerun, source hashes, executable schemas, vet/format/diff/examples and targeted race are verified. Full repository race completed successfully on this same snapshot. | Complete |

## Required work items and scenario groups

Work items 1–7 are implemented and evidenced, including work item 2's real foreign-scope invariant.
Work item 8 has versioned corpus/report shapes, actual versions/configuration/seed/repeats and honest measurement boundaries; its final saved artifact is regenerated and independently reproduced.

| Group | Actual plan and oracle evidence | Status |
| --- | --- | --- |
| Sessions/corrections | `preference-sessions`: fresh engine, corrected accepted revision, old historical revision, explicit unknown/conflict options | Complete |
| Temporal | `preference-temporal`: distinct valid/recorded queries and historical reads after current revoke/expiry | Complete |
| Retrieval | `procedure-retrieval`: relevant typed query, distractor exclusion, duplicate backend rank signals, stale filtering, exact one-projection budget | Complete |
| Abstention/unavailability | `procedure-abstention`, preference/procedure controlled failures: healthy empty/conflict/unavailable distinct, error classes checked | Complete |
| Poisoning | Guarded/permissive plans: accepted false knowledge remains data and factual weakness visible | Complete |
| Forget/derived | Memory and SQLite plans: interrupted managed purge, retry, late-write denial, source revision/removal/durable revocation | Complete |
| Consolidation | Baseline/exact/domain/semantic: actual apply/reject, exact facts/negation/validity/source/lineage and unchanged original documents | Complete |
| Evaluator failures | Self-check parent with seven non-passing children plus direct negative subprocess outcomes | Complete |

## Independent verification

`GOCACHE=/private/tmp/memy-task03-go-cache go test -count=1 ./internal/quality ./examples/quality ./examples/quality-integration`
passed on the corrected implementation: quality13.842s, CLI2.878s. Checked real
foreign-scope state, independent projected healthy-empty/conflict/outage,
strict projection sets, isolated grader inputs and fresh canonical checks after
grading. Small budgets preserve safe partial observations and do not index absent
projections or label undelivered context as measured.

The typed integration example passed with extractor `integration-extractor/v1`,
search `integration-bounded-rrf/v1` and grader `integration-exact-grader/v1`;
effective/rendered/final pass. Independent compiled subprocess probes return
exit1 for wrong payload/revision, missing lineage, late privacy and false-empty
outage, and exit2 for checker/adapter errors. The normal corpus includes expected
semantic rejection and returns exit0.

The old global tea assumptions and Trial.Accepted protocol are removed without
compatibility aliases. Typed domain comparison stays fixture-owned; root gains
no evaluation types, provider SDK or classifier. Scripted knowledge/procedures
remain data; the suite makes no universal poisoning-protection or measured LLM
benefit claim. Storage and provider measurements use explicit availability and
units, and Task02 remains the performance evidence source.

## Final acceptance

All eight task work items and all eight mandatory scenario groups are complete.
The foreign-scope gap is closed by actual same-store second-tenant checks and
independent preservation assertions. No required item remains pending. No
production, test or other documentation edits were made by this reviewer.

## Frozen artifact inspection

Independently recomputed all 127 SHA256 hashes in `source-hashes.json`: no drift.
Saved `docs/quality-report.json` has the pinned manifest, 32 passing parent
scenarios, and 14 negative children (10 failed and 4 unknown). The semantic
candidate remains descriptively failed with host review/effective/canonical/
rendered/final pass. Real cross-scope and both forget adapters contain the
required passing foreign privacy checks. Projected abstention has three separate
mandatory checks (healthy empty, conflict suppression, unavailable).

Rebuilt the final CLI independently with `-buildvcs=false`, executed the default
manifest and compared stdout bytes to the saved golden: exit0 and byte-identical.
`cli-validation.json` independently inspected: 14 real process cases have expected
0/1/2 codes, saved JSON and mode0600; deterministic repeat bytes agree. The final
root targeted race artifact passes quality129.795s and CLI36.361s.
`final-checks.json` plus their outputs show vet, lifecycle/retrieval/integration
examples, `git diff --check` and gofmt all pass. Format output is empty.

The whole repository race is complete and counted: `go test -race -count=1 -timeout=20m ./...` returned exit0. `race.txt` reports root915.557s and every tested package passes, including quality128.690s, CLI35.916s, reference, memory and SQLite. `final-race-result.json` records successful process completion, 127 verified hashes and source_drift=[]. I independently recomputed those hashes after completion: all match. `validation.md` accurately records the final gates. The existing graph regression retains its input and assertions. Elapsed times are diagnostics, not performance claims.

## Final documentation delta approval

After acceptance, `docs/acceptance.md` and `docs/quality.md` changed only to
record the completed QH04 status, final validation and independent-review links.
I inspected those final texts and independently recomputed all 127 current
source-hash entries: every hash matches. The independent implementation/schema/
module/manifest snapshot also remains unchanged (121 entries). The post-gate
change inventory names only those two acceptance-documentation files.

Completeness acceptance remains **100% (10/10)** on this exact final snapshot.
No additional implementation validation is required for this status-only delta.
