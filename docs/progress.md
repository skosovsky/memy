# Implementation progress

The goal remains active. This log is not final acceptance or a reduced scope.
No commits, release, publication or neighboring-library changes were made.
All obligations in `.cursor/tasks/task1.md` and the frozen 84-row inventory in
`acceptance.md` remain in scope. No completeness percentage is claimed.

## Current artifacts

Typed engine lifecycle includes Remember, Extract, Revise, Accept, Reject,
Commit, Get, Snapshot, Recall, Project, Forget, Sweep, Fence, WithDerivedWrite
and Reintroduce. Consumer-owned payload/reference/authority/query types,
typed resolver and consolidation providers are implemented.

Memory and SQLite stores have shared transactional conformance tests. SQLite
uses the optional pinned CGO driver, atomic conditional writes and durable
operation receipts. Lifecycle fault tests exercise commit and forget before
and after durable commit, close/reopen recovery and purge retry. Independent
SQLite writer cancellation uses a bounded busy retry and is tested.

Offline reference paths include host authority/source registry/clock/retention,
lagging deterministic index, managed summary sink, eventual and composite
search, typed extractor/merge/projector and explicit reintroduction policy.

The fixed quality corpus runner compares baseline, exact dedup, domain merge
and scripted semantic merge. `docs/quality-report.json` records an actual run;
`docs/quality.md` states methodology and limits. The harmful semantic output
is rejected; originals survive and auto-apply is disabled.

## Latest safety work

- Proposal replay loads immutable content revisions after later edits;
  remembered/revised/extracted replay checks stored scope and identity.
- Commit replay checks operation ID, record ID and visibility scope.
- Runtime document validation rejects unknown states/schema/fields and
  verifies reviewed proposal content digests, including canonical heads.
- Canonical Get checks the stored head identity before serving history.
- Acceptance checks transitive lineage and expiration of any ancestor,
  even before retention Sweep. Derived reads perform the same check.
- Extract rejects an empty codec version or encoded input; an evidence-free
  provider returns explicit missing evidence without a canonical mutation.
- Regression tests cover malformed payload/ID, expired ancestors before Sweep,
  abstention, mutable consumer payload isolation and replay after correction.

## Verification

Pinned toolchain: Go 1.26.5. Reproducible local command prefix:

```sh
env GOTOOLCHAIN=local GOCACHE=/private/tmp/memy-go-build \
    GOPATH=/private/tmp/memy-gopath
```

`go vet ./...`, `go test ./...` and `go test -race ./...` passed after
the replay/digest/lineage fixes, new regressions, validation-helper refactor,
unused-parameter cleanup and docs/file-permission fixes. Earlier fuzz run passed for 3 seconds with two workers; final DoD/fuzz
verification must be repeated after final changes. CI is authored, not remotely
executed.

Full unchanged golangci config was rerun after proposal lifecycle refactors.
Latest output: `/private/tmp/memy-lint-proposal-final.log`, completed exit 2,
reports 21 issues (20 cognitive complexity, 1 nesting). No package complexity,
shadowing, magic number, struct completeness or serialization/style finding
remains in this report. No checks were disabled; lint still does not pass.


## Work remaining before final acceptance

1. Fill every evidence row and audit the original task for omitted obligations.
   Existing acceptance rows remain pending until individually verified.
2. Audit completed wire schemas and migration/import guidance; finish
   error/receipt reference and public examples.
3. Audit typed state snapshots, redaction profiles and versioned host policy
   rechecks. Ensure every promised capability has a runnable reference path.
4. Add/verify remaining multi-session cases: episodic scope, procedural data,
   positive semantic review/commit, authority changes during callbacks and
   malformed capability/visibility metadata.
5. Review quality runner input validation and measurement boundaries; retain
   its negative semantic result and honest byte/cost/latency limitations.
6. Fix full lint without disabling checks; reduce large callbacks and shadowing.
7. Run final formatting, vet, test, race, lint and offline examples; inspect
   actual results instead of treating previous runs as final-state evidence.
8. Only after implementation and evidence are ready, launch the two independent
   acceptance subagents: completeness against the original source, and
   adversarial correctness. Fix findings and repeat final-state review.

Known boundaries: full-record ACL with unsupported field masks rejected;
logical deletion covers registered sinks, not unmanaged backups or delivered
answers; external identity/source decisions require host-valid leases.

