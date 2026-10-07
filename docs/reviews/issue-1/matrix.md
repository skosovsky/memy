# Issue 1 acceptance matrix

Pre-release verification. Code review and published artifact checks are separate gates.
The source manifest binds the independent reviews; historical reviews do not count.

| Requirement | Code/contract | Executable evidence | Current result |
|---|---|---|---|
| AC1 independent core/BYOT | score.go; unchanged go.mod; docs/design.md; docs/managed-projections.md; testdata/consumer outside core | optional suite runs in a temporary module; core imports no consumer | implemented; final reviewer checking |
| AC2 canonical authority/lifecycle | Recall/RecallProjected existing gates; host/session.go distinct proposer/reviewer | TestCanonicalRecallProjectedRagyContextyForget; TestPersistentCompiledContextManagedForget; core authority/acceptance tests in validate | consumer race PASS; full validation repeat review on final snapshot |
| AC3 exact scope/structural keys | checkpoint.Key and scoped primary/reverse keys | TestCheckpointScopeIsolationAndStructuralKeys; TestCheckpointSinkConformance; remote cross-tenant fixture | backend-final race PASS |
| AC4 score absence/native evidence | Score.Validate/Compare; Projection.Retrieval; Composite native signal preservation | TestScorePresenceContract; TestScoreRankerPresenceOrder; TestCompositeNeverInventsNativeScore; SearchSuite score_presence; malformed score cases | score-final/backend-final race PASS |
| AC5 ownership/full-body budget | cloneProjectedBody detaches evidence; selection by exact ref; JSONPacking whole-body measure | TestProjectedEvidenceCanonicalAndDetached; TestProjectedUnicodeBytesAndEstimatedUnits; existing projection mutation/selection/revalidation tests | score-final race PASS |
| AC6 write/revoke order | synchronous WithDerivedWrite; backend epoch admission | TestForgetBetweenProjectionAndPersistence; TestWriteBeforeForgetAndUnknownCallbackOutcome; remote stale-retry rejection | backend-final race PASS |
| AC7 durable deletion/retry | checkpoint transactional rows/lineage/epoch; registered remote Sink | TestDurableMultiLineagePendingRetryRestore; TestPurgeAllDependentCopiesAndLateOldScopeBatch; TestRemoteCanonicalForgetReceiptLossRecovery | review-fixes race PASS; both read/remote findings fixed |
| AC8 restore fail closed | checkpoint.ReadValidated; docs managed-projections restore ownership | old-checkpoint restore, stale source/cancellation, TestCheckpointMissingAndUnavailableCanonicalLineage, TestCheckpointDeliveryRevalidatesAfterAllStorageIO | review-fixes race PASS; explicit gaps covered |
| AC9 final validation/local + published consumer/backend | make validate; scripts/consumer_checks.py; optional CI lanes | validate.txt completed exit 0; consumer-local.txt/json eight semantic fixtures; backend-final.txt separate process/TCP/SQLite | full validate/local/published/backend PASS |
| AC10 local duplicates | local-duplicates/manifest.json, full copies/diffs, decision.md | byte-for-byte archival checks; normal compilation; no exclusion workaround | archived safely; compilation succeeds |
| AC11 independent reviews | source-snapshot.json | completeness-final.md and correctness-final.md on 5d517d4… (202 hashes) | implementation 100%; no open confirmed defects; full acceptance 95% pending AC9 |
| AC12 release/migration/issue close | docs/migration.md; release-break; author comment; closed issue state | release-break.txt + artifact-verification.json + consumer-published.json | publication verified; migration/closure pending |

## Work items and boundaries

- Spec-first: docs/design.md + managed-projections.md preceded production changes;
  contract-red.txt demonstrates absent API before implementation.
- Durable example: separate canonical and checkpoint SQLite databases, real host
  acceptance, full exact lineage and structural scoped keys; make examples includes
  the runnable example. No canonical storage format migration is needed.
- Distributed boundary: the explicitly implemented optional HTTP/SQLite sink has
  a real process/TCP/restart integration suite, including registered canonical
  Forget recovery. Claims cover that backend's deletion and acknowledgements,
  not unspecified production providers, HA or replicated storage. The selected backend satisfies the explicit backend integration requirement;
  no broader production guarantees or scope transfer are inferred.
- Local consumer lane includes real retrieval document fixture mapping and context
  CompileSnapshot + ConversationCodec roundtrip, preserving evidence in the full
  canonical envelope. The published lane must use the exact new memy tag and
  checks that the entire module graph contains no replace directives.
- Release remains one root Go module. Temporary consumer modules are assembled
  outside the repository, so they do not alter the release module discovery.

## Verification history

The first full validation (`validate-initial.txt`) failed a composite conformance
assertion that incorrectly equated a computed ranking score with native score.
The corrected assertion checks preserved native evidence separately. The current
full validation uses `make validate TEST_FLAGS='-v -race -timeout=30m'`; its first
run tests the changed sources, and release validation may reuse those exact
unchanged results. No tests are excluded. Earlier lint/test failure logs remain
historical evidence, not successful final gates.

Independent review found and fixed checkpoint storage I/O after eligibility
validation and uncertain non-200 HTTP commit outcomes. `review-fixes.txt` covers
the fixes and explicit missing/unavailable canonical lineage cases with race enabled.

## Release decision

Use `make release-break`: the public Candidate/SearchSignal/Ranked score type and
projected output shape change directly, without compatibility wrappers. Canonical
persisted schemas are unchanged. Published consumer verification must fetch the
new tag and reject every module replacement before closeout.

Prepublication compatibility check against the currently published neighboring
modules passed eight semantic race tests (`prepublished-neighbors.txt`). This run
uses a local memy replacement and is explicitly not published-artifact acceptance.
The required no-replacement published lane still follows the release.

Final `make validate TEST_FLAGS='-v -race -timeout=30m'` completed with exit 0.
Formatting, vet, lint (zero findings), every package race test, runnable examples
and ten release-script tests passed. Manifest recheck after examples: 202/202
source hashes unchanged. Local consumer lane also passed on that source snapshot.

## Published artifact

`make release-break TEST_FLAGS='-v -race -timeout=30m'` exited 0 and
published v0.3.0 at fff101ecf3758ecdec015561a1338fc61d202446. Remote tag was
independently queried. The exact new published module passed eight consumer race
fixtures with GOWORK=off and zero Replace directives across its entire graph.
Its Go source fingerprint equals the reviewed local fingerprint:
`d64bf6765c4b0e072f18199fc83670b2d5483beafee76f3eb4ee05748b0121a1`.
See consumer-published.json/txt, release-break.txt and artifact-verification.json.

## CI checkout correction after API publication

The first tag CI local consumer lane cloned default branches, whose context
API lagged the independently verified local and published sources. The failure
is preserved in consumer-ci-local-initial.txt. Portable local clones now select
explicit reviewed refs/commit hashes from consumer/revisions.json and reject
retargeted refs; --siblings still tests actual current host checkouts. This is
a CI bootstrap correction with unchanged Go/public API, requiring release-patch
after repeated validation and independent review of the new source manifest.
Final CI success and new published consumer evidence are still required.
