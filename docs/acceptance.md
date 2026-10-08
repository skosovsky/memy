# Acceptance matrix

Current tooling uses `make check`; historical `make validate` commands below
refer to their frozen snapshots. See [development checks](development.md).

Frozen requirement inventory v1, 2026-10-04, before implementation.
Source: `.cursor/tasks/task1.md`. Each row must be fully implemented and
verified to count; partial and unverified rows count zero. Rows cannot be
deleted/merged to improve the percentage. New discovered source requirements
are added with explanation. Reported implementation completeness is separate
from final overall acceptance, which requires both final review gates.

Historical implementation acceptance: 83/83 requirements independently verified, plus both
independent review gates satisfied: 85/85 (100%). Completeness evidence is in
reviews/completeness-managed-writes.md; correctness closure is in reviews/correctness-managed-writes.md
and the preceding independent reports. Final exact snapshot confirmations are
recorded separately in reviews/completeness-final.md and reviews/correctness-final.md.
A listed fixture alone is not proof; the independent reports inspect the task,
implementation, contracts and behavioral evidence. See reviews/final-audit.md
for finding closure and command provenance.

The 85-row inventory and its review statuses below describe the historical
accepted snapshot. Retrieval API references now use the task03 contract:
coverage means availability/index visibility, not relevance completeness;
RecallResult.Complete is removed, and bounded processing is reported separately
by RecallProgress. The frozen historical denominator is unchanged. Task03 is
independently accepted at 100%; its separate gates RT03-01..08 are verified below.
Evidence: [completeness](reviews/task03/completeness.md),
[correctness](reviews/task03/correctness.md), and [validation](reviews/task03/validation.md).

Quality API/test/example pointers below identify current v2 counterparts after
removal of the v1 harness. They do not renew the historical acceptance statuses
or change the frozen 85-row denominator. The v1 latency evidence belongs to the
historical accepted snapshot; v2 reports no quality timing metric. Task04 has its
own QH04-01..10 matrix below and is independently accepted.

