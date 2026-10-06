# Task03 independent completeness review

Reviewer: independent completeness subagent; no implementation changes.
Status: FINAL ACCEPTED, 9/9 evidence groups verified = **100%**. This is an
acceptance-evidence percentage, not a claim about retrieval relevance. The last
group includes the final whole-suite race result on the reviewed snapshot.

## Requirement → evidence → status

| Group | Requirement | Evidence examined | Status |
|---|---|---|---|
| 1 | Separate availability/visibility, truncation, canonical filtering, ranking omissions and output omissions; clear breaking change | `docs/design.md` retrieval contract; `RecallProgress`, `Coverage`, removed `RecallResult.Complete`; `docs/migration.md`; `TestRetrievalBudgetProgressSeparatesCandidateStages` | verified |
| 2 | Typed evidence and fusion before deduplication; permutation invariance, duplicate control, deterministic ties, independent score scales | `SearchSignal`, `Ranked.Signals`; `reference.Composite`, identity-keyed weights, distinct per-backend ranks, explicit RRF K; `TestCompositeRRFPermutationDuplicatesAndScales`, `TestCompositeWeightsAndGlobalBound`; core ranker retains original signals | verified |
| 3 | Partial failures, minimum visibility, all-unavailable error and cancellation preserve their separate guarantees | `Composite.Search`, `recallCoverage`; `TestCompositeDegradedMinimumAndCancellation`, `TestCompositeMinimumSatisfiedWithOtherPendingEntries`, `TestRetrievalBudgetUnavailableCoverageCannotReportAbsence`; existing `TestCancellationDuringVisibilityWaitPreservesCanonicalCommit`; conformance minimum/cancel profile | verified |
| 4 | Candidate-return bound is passed to adapters and checked before expensive canonical work; unsupported/overflow behavior explicit | Mandatory `SearchOptions.MaxCandidates`, capability gate, overflow before validation/FencedView; `TestRetrievalBudgetRejectsMetadataBeforeCanonicalDecode` checks zero decoded documents for 18 malformed/oversized inputs; `TestRetrievalBudgetRequiresBoundedPortBeforeSearch`; conformance candidate-bound profile | verified |
| 5 | One optional consumer-owned budget path; final representation, exact/estimated units, large records, zero cost, uint overflow, ordering, cancellation | `RecallProjected`, `OutputPolicy`, detached codec inputs, identity-only `OutputSelection`, final `Measure`; uint64 compare; `reference.JSONPacking` whole-body measurement and oversized handling; `TestRecallProjectedCostBoundaries`, `TestRecallProjectedCapturesRequestedBudget`, all `TestJSONPacking*` | verified |
| 6 | Preserve canonical payload and provenance/conflict/uncertainty/expiry; reject injected foreign revisions; fresh canonical delivery after callbacks | Shared `finishProjection`, exact-ref `projectionRecord`, all projected refs including omitted refs checked after policy; `TestRecallProjectedPolicyMutationIsolation`, `TestRecallProjectedRejectsInvalidSelection`, `TestRecallProjectedUsesExactHistoricalRevision`, `TestRecallProjectedRevalidatesAfterPolicyCallbacks` on both stores and both phases, synchronized `TestRecallProjectedPolicyAllowsConcurrentForget`; `TestRetrievalBudgetRankerCannotReplaceSignalsOrAuthorizedReferences` | verified |
| 7 | BYOT with two payload/query types including nontext query; algorithms outside lifecycle core; no required message/embedding/vendor model | Lifecycle/quality preference payload with string query vs retrieval Incident payload with structured Query; generic `Search[Q]`, `Projector[P,R,O]`, `OutputPolicy[O,R]`, output/source codecs; fusion/JSON packing in reference; documented sparse/dense port replacement; no vendor dependency introduced | verified |
| 8 | Runnable offline example shows divergent backends, fusion, stale candidate, partial failure, omission reasons, retained metadata and strict final body; migration/conformance updated | Independently ran `go run ./examples/retrieval`: RRF keeps both signals, one stale filtered, truncation true; final body 1183/1183 bytes, budget/oversized reasons; partial failure unavailable coverage with 485-byte body; fixture explicitly disclaims production quality/billing measurement. `README.md`, `docs/migration.md`, `conformance/search.go` | verified |
| 9 | Final whole-suite race/vet/format/example gate on final snapshot and independent acceptance evidence | `final-checks.json`: vet, lifecycle, quality, retrieval, diff-check and formatting all exit0. `final-race.txt` and `final-race-result.json`: whole suite race PASS exit0, root682.555s, all packages pass; final110-entry source SHA256 manifest independently matched with zero mismatches | verified |