## Wire contract work

All seven persisted kinds now have JSON Schema 2020-12 data schemas plus the
versioned document envelope with per-kind definitions. The test-only validator
is pinned to jsonschema/v6 v6.0.2. Runtime core imports only the standard library
(verified with go list -deps); SQLite and test dependencies remain isolated.

TestPersistedLifecycleMatchesWireSchemas compiles all files offline and checks
actual proposals, acceptances, records, operation receipts, epochs, revocation
ledgers and purge receipts, including a payload-free tombstone after Forget.
TestWireSchemaRejectsMalformedEnvelope rejects missing/unknown fields, future
schema, malformed timestamp and invalid epoch. Runtime now rejects omitted
fields and null scalar values before decoding defaults. A corrupt null epoch
cannot reset a forget fence (TestNullRevocationEpochCannotResetFence). Digest,
interval ordering and authority/lineage checks remain runtime invariants beyond
JSON Schema shape validation.

`docs/migration.md` describes explicit version changes, host-approved import via
Remember/Accept/Commit, history mapping and backup/revocation boundaries. README
links the contracts. Two acceptance rows now include inspected evidence links;
statuses remain pending until full obligation-level audit. All mandatory rows
and their denominator remain unchanged.

## Expiry and complexity continuation

Canonical reads previously enforced retention but not the originating proposal
expiry; dependency reads enforced both. A single earlier-nonzero-deadline rule
now covers canonical reads, lineage, projection annotations and Sweep. The new
TestCanonicalProposalExpiryClosesReadsAndPurges verifies unbounded retention
cannot extend finite evidence lifetime, including actual history payload purge.
This is an implemented correction, not a narrowed deletion contract.

The canonical JSON parser is split into object/array/end handling, preserving
duplicate-key rejection and number precision. Get splits canonical head,
revision selection and eligibility checks. Input validation splits source and
exact historical revision checks from transitive current-head validation.
Stored transition validation and adversarial test mutations have smaller
helpers. Existing behavioral tests remain in place.

The identified retention revalidation gap has now been implemented. Concrete
decisions must be stable for unchanged payload and version. Commit, proposal
read/replay, Get, Snapshot, Recall, Project and transitive lineage re-evaluate
policy/version/deadline. A changed decision is stale input; policy outage is
policy denied without payload. Ranking/projection callbacks are followed by
another check, and duplicate reconciliation validates linked inputs.

Sweep evaluates both current and stored deadlines for canonical histories and
immutable proposal histories, including drafts. It collects content-free
retention targets and uses ordinary Forget/purge for canonical payload and
managed sinks. Policy changes do not extend an already stored deadline.
Record.ExpiresAt reports effective evidence/retention expiry separately from
Record.Retention, which preserves the original host decision.

New regression coverage:
TestRetentionChangeInvalidatesAcceptedCommit,
TestRetentionReadChangesAndOutageFailClosed,
TestSweepAppliesShorterCurrentRetentionDeadline,
TestRecallRechecksRetentionAfterRanking,
TestProjectionRechecksRetentionAfterProvider,
TestSweepPurgesDraftsUnderCurrentRetention,
TestDuplicateRejectsInputWithChangedRetention.

Scoped error identifiers were renamed using Go AST declaration/reference
identity, retaining existing data flow. No lint exclusions or rule changes
were introduced. Final verification of this continuation: gofmt produces no file list;
go vet ./..., go test ./... and go test -race ./... all completed successfully.
Independent acceptance has not started.

## Proposal lifecycle refactoring

Remember, Revise and Accept now have separate immutable replay, preparation,
persistence and acceptance helpers. Extract separates request digest, epoch/
replay capture, output encoding, conditional transaction and proposal creation.
Canonical request digest inputs and operation identities remain unchanged;
all proposals and operation references still persist in one atomic update.
The shared preparation derives retention from the actual encoded consumer
payload, checks scope/epoch/sources/lineage, and rejects a zero creation clock
before any successful persistence. TestZeroClockCannotPersistMalformedProposal
covers Remember and Extract and verifies no partial content or receipt remains.
Schema fixture validation uses smaller helpers with the same per-kind checks.
Final verification for this state: gofmt has no output; go vet ./...,
go test ./... and go test -race ./... all completed with exit 0.

