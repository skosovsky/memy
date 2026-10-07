# Independent completeness review — pre-release

Reviewed source manifest: `3072e9ac7f7f007c3c8e84a6edaca58507e0e26046821f19623b70988643fec0` (202 files). Every listed SHA256 was independently recomputed and matched. Production/test/source files were not edited. This report reviews the pre-release implementation; it does not certify publication or closeout.

## AC1–AC10

Percentages describe evidenced completion of each requirement, not statistical confidence. Equal-weight aggregate at this review: **94.5%**. Eight criteria are implemented and evidenced; AC8 has specific missing executable coverage; AC9 lacks final validation and the published consumer lane.

| Criterion | Completion | Source/contract and evidence | Remaining work |
|---|---:|---|---|
| AC1 boundaries | 100% | `docs/design.md`, `docs/managed-projections.md`; generic core Engine/Search/Projector/OutputPolicy; host/checkpoint packages own typed query, authentication grants, serialization and SQLite/HTTP checkpoint integration. Consumer imports are confined to optional `testdata/consumer` and temporary workspace script. | No architectural violation found in reviewed scope. |
| AC2 canonical authority | 100% | `recall.go`, `recall_state.go`, `projected_recall.go`; `TestProposalAcceptanceIsolation`, `TestCanonicalReadsRevalidateSource`, `TestSourceRevalidatedAfterReadCallbacks`, projected callback revalidation cases source/authority/expiry/forget/cancel; consumer canonical fixture sends stale revision and hostile index payload and checks canonical result. `host.Seed` requires a distinct reviewer before Commit. | None identified. |
| AC3 isolation | 100% | `checkpoint.Key` structurally serializes scope, handle and sorted exact lineage; Store SQL scopes all rows. `TestCheckpointScopeIsolationAndStructuralKeys` tests identical IDs/handles across tenant/namespace/subject and ambiguous separators. Remote process fixture also tests tenant isolation. | None identified. |
| AC4 evidence | 100% | `score.go`, validations/cloning, `Projection.Retrieval`, reference RRF/ScoreRanker. `TestScorePresenceContract`, `TestScoreRankerPresenceOrder`, `TestProjectedEvidenceCanonicalAndDetached`, `TestCompositeNeverInventsNativeScore`, `TestCompositePreservesProvidedRawScoreWithDistinctRanks`, and conformance score-presence fixtures cover absent/zero/negative/nonfinite/ambiguous state, rank-only evidence and deterministic ordering. Plain Project evidence is nil. | None identified. |
| AC5 ownership/budget | 100% | Detached Retrieval/signals in projected clone, identity-only policy selection; mutation/invalid-selection/revalidation/cost-boundary fixtures. `TestProjectedUnicodeBytesAndEstimatedUnits` distinguishes UTF-8 bytes/runes and estimated units. Exact result json length includes evidence and metadata, budget receipt explicitly out of band. | None identified. |
| AC6 race | 100% | WithDerivedWrite canonical gate and checkpoint epoch fence; channel-controlled `TestForgetBetweenProjectionAndPersistence`, `TestWriteBeforeForgetAndUnknownCallbackOutcome`, existing managed race suite; late candidate/late checkpoint write tests. | None identified. |
| AC7 durable purge | 100% | SQLite transaction persists data/lineage/handle/epoch; Purge acknowledges after commit. `TestDurableMultiLineagePendingRetryRestore`, `TestPurgeAllDependentCopiesAndLateOldScopeBatch`, sink conformance, process backend tests prove multi-copy deletion after reopen, pending retry without new canonical revision, stale writes, idempotent old batches and unknown response reconciliation. | Guarantees limited to tested local SQLite and HTTP/SQLite participant contract. |
| AC8 restore | 95% | `checkpoint.ReadValidated` loads authenticated scope, obtains fresh canonical fence, validates full exact lineage inside gate and invalidates on gate failure. Executable tests reject revoked old checkpoint, source revision change and cancellation; surviving source recompile is demonstrated. Contract explicitly requires current revocation ledger when restoring canonical backup. | Add checkpoint restore fixtures for a missing canonical exact ref and unavailable canonical verification, asserting no returned bytes and appropriate invalidation/retry. Current tests do not exercise these named restore cases directly. Backup ledger requirement is documented responsibility, not an implemented ledger importer. |
| AC9 checks | 50% | Optional local suite log/manifest records eight semantic tests with GOWORK off; script published lane requires explicit changed tag and rejects every Replace in resolved graph. CI has separate local/published lanes. Explicit process/TCP/SQLite backend tests passed independently below. | `make validate` was still running (observed log is not successful final exit evidence). Published lane has not run against new artifact. Production backend selection/acceptance by issue author is unresolved. Do not claim external issue completion. |
| AC10 workstation | 100% | Three original byte copies, full diffs, manifest/checksums and per-file disposition preserved under local-duplicates; no package exclusions/build tags. Current ordinary targeted compilation passes; no alternate duplicate files remain in source packages. | None identified; full ordinary tree check is also pending under AC9 validation. |