## Independent checks

- `GOCACHE=/private/tmp/memy-task03-completeness-cache go test -count=1 -timeout=3m ./reference -run 'Test(Composite|Index|JSONPacking)'`: pass, 0.419s.
- Same cache, `go test -count=1 -timeout=3m . -run 'Test(RecallProjected|RecallCandidate|RecallSearch|RecallProgress|RecallMalformed)'`: pass, 1.016s; relevant matched family is RecallProjected.
- Same cache, `go test -count=1 -timeout=3m . -run 'TestRetrievalBudget'`: pass, 0.522s.
- Same cache, `go run ./examples/retrieval`: pass; serialized size independently matches the out-of-band budget receipt.

No missing task03 implementation requirement found. All nine evidence groups
are verified on the final snapshot. Final whole-suite race includes the existing
10,000-node graph regression with unchanged input/assertions. Task03 is accepted
for completeness100%. Earlier interrupted runs are not passing evidence.

## Reviewed fixes after preliminary snapshot

The correctness reviewer found three production/fixture issues: unavailable
coverage with minimum precedence, child cancellation without coverage, and stale
projection state after an eligible state transition. Final code checks overall
availability before minimum, preserves child cancellation/deadline before result
validation, and captures each projected revision's state. Final revalidation
rejects changed state with ErrStaleInput for selected and omitted identities;
it does not alter an already measured output body. The corrected fixture uses
identity-sorted coverage. New regression cases cover all of these branches.

Independent fresh targeted race after those fixes:
`GOCACHE=/private/tmp/memy-task03-completeness-cache go test -race -count=1 -timeout=3m . ./reference -run 'Test(RetrievalBudget|RecallProjected|Composite|Index|JSONPacking)'`
PASS: root5.322s, reference1.170s. The changed-state test exercises eight
memory/sqlite × Select/Measure × selected/omitted combinations. This closes the
examined implementation gaps; final whole-suite gate subsequently passed.

A subsequent correctness finding was also reviewed: Composite now preserves an
existing child signal's raw Score instead of overwriting it with Candidate.Score;
Candidate.Score remains the fallback when the child supplied no signal. Distinct
list ranks are intentionally reassigned after duplicate removal. New
`TestCompositePreservesProvidedRawScoreWithDistinctRanks` covers both signals,
duplicate handling and backend permutation. Independent fresh reference targeted
race passed1.373s. The updated110-entry `source-hashes.json` was independently
compared to files with SHA256: zero mismatches. Interrupted whole-suite runs are
explicitly not counted as final passing evidence.

## Final snapshot confirmation

Independently read final whole-suite logs and result metadata, final check exit
codes and empty formatting output, then recomputed all110 source SHA256 entries:
zero mismatches. The full final race ran after all reviewed fixes. Own targeted
checks above and final evidence together cover the task's requirements; no
unverified requirement is included in the100% verdict.

Final status-only documentation update independently checked: task03 acceptance
heading/intro now say accepted, all eight RT03 rows are verified and review links
are present; local task03 checkboxes/status match accepted implementation. Updated
110-entry manifest again has zero SHA256 mismatches. No Go/contract change follows
the final passing checks. Final approval remains completeness100%.