## Canonical commit, recall and purge verification

Commit now separates accepted-proposal validation, canonical preparation,
reconciliation and atomic persistence. Related heads and history revisions
must match their scoped key identity. A zero initial recorded clock cannot
persist a malformed canonical record or epoch. Regression coverage includes
TestCorruptRelatedIdentityCannotReconcile and
TestZeroClockCannotPersistMalformedCommit.

Recall now separates backend coverage, canonical candidate selection,
detached ranking and final validation. Snapshot and recall propagate schema,
storage and cancellation errors in lineage instead of interpreting every
stale-input join as an ineligible record. The classifier recursively checks
all underlying causes before accepting known missing/revoked/stale inputs.
An earlier equality-based fix was changed by errorlint's automatic rewrite;
the final unwrap-first implementation remains correct after make fix.
TestMalformedLineageDoesNotBecomeEmptyRecall passed 20 consecutive runs and
subsequent full verification. Projection validates scope/ID/revision and
current retention before exposing a record to its typed provider.

Forget separates expected revisions, scoped current/historical dependency
selection, content-free tombstones, revocation metadata and durable per-sink
acknowledgements. Receipt reads/updates validate scope and operation identity;
concurrent retries merge acknowledgements without replacing a completed ack.
Sink inputs receive detached record handles. Failed receipt persistence no
longer returns a success-shaped uncommitted update. Reintroduction validates
the stored revocation and all purge boundaries before retiring a tombstone.
Zero initial forget time is rejected. Typed-nil composite search backends
reject unsupported capability rather than entering a nil receiver.

Consolidation separates bounded input selection, merge, proposal preparation
and exact source deduplication. Its expiry now includes each input's effective
evidence deadline, even under unlimited retention.
TestConsolidationCannotExtendFiniteEvidenceExpiry verifies the boundary.
TestReviewedSemanticSummaryCommitsAndRetainsOriginals covers a positive
semantic proposal, separate host review/commit, preserved constraints/lineage
and intact originals. The negative semantic fixture remains in the protocol.

The consumer quality runner validates corpus assumptions in both Load and
Run before allocating state. TestMalformedCorpusCannotProduceQualityReport
covers empty inputs, missing references, duplicate source IDs, invalid
intervals and a changed mandatory negative claim without partial results.
The saved docs/quality-report.json was regenerated against this implementation.

TestEpisodeAndProceduralProposalRemainDataAfterReopen adds a persistent
multi-session fixture: a bounded incident outcome is unavailable outside its
interval, a procedural extraction remains proposed, and typed data projection
preserves the host's separately supplied executable prompt.

Final commands for this continuation used Go 1.26.5 on macOS arm64 with CGO:

- make fix: exit 0, strict golangci-lint v2.14.0 reports 0 issues. No exclusions,
  thresholds or enabled checks were changed.
- make validate: exit 0 after the final semantic and bounded-knowledge fixtures;
  includes format, go vet ./..., go test ./..., go test -race ./..., strict lint
  without automatic fixes, and both offline executable examples.
- make fuzz: exit 0; FuzzJSONCodec, two workers, 30-second budget, 822055
  executions, actual completion 31.474 seconds. Later changes added fixtures
  only; the codec implementation did not change.
- go run ./examples/quality -out docs/quality-report.json: exit 0, actual local
  measurements saved; no statistical or commercial-model claim.
- go list -deps for root: only github.com/skosovsky/memy is nonstandard.

The deprecated exhaustruct linter produces a tool warning, not a finding;
strict checks still pass. Linux CI is authored but was not remotely executed.
The 84-row matrix is not yet fully audited. Independent acceptance agents
have not started, and no overall completion percentage is asserted.

## Source-to-implementation audit before independent acceptance

All 84 frozen rows were re-counted mechanically. Evidence is now populated
for 82 implementation/DoD rows; the other two are reserved for the requested
independent acceptance reports. Populated evidence is not a completion claim.

The audit identified and implemented explicit LifecycleCapabilities for engine
valid-time/recorded-time filtering and unsupported field projection. The
transactional store advertises its own lower-level profile separately from
Search visibility. The design was updated before this API addition.