| ID | Required behavior | API / docs | Behavioral test | Runnable example | Status |
|---|---|---|---|---|---|
| BOOT-01 | Pinned compatible toolchain and reproducible CI | go.mod; .github/workflows/ci.yml; Makefile; docs/design.md | make validate on pinned local Go 1.27.1; CI configuration inspected | make validate | verified independently |
| BOOT-02 | README/package docs/design before API | README.md; doc.go; docs/design.md; docs/progress.md design-first record | Example; TestOfflineLifecycle | examples/lifecycle | verified independently |
| BOOT-03 | BYOT payload/reference/input/query/projection/authority | Config[P,R,A]; Extract[I]; Search[Q]; Project[O]; Resolver[K] | TestTypedDomainResolverMapsClaimWithoutInferringScope; TestTypedProjectionAndExportAuthority | Example; ExampleJSONCodec; examples/lifecycle; examples/quality | verified independently |
| BOOT-04 | Core offline, no ai-libs/telemetry/network dependency | doc.go; go.mod; root dependency graph | go list -deps .: root is only nonstandard package; offline full fixtures | examples/lifecycle; examples/quality require no credentials | verified independently |
| BOOT-05 | Versioned deterministic codecs/digests; map-order independence | codec.go; proposalContentDigest; versioned codecs | TestJSONCodecCanonicalMapOrder; TestJSONCodecRejectMalformed; FuzzJSONCodec | Example; schemas and docs/design.md | verified independently |
| BOOT-06 | Wire schemas/version checks/migration/import policy | wire.go; wire_shape.go; validation.go; schemas/*-v2.schema.json; docs/migration.md | TestPersistedLifecycleMatchesWireSchemas; TestWireSchemaRejectsMalformedEnvelope; TestMalformedCanonicalDocumentsFailClosed; TestCanonicalCannotDropOrReplaceReviewedLineage; TestNullRevocationEpochCannotResetFence; TestByteWireRepresentationFailsClosed; TestTimestampWireFormatMatchesExecutableSchema; TestRejectIncompatibleSchema | examples/lifecycle; docs/migration.md host-approved import | verified independently |
| BOOT-07 | Bounded concurrency, context/cancel, resource cleanup | Store; Engine synchronous callbacks; reference Index deadline wait | StoreSuite/context_aware_wait,cancel_rollback,closed; TestIndependentWriterCancellationAndRecovery; TestVisibilityCancellationAndBackendFailure; TestCancellationDuringVisibilityWaitPreservesCanonicalCommit | examples/lifecycle temporary resource cleanup | verified independently |
| BOOT-08 | In-memory reference and common conformance | store/memory; conformance.StoreSuite | store/memory.TestConformance: 12 shared invariant cases | Example | verified independently |
| BOOT-09 | Persistent atomic/CAS/idempotent backend | store/sqlite; operation ledger; Commit/Forget atomic updates | TestConcurrentCommitAndDurableIdempotency; TestDurableCommitFaultAndOperationRecovery; TestDurableForgetFaultPendingReceiptAndReopen | examples/lifecycle | verified independently |
| BOOT-10 | Close/reopen and independent concurrent persistent writers | store/sqlite BEGIN IMMEDIATE; schema and close handling | TestDurableReopen; TestIndependentConcurrentWriters; TestIndependentWriterCancellationAndRecovery | examples/lifecycle close/reopen | verified independently |
| BOOT-11 | Before/after-commit fault injection and recovery | sqlite.Options.Fault; ErrUnknownOutcome; operation replay | TestFaultRecovery; TestDurableCommitFaultAndOperationRecovery; TestDurableForgetFaultPendingReceiptAndReopen | docs/design.md recovery contract; examples/lifecycle | verified independently |
| BOOT-12 | Honest capability and unsupported/error taxonomy | LifecycleCapabilities; StoreCapabilities; SearchCapabilities; errors.go | TestLifecycleCapabilitiesMatchTemporalAndFieldBehavior; TestAuthorityAndFieldProfilesFailClosed; TestMixedSearchPreservesCoverageAndRejectsStrongerProfile; TestCompositeRejectsTypedNilBackend | README guarantees/limits; examples/lifecycle | verified independently |
| BOOT-13 | Reusable conformance for every implemented non-Store port | conformance SearchSuite; SinkSuite; CodecSuite; AuthoritySuite; SourcesSuite; ClockSuite; CallbackSuite | reference.TestSearchConformance; TestSinkConformance; TestHostPortConformance; TestTypedCallbackConformance | reference/conformance_test.go consumer-owned typed fixtures | verified independently |
| MEM-001-01 | Scoped records, revisions, provenance, source versions | Scope; Record; Provenance; Source; lifecycle.go | TestValidAndRecordedTime; TestProposalAcceptanceIsolation; TestPolicyAndSourceChangesRejectCommit; TestSourceOutageCannotBecomeSuccessfulCanonicalCommit | Example; ExampleJSONCodec; examples/lifecycle | verified independently |
| MEM-001-02 | Required host authority decisions before read/write | Authority[A]; authorize/reauthorize | TestAuthorityAndFieldProfilesFailClosed; TestProjectionRechecksAuthorityAfterConsumerCallback | Example; ExampleJSONCodec; examples/lifecycle | verified independently |
| MEM-001-03 | Payload cannot assign scope/authority | Suggestion excludes host authority/scope; engine stamps authorized scope | TestAuthorityAndFieldProfilesFailClosed; TestForgetSelectorsRespectExactAuthorityBoundary | examples/lifecycle adversarial extraction | verified independently |
| MEM-001-04 | Cross-tenant denial conceals existence | authorize before bucket access; exact Scope | TestAuthorityAndFieldProfilesFailClosed existing/absent denial; TestForgetSelectorsRespectExactAuthorityBoundary; TestProcedurePlansExecutePublicLifecycle cross-scope case | examples/quality foreign tenant in shared store | verified independently |
| MEM-001-05 | Field projection capability rejects unsupported profiles | LifecycleCapabilities.FieldProjection=false; authorize rejects Fields | TestAuthorityAndFieldProfilesFailClosed; TestRestrictedRecallAndProjectionNeverExposeFieldsToPolicies | README limits; docs/design.md | verified independently |
| MEM-001-06 | Concurrent expected-revision writes: one winner | CommitRequest.Expected; head CAS and atomic reconciliation | TestConcurrentCommitAndDurableIdempotency; StoreSuite/concurrent_cas | docs/design.md; examples/lifecycle correction | verified independently |
| MEM-001-07 | Same operation/input recovers receipt, different input conflicts | operationDigest; operationDisk; replayCommit | TestConcurrentCommitAndDurableIdempotency; TestProposalReplayKeepsOriginalRevisionAfterLaterEdits | examples/lifecycle; docs/design.md | verified independently |
| MEM-001-08 | Canonical state and receipts atomically persisted | persistCommit in Store.Update; scoped operation ledger | TestDurableCommitFaultAndOperationRecovery; TestFaultRecovery; StoreSuite/rollback | examples/lifecycle; docs/design.md | verified independently |
| MEM-002-01 | Remember and typed extraction produce separate proposals | Remember; Extract; Proposal separate documents | TestProposalAcceptanceIsolation; TestZeroClockCannotPersistMalformedProposal | Example; ExampleJSONCodec; examples/lifecycle | verified independently |
| MEM-002-02 | Proposed/accepted/rejected/expired lifecycle | ProposalState; Accept; Reject; Proposal expiry; Sweep | TestProposalAcceptanceIsolation; TestPreferencePlansPublicLifecycle semantic rejection; TestCanonicalProposalExpiryClosesReadsAndPurges | examples/quality; docs/design.md | verified independently |
| MEM-002-03 | Host acceptance is separate and proposal-specific | Accept creates proposal-specific Acceptance; acceptedProposal | TestProposalAcceptanceIsolation sibling rejection | Example; ExampleJSONCodec; examples/lifecycle | verified independently |
| MEM-002-04 | Acceptance binds digest/revision/actor/scope/policy | Acceptance; acceptanceMatches; persistAcceptance | TestChangedProposalInvalidatesAcceptance; TestPolicyAndSourceChangesRejectCommit; TestSourceOutageCannotBecomeSuccessfulCanonicalCommit | Example; docs/design.md | verified independently |
| MEM-002-05 | Commit rechecks acceptance, policy, rights, expiry and sources | acceptedProposal; validateInputs; validateRetention; final reauthorize | TestPolicyAndSourceChangesRejectCommit; TestSourceOutageCannotBecomeSuccessfulCanonicalCommit; TestRetentionChangeInvalidatesAcceptedCommit; TestAcceptanceRejectsExpiredAncestorBeforeSweep | examples/lifecycle; docs/design.md | verified independently |
| MEM-002-06 | Changed digest/policy/source yields stale acceptance/input | Revise; input/source/policy validation | TestChangedProposalInvalidatesAcceptance; TestPolicyAndSourceChangesRejectCommit; TestSourceOutageCannotBecomeSuccessfulCanonicalCommit; TestProposalReplayKeepsOriginalRevisionAfterLaterEdits | docs/design.md; examples/lifecycle | verified independently |
| MEM-002-07 | Source outage/missing evidence/unsupported schema fail explicitly | ErrMissingEvidence/ErrSourceUnavailable/ErrSchema; decodeDocument | TestExtractionAbstainsWithoutEvidence; TestPolicyAndSourceChangesRejectCommit; TestSourceOutageCannotBecomeSuccessfulCanonicalCommit; TestMalformedCanonicalDocumentsFailClosed | docs/design.md; schemas; examples/quality | verified independently |
| MEM-002-08 | Adversarial text cannot escalate permissions/prompts | host acceptance; data Trust; no executable prompt/tool port | TestProposalAcceptanceIsolation; TestEpisodeAndProceduralProposalRemainDataAfterReopen | examples/lifecycle accepts timezone, not permission claim | verified independently |
| MEM-002-09 | Idempotent extraction job identity/input and stale epoch fence | extractionSnapshot; request digest; persistExtraction epoch check | TestProposalAcceptanceIsolation replay; TestLateExtractionFencedByForget; TestZeroClockCannotPersistMalformedProposal | examples/lifecycle; docs/design.md | verified independently |
| MEM-003-01 | Caller resolver claim mapping, no embedding inference | Resolver[P,R,K]; CommitTarget; resolver.go | TestTypedDomainResolverMapsClaimWithoutInferringScope | docs/design.md typed resolver/operation retry guidance | verified independently |
| MEM-003-02 | Append/duplicate/supersede/conflict preserve lineage | Reconciliation; reconciliation.go; commitLineage | TestDuplicateLineageRevokesTransitiveDependents; TestConflictHistoryAndExplicitResolution; TestConcurrentCommitAndDurableIdempotency | examples/lifecycle correction; examples/quality append summaries | verified independently |
| MEM-003-03 | Resolution records basis/policy and all expected revisions | Reconciliation.PolicyVersion/Basis/Related; relatedRevision | TestConflictHistoryAndExplicitResolution; TestCorruptRelatedIdentityCannotReconcile; TestConcurrentCommitAndDurableIdempotency | docs/design.md; examples/lifecycle | verified independently |
| MEM-003-04 | Observed/recorded/valid time distinct; invalid interval rejected | Interval.Validate; Record observed/recorded/valid times; LifecycleCapabilities | TestValidAndRecordedTime; TestJSONCodecRejectMalformed; TestRegressedClockCannotBackdateRecordedCorrection | docs/design.md temporal model | verified independently |
| MEM-003-05 | Unknown valid interval excluded from known-as-of claims | readable unknown-validity predicate; IncludeUnknown | TestUnknownValidityAndOverlappingClaims | docs/design.md; docs/migration.md unknown import | verified independently |
| MEM-003-06 | July/September fixture selects UTC+3/UTC+7 | Get valid-as-of canonical history | TestValidAndRecordedTime UTC+3 before August / UTC+7 after August | docs/design.md; runnable Go temporal fixture | verified independently |
| MEM-003-07 | Recorded-as-of hides later correction/state transition | recordDisk.InitialState/Transitions; readable RecordedAsOf | TestValidAndRecordedTime; TestConflictHistoryAndExplicitResolution; TestRegressedClockCannotBackdateRecordedCorrection | docs/design.md; runnable temporal fixtures | verified independently |
| MEM-003-08 | Conflicts visible/excludable; no automatic latest-wins | Conflicted; IncludeConflicts; ErrUnresolvedConflict | TestConflictHistoryAndExplicitResolution; TestUnknownValidityAndOverlappingClaims | docs/design.md; runnable conflict fixture | verified independently |
| MEM-003-09 | Concurrent supersede preserves winning revision | atomic reconciliation + expected head revisions | TestConcurrentCommitAndDurableIdempotency SQLite supersede race/reopen | examples/lifecycle; docs/design.md | verified independently |
| MEM-004-01 | Typed scoped search distinct from canonical Get | Search[Q]; Recall; Get; Candidate metadata-only | TestCanonicalCommitAndSearchVisibility; TestTypedProjectionAndExportAuthority | examples/lifecycle | verified independently |
| MEM-004-02 | Canonical ACL/status/tombstone/temporal revalidation | recallCandidate; snapshotCandidate; readable; revocation and lineage checks | TestForgetPartialPurgeRetryAndLateJobs; TestMalformedLineageDoesNotBecomeEmptyRecall; TestRestrictedRecallAndProjectionNeverExposeFieldsToPolicies | examples/lifecycle; docs/design.md | verified independently |
| MEM-004-03 | Ranking/explanations/provenance/conflict/expiry/completeness (historical requirement wording) | Ranked with typed signals; ScoreRanker; RecallResult.Coverage/Progress; Record metadata; current contract makes no relevance-completeness claim | TestCanonicalCommitAndSearchVisibility; TestRankerCannotRewriteConsumerMapsOrProvenance; TestConflictHistoryAndExplicitResolution; TestCanonicalProposalExpiryClosesReadsAndPurges | examples/lifecycle; examples/quality | verified independently |
| MEM-004-04 | Safe typed data projection/export and cache identity | Project; Projection.Trust/CacheKey; projectionRecord | TestTypedProjectionAndExportAuthority; TestProjectionCacheIdentityBindsReadContext; TestRestrictedRecallAndProjectionNeverExposeFieldsToPolicies | examples/lifecycle; bounded knowledge fixture | verified independently |
| MEM-004-05 | Controlled deterministic index lag and exact ack recall | reference.Index.Stage/Acknowledge; VisibilityToken | TestCanonicalCommitAndSearchVisibility | examples/lifecycle controlled lag | verified independently |
| MEM-004-06 | Minimum visibility bounded wait pending/unsupported | SearchCapabilities.Visibility; Index minimum deadline validation | TestCanonicalCommitAndSearchVisibility; TestMixedSearchPreservesCoverageAndRejectsStrongerProfile | examples/lifecycle; docs/design.md | verified independently |
| MEM-004-07 | Cancel visibility wait leaves canonical commit intact | context-aware index wait; canonical/search separate transactions | TestVisibilityCancellationAndBackendFailure; TestCancellationDuringVisibilityWaitPreservesCanonicalCommit | examples/lifecycle; docs/design.md | verified independently |
| MEM-004-08 | Backend failure unavailable or explicit degraded coverage | Index.Fail; Composite.AllowDegraded; Coverage | TestVisibilityCancellationAndBackendFailure; TestCancellationDuringVisibilityWaitPreservesCanonicalCommit; TestCompositePartialFailureRequiresExplicitDegradedMode | docs/design.md; runnable mixed-search fixtures | verified independently |
| MEM-004-09 | Mixed backend coverage and eventual capability honesty | reference.Composite named Backend/RRFConfig; reference.Eventual; BoundedCandidates separate from Visibility | TestMixedSearchPreservesCoverageAndRejectsStrongerProfile; TestCompositePartialFailureRequiresExplicitDegradedMode | docs/design.md; runnable mixed-search fixtures; task03 example pending current acceptance | verified independently |
| MEM-004-10 | Final authority version/decision rechecked before return | reauthorize before return/transaction end | TestProjectionRechecksAuthorityAfterConsumerCallback; TestRecallRechecksRetentionAfterRanking; TestProjectionRechecksRetentionAfterProvider | docs/design.md external decision lease boundary | verified independently |
| MEM-005-01 | Authorized record/source/subject/scope selectors, expected revision | Selector; expectedForgetRevisions; revokeOperation | TestForgetSelectorsRespectExactAuthorityBoundary; TestConcurrentCommitAndDurableIdempotency; TestForgetPartialPurgeRetryAndLateJobs | examples/lifecycle; docs/design.md | verified independently |
| MEM-005-02 | Durable content-free revoke/epochs immediately close recall | epochDisk; revocationDisk; revokeRecord content-free tombstone | TestDurableForgetFaultPendingReceiptAndReopen; TestNullRevocationEpochCannotResetFence | examples/lifecycle | verified independently |
| MEM-005-03 | Canonical/history/proposal/derived payload cleanup | revocationHistory/selectDependentRecords; purgeProposals; managed sinks | TestSourceForgetPurgesHistoricalPayloadAfterCorrection; TestForgetPartialPurgeRetryAndLateJobs byte scan; TestDuplicateLineageRevokesTransitiveDependents | examples/lifecycle; docs/design.md | verified independently |
| MEM-005-04 | Managed index + summary sink lineage and deletion handles | Sink; PurgeBatch/PurgeAck; reference Index/ProjectionSink | TestForgetPartialPurgeRetryAndLateJobs; TestManagedWriteCannotRacePurgeToResurrectArtifacts | examples/lifecycle two sinks | verified independently |
| MEM-005-05 | Durable purge receipt per sink, pending/failed/complete states | PurgeReceipt; persistSinkResult; failed/pending state classification | TestDurableForgetFaultPendingReceiptAndReopen; TestFailedPurgeRemainsFencedAndCanRecoverAcrossReopen | examples/lifecycle pending retry; docs/design.md failed semantics | verified independently |
| MEM-005-06 | One sink fails, retry completes without new revisions | finishPurge merges durable per-sink acknowledgements | TestForgetPartialPurgeRetryAndLateJobs; TestDurableForgetFaultPendingReceiptAndReopen; TestFailedPurgeRemainsFencedAndCanRecoverAcrossReopen | examples/lifecycle | verified independently |
| MEM-005-07 | Late extraction/index/summary writes fenced incl. purge races | EpochFence; WithDerivedWrite; extraction epoch CAS | TestLateExtractionFencedByForget; TestManagedWriteCannotRacePurgeToResurrectArtifacts memory and SQLite; TestManagedWriteRejectsExpiryDuringFinalReauthorization; TestForgetPartialPurgeRetryAndLateJobs | examples/lifecycle; docs/design.md | verified independently |
| MEM-005-08 | Stale index restore cannot disclose revoked payload | canonical tombstone/revocation revalidation despite index metadata | TestForgetPartialPurgeRetryAndLateJobs stale index restoration | examples/lifecycle; docs/design.md backup boundary | verified independently |
| MEM-005-09 | Injected retention/clock; expiry follows same purge lifecycle | RetentionPolicy; Clock; Sweep; effective evidence deadline | TestRetentionUsesManagedPurgeAndResumesPending; TestCanonicalProposalExpiryClosesReadsAndPurges; TestSweepAppliesShorterCurrentRetentionDeadline; TestSweepPurgesDraftsUnderCurrentRetention | docs/design.md; runnable retention fixtures | verified independently |
| MEM-005-10 | Tombstone retention/reintroduction versioned host policy | ReintroductionPolicy; Reintroduce; immutable retired record IDs | TestReintroductionNeedsExplicitHostPolicy; TestFailedPurgeRemainsFencedAndCanRecoverAcrossReopen | docs/design.md; runnable host-controlled reintroduction fixture | verified independently |
| MEM-005-11 | Backup/deletion boundaries and unmanaged copies declared | README boundaries; docs/design.md; docs/migration.md | TestForgetPartialPurgeRetryAndLateJobs proves logical API boundary; stale restoration fixture | docs/migration.md revocation ledger before serving restored backup | verified independently |
| MEM-006-01 | Opt-in exact dedup with interval/negation/lineage preservation | Consolidate ExactDedup; exactDedup/dedupSources | TestExactDedupPreservesNegationValidityLineageAndOriginals; TestConsolidationCannotExtendFiniteEvidenceExpiry | examples/quality | verified independently |
| MEM-006-02 | Typed BYOT domain merge provider and reference path | Consolidator[P,R]; reference.MergeFunc; DomainMerge | TestDomainMergeAndBudgetFailureAreNonDestructive; TestConsolidationQualityGatesPreserveOriginalState; TestPreferencePlansPublicLifecycle | examples/quality | verified independently |
| MEM-006-03 | Typed semantic provider and offline scripted reference path | SemanticMerge; reference.MergeFunc; host review | TestSemanticProposalReviewAndSourceRevocation; TestReviewedSemanticSummaryCommitsAndRetainsOriginals | examples/quality scripted negative semantic; positive runnable fixture | verified independently |
| MEM-006-04 | Scoped revision set, versioned policy/budget/utility gate | ConsolidationRequest; consolidationInputs; Budget/MinimumUtility | TestDomainMergeAndBudgetFailureAreNonDestructive; TestConsolidationQualityGatesPreserveOriginalState; TestExactDedupPreservesNegationValidityLineageAndOriginals | examples/quality; docs/design.md | verified independently |
| MEM-006-05 | Consolidation emits loss/uncertainty annotated proposals | Suggestion/Provenance losses, uncertainties, lineage; prepareSuggestions | TestSemanticProposalReviewAndSourceRevocation; TestReviewedSemanticSummaryCommitsAndRetainsOriginals | examples/quality | verified independently |
| MEM-006-06 | Normal acceptance/reconciliation; originals survive | Consolidate proposals only; ordinary Accept/Commit; append derived records | TestReviewedSemanticSummaryCommitsAndRetainsOriginals; TestExactDedupPreservesNegationValidityLineageAndOriginals | examples/quality | verified independently |
| MEM-006-07 | Derived scope intersection and private-input prevention | exact scoped Snapshot inputs; full-record ACL; Fields rejected before provider | TestExactDedupPreservesNegationValidityLineageAndOriginals foreign input; TestRestrictedRecallAndProjectionNeverExposeFieldsToPolicies | examples/quality foreign B; README field-profile boundary | verified independently |
| MEM-006-08 | Stale/revoked source rejects acceptance; dependent invalidation | validateInputs exact source/lineage; transitive canonical invalidation | TestSemanticProposalReviewAndSourceRevocation; TestDuplicateLineageRevokesTransitiveDependents; TestConsolidationReplayAfterInputCorrection | docs/design.md; runnable source-revocation fixtures | verified independently |
| MEM-006-09 | Auto-apply disabled, insufficient evidence/budget fail safely | Consolidate requires host review; evidence, utility and budget rejection | TestDomainMergeAndBudgetFailureAreNonDestructive; TestConsolidationQualityGatesPreserveOriginalState; TestExtractionAbstainsWithoutEvidence; TestPreferencePlansPublicLifecycle rejects negative semantic | examples/quality; docs/quality.md | verified independently |
| MEM-006-10 | Versioned corpus compares expected records/false-memory/privacy | testdata/quality-v2.json; quality.Run; CasePlan required checkpoints; schemas/quality-*-v2.schema.json | TestPreferencePlansPublicLifecycle; TestProcedurePlansExecutePublicLifecycle; TestMalformedManifestFailsBeforeDispatch; TestQualitySchemasExecuteAgainstActualReports | examples/quality; docs/quality-report.json v2; separate accepted QH04 matrix | verified independently |
| MEM-006-11 | Zero leaks/lost negations/intervals, measured bytes/cost/latency | v2 exact payload/revision/negation/interval/privacy gates; payload/context bytes and abstract units; v1 latency remains historical only | TestPreferencePlansPublicLifecycle; TestExactDedupPreservesNegationValidityLineageAndOriginals; TestProcedurePlansExecutePublicLifecycle; TestPreferenceErrorDoesNotCertifyZeroMeasurements | docs/quality.md measurement boundaries; quality-report.json per-metric known/unavailable status | verified independently |
| MEM-006-12 | Negative semantic result saved; shadow/rollback guidance | v2 candidate/host-review/effective/canonical/rendered/execution stages; expected semantic rejection preserves baseline; docs/quality.md rollout/rollback boundaries | TestPreferencePlansPublicLifecycle semantic case; TestExpectedCandidateRejectionCanPassProtocol; TestEvaluatorMutationsRejectWrongObservations | examples/quality; quality-report.json separate descriptive bad candidate and final protocol verdict | verified independently |
| EVAL-01 | Authorized typed snapshots/receipts with host redaction | Snapshot; typed Record/Provenance; Commit/Purge receipts; authorize rejects unsupported field redaction | TestRestrictedRecallAndProjectionNeverExposeFieldsToPolicies; TestTypedProjectionAndExportAuthority; TestRankerCannotRewriteConsumerMapsOrProvenance | examples/lifecycle; README full-record reference profile | verified independently |
| EVAL-02 | Quality checked for first extraction/recall before consolidation | RunProcedureCase typed Extract/Accept/Commit/RecallProjected and independent canonical checks; preference consolidation baseline checked separately | TestProcedurePlansExecutePublicLifecycle; TestPreferencePlansPublicLifecycle; TestPreferenceOracleRejectsSameCountWrongObservation | examples/quality; examples/quality-integration same checkpoints; docs/quality.md | verified independently |
| EVAL-03 | Multi-session persistent corrected preference; state/recall/answer separate | SQLite lifecycle with fresh engine after reopen; separate canonical/recall/projected output checks | examples/lifecycle.TestOfflineLifecycle; TestValidAndRecordedTime; TestEpisodeAndProceduralProposalRemainDataAfterReopen | examples/lifecycle | verified independently |
| EVAL-04 | Abstention for insufficient evidence | Extract rejects empty suggestions/evidence; no canonical update | TestExtractionAbstainsWithoutEvidence | docs/design.md; runnable extraction abstention fixture | verified independently |
| EVAL-05 | Episodic result stays bounded; procedural proposal cannot execute | bounded valid interval; procedural proposals separate; Project Trust=data | TestEpisodeAndProceduralProposalRemainDataAfterReopen | docs/design.md; runnable persistent bounded-knowledge fixture | verified independently |
| EVAL-06 | Corpus/provider/resolver/projection/store versions recorded | v2 manifest pins corpus/scenario/provider/model/policy/search/projector/packing/grader versions, budgets, seed/repeats; scenario records actual ports; fixture store selection and go.mod remain separate | TestSavedManifestPinsRegisteredPlans; TestSeedAndRepeatsActuallyDispatch; TestProcedurePortSubstitutionRetainsCheckpointOracles; TestQualitySchemasExecuteAgainstActualReports | examples/quality; examples/quality-integration; docs/quality-report.json v2 | verified independently |
| DOD-01 | gofmt and go vet pass | Makefile format/vet; docs/progress.md verification log | make validate: format and go vet ./... | make validate | verified independently |
| DOD-02 | go test and go test -race pass incl. adapters | Makefile test/race; all adapters in one module | make validate: go test ./..., go test -race ./... | examples/lifecycle and examples/quality | verified independently |
| DOD-03 | Package examples + full offline proposal→accept→commit example | example_test.go; examples/lifecycle; examples/quality; docs/design.md | Example; ExampleJSONCodec; examples/lifecycle.TestOfflineLifecycle; TestPreferencePlansPublicLifecycle | make examples | verified independently |
| DOD-04 | Malformed input, adversarial cases, faults, cancellation verified | conformance and lifecycle adversarial/fault suites; FuzzJSONCodec | TestMalformedCanonicalDocumentsFailClosed; TestProposalAcceptanceIsolation; TestFaultRecovery; TestVisibilityCancellationAndBackendFailure; TestCancellationDuringVisibilityWaitPreservesCanonicalCommit; TestManagedWriteCannotRacePurgeToResurrectArtifacts | make test; make race; make fuzz | verified independently |
| DOD-05 | Complete evidence matrix + honest limitations and command results | docs/acceptance.md; docs/progress.md; README limits; docs/quality.md | 85-row inventory cross-checked against source; BOOT-13 discovered independently; completeness-managed-writes.md | make validate; docs/quality-report.json | verified independently |
| REVIEW-01 | Independent final completeness review: 100% evidence | reviews/completeness-managed-writes.md; reviews/completeness-final.md | Independent task-to-code audit, fresh pinned checks, external consumer probes; exact final snapshot confirmation | reviews/snapshot-final.json | verified independently |
| REVIEW-02 | Independent final correctness review: no open confirmed defects | reviews/correctness-managed-writes.md; reviews/correctness-final.md | Independent adversarial/fault suite; C1–C13 closure chain in reviews/final-audit.md; exact final snapshot confirmation | reviews/snapshot-final.json | verified independently |

## Counting

There are 84 original frozen rows plus BOOT-13, discovered by the independent
completeness audit from task line 15: common conformance applies to all
implemented ports, not only Store. The denominator is now 85; none of the
original rows were removed or merged (verify mechanically when reporting). Completion is
fully verified rows / all rows × 100. Card percentages use the rows with that
card's prefix. Reviewers must additionally compare the original task text to
this inventory: an omitted obligation must be added, not silently ignored.

## Final counts

| Group | Verified / required | Percent |
|---|---:|---:|
| BOOT | 13/13 | 100% |
| MEM-001 | 8/8 | 100% |
| MEM-002 | 9/9 | 100% |
| MEM-003 | 9/9 | 100% |
| MEM-004 | 10/10 | 100% |
| MEM-005 | 11/11 | 100% |
| MEM-006 | 12/12 | 100% |
| EVAL | 6/6 | 100% |
| DOD | 5/5 | 100% |
| REVIEW | 2/2 | 100% |
| Total | 85/85 | 100% |

## Evidence and limitations

Runtime verification results are recorded in progress.md; an individual test
pass does not prove the full matrix. SQLite's CGO requirements and logical
purge boundaries are described in design.md. External vendor integrations are
not claimed; consumer ports have offline runnable reference paths.

The following entries preserve historical audit states. The final matrix above
and its exact snapshot confirmation reports certify that historical state;
subsequent iteration gates have their own acceptance status below.


Round 1 independently reported 81/85 complete requirements and six confirmed
correctness defects. Changes for round 2 preserve that denominator and add
regressions in acceptance_regression_test.go, source revalidation, instant-based
acceptance equality, strict wire field/key binding, complete content budgets,
and honest Index coverage. Closure requires both independent round 2 reports;
local passing repros are supporting evidence rather than final acceptance.


Round 2 independently confirmed 83/83 implementation obligations (100%), with
reserved final review gates pending (83/85 overall). Correctness confirmed C1–C6
closure, then found C7 byte-wire shape and C8 expiry across I/O. Primary also
reproduced C9 timestamp normalization. New deadline_regression_test.go covers
both stores, read/accept/ancestor-commit crossings, multi-record snapshot
expiry, and executable-schema/runtime shape consistency. Independent closure
of C7–C9 and final state confirmation are required before final acceptance.

Round 3 independently confirmed the same 83/83 implementation obligations and
closed C1–C9. C10 (expired initial acceptance lease) and C11 (transitive target
invalidating its own prospective lineage) are addressed locally in round 4.
TestAcceptanceCannotReturnExpiredInitialAuthorityLease and
TestTransitiveTargetCannotInvalidateItsOwnCommit cover both stores and
rollback/retry behavior. Their independent closure and final review gates are
still pending; the denominator remains 85.

Round 4 completeness again confirmed 83/83 implementation obligations.
Correctness independently closed C10/C11 and confirmed C12, inconsistent
canonical/runtime versus reviewed proposal lineage after storage corruption.
Round 5 validates the mandatory lineage subset; regression evidence is
TestCanonicalCannotDropOrReplaceReviewedLineage on both stores and every
canonical delivery path. Independent closure remains pending.

Round 5 completeness confirmed 83/83 again; correctness independently closed
C12, including extra Duplicate dependencies, then confirmed C13: managed write
could begin after input expiry crossed during final host reauthorization.
Round 6 adds a callback-free final deadline gate immediately before managed
callback entry. TestManagedWriteRejectsExpiryDuringFinalReauthorization covers
direct and transitive evidence on both stores, plus healthy pre-expiry writes.
Independent closure of C13 remains pending.

Round 6 independently closed C13 and reconfirmed all 83 implementation
requirements. The two independent reports satisfy REVIEW-01/02. All C1–C13
findings have independent closure evidence (reviews/final-audit.md); no
confirmed finding remains open. This is not proof that no unknown defects
exist. Final documentation bookkeeping leaves all implementation/test/schema
hashes unchanged from round 6; both reviewers confirm the final snapshot.

## Toolchain and configuration maintenance

The independent acceptance above records the original implementation snapshot.
Subsequent Go/dependency/configuration changes and their fresh verification are
recorded in maintenance.md. Historical snapshots/reviewer conclusions remain
historical evidence rather than claims about unchanged current hashes.

## Iteration 01 — identity and decisions (v2)

| Requirement | Evidence required |
| --- | --- |
| ID-01 | Both stores reject malformed scopes without mutations; exact UTF-8 identity round-trip/property tests |
| ID-02 | Engine inputs and stored documents reject malformed framework identities before normalization |
| ID-03 | All four reconciliation modes persist exact initial decisions across SQLite reopen |
| ID-04 | Exact replay preserves decision; changed request conflicts |
| ID-05 | Authorized historical reads expose only initial decision and state known as-of |
| ID-06 | Forget removes sensitive basis from history and managed projections; receipts remain content-free |
| ID-07 | v2 schema/runtime agree and old database/envelopes fail closed |

## Iteration 02 — storage and concurrency (requirements, not acceptance)

| Requirement | Required evidence |
| --- | --- |
| ST-01 | Both adapters pass shared conformance for transaction lifetime, rollback, panic/cancel, CAS/ABA, close and unknown outcome |
| ST-02 | Ordered bounded scans enforce entry/byte limits, scope/database/prefix binding, cancellation and explicit stale-cursor failure |
| ST-03 | Addressed Get and fixed-input Consolidate do not enumerate/decode unrelated history; instrumented work counters prove the cost boundary |
| ST-04 | Slow rank/project/managed callbacks in scope A permit independent scope B operations; no remote callback holds SQLite's global writer lock |
| ST-05 | Final delivery/managed-write fences preserve authority/source/retention/deadline and revoke behavior, including independent SQLite handles/processes |
| ST-06 | Durable interrupted mass purge resumes after reopen with identical operation identity and no premature completion or resurrection |
| ST-07 | Historical sources and chain/branch lineage purge completely in linear graph traversal without extra revisions on replay |
| ST-08 | Snapshot/Sweep expose bounded continuation and still enforce shortened current retention policies |
| ST-09 | Before/after memory/SQLite cost corpus covers 1k/10k/100k revisions, fixed payload and growing lineage; counters, allocations, latency and contention are recorded |
| ST-10 | Race/vet/format/examples pass; two independent reports certify all task02 requirements on the final source snapshot |

## Task03 retrieval acceptance (accepted)

Task03 changes the retrieval runtime contract; prior audit snapshots remain
historical evidence. All eight gates are independently verified for this iteration:
[completeness100%](reviews/task03/completeness.md),
[correctness](reviews/task03/correctness.md), and [validation](reviews/task03/validation.md).

| ID | Requirement | Required evidence | State |
| --- | --- | --- | --- |
| RT03-01 | Identity-stable RRF, second signals, duplicate isolation and ties | permutation/score-scale/duplicate/invalid-signal behavioral tests | verified independently |
| RT03-02 | Availability, visibility and truncation are separate | mixed failure/minimum/cancellation conformance; unavailable-all case | verified independently |
| RT03-03 | Candidate bound precedes canonical work | oversized adapter + decode/check counters; unsupported/overflow tests | verified independently |
| RT03-04 | Honest exact/estimated final-body budget | zero/oversize/overflow/tie/cancel/envelope-size tests | verified independently |
| RT03-05 | BYOT and offline adapter example | two payload/query types including nontext; exact JSON example | verified independently |
| RT03-06 | Host policies cannot spoof canonical refs/payload | hostile ranker/selector mutation/foreign/duplicate ref tests | verified independently |
| RT03-07 | Final fresh canonical delivery checks | revoke/source change/expiry during packing and measurement | verified independently |
| RT03-08 | Complete delivery and two independent reviews | whole race/vet/format/examples; completeness100% and no open errors | verified independently |


## Task04 quality harness acceptance (accepted)

The v2 contract is implemented and independently accepted: completeness 100%
(10/10 rows), with no unclosed confirmed correctness errors. The saved v2 report
has 32 passing parent protocol outcomes and 14 negative evaluator probes.

Evidence: [completeness](reviews/task04/completeness.md),
[correctness](reviews/task04/correctness.md),
[validation](reviews/task04/validation.md) and
[source hashes](reviews/task04/source-hashes.json). The final full repository
race suite passed (exit 0; root package 915.557s); vet, formatting, CLI exit checks
and all offline examples passed. The old v1 run is preserved only as
`reviews/task04/baseline-report-v1.json`.

| ID | Requirement | Required evidence | State |
| --- | --- | --- | --- |
| QH04-01 | Versioned pinned manifest and actual repeat/seed execution | strict malformed/version/group/budget validation tests; deterministic rerun and observed fixture variation | verified independently |
| QH04-02 | Separate candidate, review, effective, canonical, rendered and execution stages | expected semantic rejection keeps baseline; accepted replacement checked by exact revisions/payload; final after all stages | verified independently |
| QH04-03 | Two typed consumer domains and eight required scenario groups | Preference and Procedure/Outcome public lifecycle runs with fixture-owned setup/events/query/checkpoints/oracles; no core eval dependencies | verified independently |
| QH04-04 | Temporal/current security and derived-state boundaries | valid vs recorded time; historical revoke; interrupted purge retry, late write, stale index, source/retention and foreign scope checks | verified independently |
| QH04-05 | Retrieval/rendered budget and honest measurement units | relevance/distractor/duplicate/stale candidates; RecallProjected final full-JSON measurement; payload/storage/provider units separate | verified independently |
| QH04-06 | Guarded and permissive host boundaries observable | false/instruction-bearing payload rejected by guarded policy; permissive acceptance exposes semantic weakness; instruction data never executes harness action | verified independently |
| QH04-07 | Evaluator defects cannot preserve success | same count wrong refs/payload, missing lineage, post-review privacy leak and false-empty outage negative probe reports are non-passing | verified independently |
| QH04-08 | Unknown/errors stay unknown and reports stay safe | provider/search/policy/grader errors and missing evidence; secret/raw-error redaction tests for reports and stderr; safe partial evidence retained | verified independently |
| QH04-09 | CLI exit contract and replaceable consumer ports | subprocess exits 0/1/2; report written before exit; bad semantic expected rejection exits 0; offline port substitution executes unchanged checkpoints | verified independently |
| QH04-10 | Current docs/artifacts and independent acceptance | saved v2 corpus/report regenerated offline, old assumptions/fields removed; required checks and two independent completeness100%/no-error reviews | verified independently |