## Independent execution

Executed successfully (exit 0):

```text
go test -race -count=1 ./examples/managed-projections/... ./reference ./conformance -run 'Test(DurableMultiLineage|CheckpointScope|ForgetBetween|WriteBefore|CheckpointSource|Remote|PurgeAll|CheckpointSink|CompositeNever|CompositePreserves|JSONPacking)'
```

Checkpoint, host and reference tests passed, including separate OS-process TCP participant kill/restart and canonical Forget receipt-loss recovery. The conformance package itself has no direct tests; its contracts execute via adapter fixture tests. This run is targeted evidence and does not substitute for all of make validate.

## Work/artifact audit

1. Contract-first docs, score value shape, executable tests and migration section exist. Persisted canonical schema is unchanged by retrieval API changes, so no schema migration is necessary for this change.
2. Optional scores, native evidence separation and projected evidence are implemented directly; no compatibility wrappers found.
3. Runnable offline example performs lifecycle, canonical projection, complete-envelope materialization, managed durable write, close/reopen, revoke/pending/retry and stale-index filtering. Reviewer grants are separate host state.
4. Checkpoint implementation and runnable example are outside core. Transactional reverse lineage and scoped deletion handles are present; no recursive canonical store call occurs in the write callback.
5. Restore behavior and bounded guarantees are documented and executable for revoke/source/cancel. Named missing/unavailable exact lineage scenarios remain coverage gaps above.
6. Consumer script uses a temporary module; local source replacements are explicit and published mode rejects replacements. Published success evidence remains outstanding.
7. Local duplicate copies/checksums/diffs and disposition are preserved. No legacy merge was needed; reason is recorded per file.
8. **Required evidence matrix is unfinished:** `docs/reviews/issue-1/matrix.md` still says implementation in progress, has only Requirement/Evidence/Status columns and marks AC1–AC9 pending. Update it to requirement → code/contract → test → result, with actual final commands/results and remaining gates. This is mandatory acceptance evidence, independent of implementation percentage.
9. AC11 is ongoing: this review is one half of independent acceptance of a pre-release snapshot; correctness report and any findings must be reconciled, affected checks rerun and both reviewers recheck final snapshot.
10. AC12 release/closeout is not done. Substantial score/evidence API change requires `make release-break`, then new artifact published consumer verification. Author migration comment must cover scores/evidence, authenticated scope, sink registration, fence/full lineage synchronous writes, handles/pending retries and checkpoint invalidation, with links and actual before/after API. Issue must remain open until agreed external boundary is verified or explicitly accepted by author.

## Actionable gaps

- Complete requirement-by-requirement matrix with source/test/result evidence.
- Add the missing/unavailable canonical-lineage restore fixtures described under AC8.
- Obtain successful terminal make validate evidence on final source snapshot.
- Complete release-break and changed-artifact published consumer lane; review published graph and semantic test results.
- Resolve author agreement on production backend/external scope before claiming all external requirements closed. The process HTTP/SQLite integration is real participant evidence, not a mock, but cannot certify arbitrary production/HA/distributed storage.
- Finish independent final snapshot acceptance and explicit migration communication before issue closure.

No confirmed architectural boundary violation was found. This is a bounded completeness review and makes no mathematical error-free claim.