PurgeFailed now has runtime semantics: missing mandatory sinks, explicitly
unsupported purge and invalid acknowledgements produce durable failed
receipts; unavailable participants remain pending. Repair and retry of the
original operation can complete cleanup without advancing the revoke epoch.
The fence persists throughout failure and reopen. External error text is not
saved. TestFailedPurgeRemainsFencedAndCanRecoverAcrossReopen covers all three
failed conditions with actual SQLite reopen and repair.

Additional authoritative behavioral evidence:

- TestForgetSelectorsRespectExactAuthorityBoundary exercises record, source,
  subject and scope selectors against A's independent sources and B in the same
  physical store. B survives and remains inaccessible to A.
- TestRestrictedRecallAndProjectionNeverExposeFieldsToPolicies proves field
  restriction prevents ranker/projector invocation and excludes private content
  from serialized recall/export. Record/Recall metadata has explicit snake_case
  JSON field names, consistent with projection/provenance exports.
- TestProjectionRechecksAuthorityAfterConsumerCallback rejects returned output
  when the host policy changes during projection.
- TestProjectionCacheIdentityBindsReadContext separates actor, purpose, host
  policy version and representation version even when output content matches.
- TestManagedWriteCannotRacePurgeToResurrectArtifacts runs against both memory
  and SQLite: a managed writer holds canonical exclusion, an attempted revoke
  times out, then real completion/purge removes staged index and summary data;
  delayed acknowledgements and old-fence writes cannot resurrect artifacts.
- TestCancellationDuringVisibilityWaitPreservesCanonicalCommit observes the
  real index blocking select before cancellation, then verifies canonical state
  and pending index input survive. It does not merely pass an already-canceled
  context into the engine.
- TestConsolidationQualityGatesPreserveOriginalState exercises low utility,
  empty output, missing evidence, input/output byte budgets and intact originals.
- TestSourceOutageCannotBecomeSuccessfulCanonicalCommit verifies explicit
  source-unavailable failure without a canonical revision.
- ExampleJSONCodec is a second executable package example for consumer-owned
  canonical codec values.

Verification: make fix completed with strict lint 0 issues. The first final
make validate passed vet/test/race but lint failed with a global runner lock;
it was not counted as a successful gate. A read-only process check confirmed
no live golangci-lint process remained. The same make validate was retried and
completed with exit 0: format, vet, full tests, race, strict lint 0 issues and
both executable examples. This was a terminal command failure, not a restarted
job based on an observation timeout. The final log is saved with review evidence.
The offline quality report was regenerated with this code; measurements remain
single-run synthetic observations. No commits, release or remote CI execution.

The next required step is the two independent acceptance agents against this
source snapshot. Completeness must still be proven against the original task,
including any missed obligation; correctness review must actively reproduce
adversarial cases. No 100% claim has been made.

## Independent acceptance round 1 and remediation

Both independent agents inspected unchanged round 1 snapshot
bca4d7119843f283d89f1672606016031e54e94b8f800c10755d9f3cf6be741d.
Completeness found the omitted common non-Store conformance obligation: BOOT-13
was appended, preserving all 84 original rows and raising the denominator to 85.
Reported completeness before remediation: 81/85 (95.29%). Correctness actively
reproduced six defects despite fresh passing normal/race tests; full reports are
preserved under docs/reviews. No report was shared between independent agents.

Contract clarifications preceded remediation. Added exact source revalidation
on reads/transitive lineage and after read callbacks, instant-based acceptance
comparison, history-key binding, case-sensitive unknown wire-field rejection,
complete content byte budgets and honest Index coverage without a minimum.
Regression tests cover both stores and separate output-gate invocation from
input-budget rejection. Common suites now exercise all implemented port types;
reference fixtures preserve consumer types. Deterministic ranker/fixed retention
have no injected dependency outage, explicitly recorded by the callback profile.

External original repro suite rerun passed locally. New reference conformance
and root regressions passed in go test ./.... Full gates and the next independent
snapshot are pending; no claim of final acceptance is made at this stage.

Round 2 local make validate completed with exit 0: format/vet/full test/race,
strict pinned golangci-lint v2.14.0 (0 findings, unchanged lint configuration),
and both offline examples. Evidence: docs/reviews/checks-source-integrity.log. Regenerated
quality-report.json from the actual current consumer protocol. Initial direct
lint used an unwritable default cache; later attempt hit a terminated global
runner lock. Read-only process check found no live runner; the completed retry
used the writable configured cache. Neither failure was counted as a pass.

