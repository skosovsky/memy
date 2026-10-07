# Independent completeness review — final implementation snapshot

Reviewed source manifest: `5d517d4d29900935f1ead6d841e8c46d347abeb80622843b247a176e4cedb729` (202 files). Every listed SHA256 was independently recomputed and matched. Production/test/source files were not edited. This report reviews the pre-release implementation; it does not certify publication or closeout.

## AC1–AC10

Percentages describe evidenced completion of each requirement, not statistical confidence. Equal-weight aggregate at this review: **95%**. AC1–AC8 and AC10 are fully implemented and evidenced. AC9 lacks final validation and the published consumer lane. Implementation-only completeness is **100%**; full AC1–AC10 acceptance remains **95%** until those gates pass.

| Criterion | Completion | Source/contract and evidence | Remaining work |
|---|---:|---|---|
| AC1 boundaries | 100% | `docs/design.md`, `docs/managed-projections.md`; generic core Engine/Search/Projector/OutputPolicy; host/checkpoint packages own typed query, authentication grants, serialization and SQLite/HTTP checkpoint integration. Consumer imports are confined to optional `testdata/consumer` and temporary workspace script. | No architectural violation found in reviewed scope. |
| AC2 canonical authority | 100% | `recall.go`, `recall_state.go`, `projected_recall.go`; `TestProposalAcceptanceIsolation`, `TestCanonicalReadsRevalidateSource`, `TestSourceRevalidatedAfterReadCallbacks`, projected callback revalidation cases source/authority/expiry/forget/cancel; consumer canonical fixture sends stale revision and hostile index payload and checks canonical result. `host.Seed` requires a distinct reviewer before Commit. | None identified. |
| AC3 isolation | 100% | `checkpoint.Key` structurally serializes scope, handle and sorted exact lineage; Store SQL scopes all rows. `TestCheckpointScopeIsolationAndStructuralKeys` tests identical IDs/handles across tenant/namespace/subject and ambiguous separators. Remote process fixture also tests tenant isolation. | None identified. |
| AC4 evidence | 100% | `score.go`, validations/cloning, `Projection.Retrieval`, reference RRF/ScoreRanker. `TestScorePresenceContract`, `TestScoreRankerPresenceOrder`, `TestProjectedEvidenceCanonicalAndDetached`, `TestCompositeNeverInventsNativeScore`, `TestCompositePreservesProvidedRawScoreWithDistinctRanks`, and conformance score-presence fixtures cover absent/zero/negative/nonfinite/ambiguous state, rank-only evidence and deterministic ordering. Plain Project evidence is nil. | None identified. |
| AC5 ownership/budget | 100% | Detached Retrieval/signals in projected clone, identity-only policy selection; mutation/invalid-selection/revalidation/cost-boundary fixtures. `TestProjectedUnicodeBytesAndEstimatedUnits` distinguishes UTF-8 bytes/runes and estimated units. Exact result json length includes evidence and metadata, budget receipt explicitly out of band. | None identified. |
| AC6 race | 100% | WithDerivedWrite canonical gate and checkpoint epoch fence; channel-controlled `TestForgetBetweenProjectionAndPersistence`, `TestWriteBeforeForgetAndUnknownCallbackOutcome`, existing managed race suite; late candidate/late checkpoint write tests. | None identified. |
| AC7 durable purge | 100% | SQLite transaction persists data/lineage/handle/epoch; Purge acknowledges after commit. `TestDurableMultiLineagePendingRetryRestore`, `TestPurgeAllDependentCopiesAndLateOldScopeBatch`, sink conformance, process backend tests prove multi-copy deletion after reopen, pending retry without new canonical revision, stale writes, idempotent old batches and unknown response reconciliation. | Guarantees limited to tested local SQLite and HTTP/SQLite participant contract. |
| AC8 restore | 100% | `checkpoint.ReadValidated` snapshots validated structural key, scope, full lineage and bytes before fresh canonical fence/gate; no external I/O occurs after eligibility. New `TestCheckpointMissingAndUnavailableCanonicalLineage` proves exact orphan revision rejection/invalidation and unavailable canonical DB fail-closed behavior; existing tests cover revoked old checkpoint, source change/cancel/recompile. `TestCheckpointDeliveryRevalidatesAfterAllStorageIO` prevents external I/O after authority checks. | None identified; canonical backup/current revocation ledger remains explicitly documented host responsibility. |
| AC9 checks | 50% | Optional local suite has repeated successful eight-test race run and manifest on current code. Published script/CI lane enforces changed explicit tag, GOWORK off, and no Replace anywhere. Process/TCP/SQLite durable backend suite is real integration with canonical registered Sink and restart/unknown-effect recovery. | Full make validate successful terminal exit not yet observed. Published artifact lane pending release. |
| AC10 workstation | 100% | Three original byte copies, full diffs, manifest/checksums and per-file disposition preserved under local-duplicates; no package exclusions/build tags. Current ordinary targeted compilation passes; no alternate duplicate files remain in source packages. | None identified; full ordinary tree check is also pending under AC9 validation. |