## Independent acceptance round 2 and final deadline remediation

Completeness independently confirmed 83/83 implementation obligations (100%),
with the two final review gates still pending (83/85 overall, 97.65%). Correctness
independently reproduced closure of C1–C6 and confirmed new C7 (numeric byte
arrays accepted in base64 fields) and C8 (expiry crossed during source I/O,
including accepted expired proposals and committed expired ancestors). Reports
preserve the unchanged 89-file snapshot 9e13403fe095478a6d41167f769391674d5dc025b53eb395e0e4df6613aa4e14.

Contract clarifications preceded final gates. Added canonical base64 shape checks
excluding structural RawMessage, and callback-free final deadline traversal over
all selected roots and exact transitive dependencies. Get/Snapshot/Recall/Project,
Accept and Commit enforce post-I/O effective deadlines; Commit includes bound
acceptance expiry. New tests cover memory/SQLite, numeric byte arrays/newlines,
read/accept/ancestor-commit crossings and an earlier finite snapshot record that
expires during later unlimited-record source I/O.

The primary independently reproduced C9: Go normalizes decimal-comma recorded_at
contrary to RFC3339 wire format. requiredTime now enforces timestamp shape,
with regressions checking executable schema and runtime reject comma and invalid
timezone offsets. C9 is explicitly attributed to the primary, awaiting independent
closure; it is not retroactively attributed to the round 2 reviewer.

Fresh full go test ./... passed, and the external original C1–C8 audit module
passed locally after final gates. Round 2 make fuzz completed on its frozen
snapshot (108929 executions, 31.568s, exit 0); JSONCodec itself is unchanged by
this remediation. Final full gates and independent re-review are still pending.

Round 3 full make validate retry completed with exit 0: format/vet/tests/race,
strict pinned lint 0 findings, both offline examples. The first final run passed
runtime checks but failed golines on one test closure; formatting was fixed,
without lint changes, then the complete command passed. Evidence:
docs/reviews/checks-wire-deadlines.log. External round 2 suite plus primary timestamp
repro passed on the current fixes (exit 0, docs/reviews/external-primary-wire-deadlines.log).
Quality report regenerated from this actual current implementation. Snapshot
and independent final re-review follow; C7–C9 closure is not yet asserted by the
primary on behalf of an independent reviewer.

## Round 3 independent findings and round 4 remediation

Round 3 completeness confirmed 83/83 implementation obligations, with final
review gates pending (83/85 overall). Correctness independently closed C1–C9
and confirmed C10: a renewable authority decision could leave Accept returning
its expired initial lease; and C11: updating a target used transitively in its
own lineage immediately invalidated the new record. C11 originated with the
primary and was independently reproduced, not attributed to the reviewer.

Contract clarification preceded remediation. Accept now checks the bound
initial decision expiry after source I/O and reauthorization; failed review
leaves the proposal Proposed and allows a fresh review. Commit traverses the
full prospective lineage and rejects any occurrence of its target before
persisting. Transaction rollback preserves staged Supersede changes.
lease_lineage_regression_test.go covers memory and SQLite, fresh lease retry,
Append/Supersede rollback, safe alternate target and identical receipt replay.

Round 4 make validate passed (exit 0): format, vet, full tests, race, pinned
strict lint (0 findings), both offline examples. Evidence: checks-authority-lineage.log.
The external round 3 adversarial module plus primary lineage regression passed
(exit 0), external-authority-lineage.log. Independent closure of C10/C11 remains pending.

## Round 4 independent findings and round 5 remediation

Completeness confirmed 83/83 implementation requirements; overall 83/85
reserved review gates pending. Correctness independently closed C10/C11 on
both stores, then confirmed C12 (P2): canonical runtime lineage could omit
accepted proposal dependencies after privileged storage corruption. The
reviewed proposal digest remained intact, allowing derived content delivery
after the ancestor source was removed. The primary reran the independent
C12 public-API repro and observed failure on both stores before remediation.

Documented the mandatory reviewed-lineage subset invariant before the fix.
Non-revoked record validation now requires each exact Proposal.Lineage ref
to be present in Record.Lineage. Additional reconciliation dependencies remain
allowed; content-free revoked tombstones retain their separate contract.
TestCanonicalCannotDropOrReplaceReviewedLineage exercises dropped lineage,
record substitution and revision substitution on memory/SQLite and all four
delivery paths, preventing provider invocation or successful content delivery.
The external adversarial suite now passes. Final lint and round 5 acceptance
remain pending.

Round 5 final make validate completed exit 0 with every gate, both examples
and pinned strict lint 0 findings. Initial full runtime checks passed but lint
was blocked by another machine-wide runner; serial-runner retry then found
five shadowed names in the new regression, which were renamed. The complete
command was repeated successfully with GOLANGCI_LINT=/private/tmp/memy-lint-serial.
This wrapper executes the same pinned Go-run linter and adds only
--allow-serial-runners; .golangci.yml and enabled checks are unchanged.
Evidence: checks-reviewed-lineage.log; failed attempts retained as checks-reviewed-lineage-initial.log
and lint-reviewed-lineage-initial.log. External original adversarial suite passed exit 0,
external-reviewed-lineage.log. Quality report regenerated from the actual current CLI.

## Round 5 acceptance and managed write remediation

Both reviewers used explicit /opt/homebrew/Cellar/go/1.26.5/libexec/bin after
an external machine PATH change to Go 1.27.1. Earlier primary round 5 report
records Go 1.26.5; nevertheless repeated full DoD with explicit pinned PATH
passed exit 0 (checks-reviewed-lineage-pinned.log). No source formatting changes were
made for Go 1.27.1, and aborted unpinned reviewer checks were not counted.
Completeness confirmed all 83 implementation obligations and retained 85 rows.
Correctness independently closed C12, including positive extra Duplicate
dependencies, but confirmed C13: a managed callback could start after expiry
crossed during final authority I/O. The primary independently reproduced C13
on both stores under pinned Go 1.26.5 before remediation.

Contract clarified before code: WithDerivedWrite checks all exact transitive
deadlines after final reauthorization immediately before callback entry.
Added that callback-free gate; external effects after callback entry retain
their declared host boundary. New AAA regression covers memory/SQLite, direct
and transitive evidence, healthy pre-expiry entry and no callback at expiry.
Existing managed write/revoke concurrency regression passed unchanged. Full
DoD, external module and independent closure remain pending for round 6.

Round 6 explicit pinned Go 1.26.5 make validate completed exit 0: format, vet,
full tests/race, strict pinned lint 0 issues (same serial-runner wrapper), both
offline examples. External round 5 adversarial suite passed exit 0, including
original C13 reproduction. Evidence: checks-managed-writes.log and external-managed-writes.log.
Quality report regenerated under explicit pinned PATH. Independent round 6
closure and final state confirmation follow; no closure is asserted for reviewers.

## Final acceptance bookkeeping

Round 6 completeness independently confirmed all 83 implementation obligations
against the task and unchanged 85-row inventory. Correctness independently
closed C13, reran all previous external adversarial regressions and found no
new confirmed defects in the executed bounded audit. The C1–C13 independent
closure chain is indexed in reviews/final-audit.md. Both review gates now have
actual independent reports, bringing requirement evidence to 85/85 (100%).

Only README, acceptance and this progress bookkeeping change after round 6;
implementation, tests, schemas, design, dependency and lint hashes remain
unchanged. Final exact snapshot confirmations are saved by both reviewers in
reviews/completeness-final.md and reviews/correctness-final.md before the goal
is marked complete. No source mutation or new runtime result is implied by
this final bookkeeping.

Final full primary DoD: explicit pinned Go 1.26.5, make validate with the same
pinned strict linter using the serial-runner wrapper, exit 0. Both independent
reviewers additionally ran fresh test/race -count=1, and completeness ran
vet/format/examples and independent consumer probes. FuzzJSONCodec earlier
completed 108929 executions in 31.568s; unchanged codec hash supports that
specific result, without claiming fuzz coverage of later lineage/deadline code.

Verified environment: macOS arm64, Go 1.26.5, CGO SQLite. Linux CI configuration
is delivered; no remote run is claimed. SQLite purge is logical, not forensic.
Field-restricted profiles are unsupported and rejected. External IAM/source
leases and callback effects are host boundaries. Quality metrics are fixed
scripted offline fixtures and single-run timings, not a commercial-model or
statistical performance benchmark. No commits, releases or publication.