## Independent execution

Pre-release targeted evidence (previous manifest), passed exit 0:

```text
go test -race -count=1 ./examples/managed-projections/... ./reference ./conformance -run 'Test(DurableMultiLineage|CheckpointScope|ForgetBetween|WriteBefore|CheckpointSource|Remote|PurgeAll|CheckpointSink|CompositeNever|CompositePreserves|JSONPacking)'
```

Checkpoint, host and reference tests passed, including separate OS-process TCP participant kill/restart and canonical Forget receipt-loss recovery. The conformance package itself has no direct tests; its contracts execute via adapter fixture tests. This run is targeted evidence and does not substitute for all of make validate.

## Work/artifact audit

1. Contract-first docs, score value shape, executable tests and migration section exist. Persisted canonical schema is unchanged by retrieval API changes, so no schema migration is necessary for this change.
2. Optional scores, native evidence separation and projected evidence are implemented directly; no compatibility wrappers found.
3. Runnable offline example performs lifecycle, canonical projection, complete-envelope materialization, managed durable write, close/reopen, revoke/pending/retry and stale-index filtering. Reviewer grants are separate host state.
4. Checkpoint implementation and runnable example are outside core. Transactional reverse lineage and scoped deletion handles are present; no recursive canonical store call occurs in the write callback.
5. Restore behavior and bounded guarantees are documented and executable for revoke/source/cancel, missing exact lineage and unavailable canonical database. Prior AC8 test gaps are resolved.
6. Consumer script uses a temporary module; local source replacements are explicit and published mode rejects replacements. Published success evidence remains outstanding.
7. Local duplicate copies/checksums/diffs and disposition are preserved. No legacy merge was needed; reason is recorded per file.
8. Matrix now has requirement → code/contract → executable evidence → result columns and names concrete test cases. Some AC7/AC8 notes still reflect prior review status; update them to final review outcome and add terminal final validation/published commands when available. Structural evidence gap is resolved; stale result text should be reconciled before closeout.
9. AC11 is ongoing: this review is one half of independent acceptance of a pre-release snapshot; correctness report and any findings must be reconciled, affected checks rerun and both reviewers recheck final snapshot.
10. AC12 release/closeout is not done. Substantial score/evidence API change requires `make release-break`, then new artifact published consumer verification. Author migration comment must cover scores/evidence, authenticated scope, sink registration, fence/full lineage synchronous writes, handles/pending retries and checkpoint invalidation, with links and actual before/after API. Issue must remain open until agreed external boundary is verified or explicitly accepted by author.

## Actionable gaps

- Reconcile matrix AC7/AC8 status text with these resolved findings and final correctness review; record terminal validation/publication evidence when completed.
- Obtain successful terminal make validate evidence on final source snapshot.
- Complete release-break and changed-artifact published consumer lane; review published graph and semantic test results.
- Preserve the documented HTTP/SQLite backend scope in publication/closeout. No author agreement is required merely because a backend-choice question was sent: task1 permits a selected backend with a real independent integration test. Its final external-boundary clause requires evidence OR explicit agreement to transfer remaining work. Here backend integration is performed; no remaining backend work is being transferred. Published consumer verification still must pass. Do not claim arbitrary production/HA/replicated guarantees.
- Finish independent final snapshot acceptance and explicit migration communication before issue closure.

No confirmed architectural boundary violation was found. This is a bounded completeness review and makes no mathematical error-free claim.

## Changes since rejected pre-release snapshot

- Missing/unavailable restore cases have executable tests against real durable checkpoint/canonical resources.
- Correctness P1: checkpoint I/O is fully completed before canonical eligibility; copied immutable bytes and exact lineage are gated with no second external Load in callback. Regression asserts that external loads cannot occur after authority checks.
- Correctness P2: unknown HTTP execution outcome is preserved as ErrUnknownOutcome with availability classification. Separate-process gateway fixture durably commits then changes reply to 502 and checks both unknown classification and retained effect.
- No unresolved implementation gap or architectural boundary violation found in this review. Correctness reviewer independently determines defect closure.

Final manifest independent targeted run, exit 0:

```text
go test -race -count=1 ./examples/managed-projections/host ./examples/managed-projections/checkpoint -run 'TestCheckpoint(MissingAndUnavailableCanonicalLineage|DeliveryRevalidatesAfterAllStorageIO)|TestRemoteGatewayStatusAfterDurableCommit'
ok github.com/skosovsky/memy/examples/managed-projections/host 1.684s
ok github.com/skosovsky/memy/examples/managed-projections/checkpoint 1.261s
```

Final manifest rechecked at report end: all 202 source hashes match. Current local consumer output ends PASS, eight tests; full validation log still has no terminal final success evidence at this review.
