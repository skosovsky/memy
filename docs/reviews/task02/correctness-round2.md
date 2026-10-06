# Task 02 — independent correctness review, round 2

Reviewer: task02_correctness. Did not implement this task; made no implementation edits or commits. This round checks closure of R3/R4/R5 and the new membership-proof architecture. It is not final acceptance of the full task or its performance evidence.

## Source identity and scope

Reviewed a changing worktree based on task01 commit 9f7b50f. File SHA-256 snapshot: correctness-round2-snapshot.sha256 (production Go, schema definitions, design contract and affected permanent regression tests). Measurements/probes below used the corrected proof implementation, including the final acknowledgement Resume/Next guard. The full race run started before that last guard; no duplicate full race was launched by this reviewer.

Read membership relation construction, exact canonical binding, counted Merkle tree witness build/shape/verification, durable per-operation evidence, bounded purge stage/receipt/counter validation, acknowledgement finalization and Sweep exact-key gate. Examined wire shape definitions and the contract's distinction between JSON shape constraints and supplementary semantic/cross-document runtime invariants. Reviewed trusted raw Store access as privileged, not an untrusted public host port.

## Findings and closure

### R3 — repeated full child decode: closed in reviewed scope

Source and lineage edges now carry proofs whose leaf binds scope, exact child revision, relation and match ID. Distinct leaf/branch/counted-root domains avoid ambiguous tree composition. Odd tree sizes require the duplicated final sibling; count/position/depth are checked. Depth is bounded to 64, and verification performs fixed-size hash operations per level.

On first membership validation the engine loads and validates the exact canonical revision, derives its complete deterministic unique relation root, and persists a content-free purge-evidence document bound to scope/operation/RevisionRef. Later bounded calls use that root to validate edges. Atomic canonical membership construction plus the active scope fence provides the root's lifetime; the index cannot assert an arbitrary relation without matching a canonical root. No host-wide unbounded cache or worker was introduced.

Independent source-Forget probe uses the same high fan-in fixture as round 1 and Limit=1:

| Parents n | Edges | Decoded documents | Decoded bytes | Copied bytes |
| --- | --- | --- | --- | --- |
| 100 | 200 | 6,097 | 2,118,113 | 2,345,218 |
| 200 | 400 | 12,097 | 4,260,180 | 4,738,319 |
| 400 | 800 | 24,097 | 8,597,846 | 9,604,719 |

This removes the earlier quadratic child-body decode term. Counts alone were unchanged, so byte counters remain necessary. Proof strings increase edge storage with logarithmic tree depth; the maximum depth provides an explicit constant bound. This review does not claim proofs certify missing edges after arbitrary privileged index deletion.

Adversarial overlay probes reject a forged edge proof with valid witness shape, a corrupted reused evidence root, and a foreign evidence scope with ErrSchema; canonical history is unchanged. A SQLite reopen after child root computation subsequently validates fan-in edges and completes all 12 record IDs. The first corruption attempt targeted an unused independent record cache; its lack of later validation did not prove a defect. The recorded final probes target a reused child cache.

### R4 — Sweep revision-key binding: closed in reviewed scope

expiredRecord now compares entry.Key against revisionKey(stored.ID, stored.Revision) before retention evaluation/target selection. TestSweepRejectsForeignCanonicalRevisionKey independently passes with race: malformed visited key gives ErrSchema; no history deletion and no initial active-sweep fence from the rolled-back page. Existing malformed-key reproduction no longer has its observed false-completion behavior.

### R5 — purge stage and receipt consistency: closed in reviewed scope

Premature emit/done are rejected against cleanup progress and the durable receipt before effects. During this round an additional acknowledgement-state variant was found: a legitimate first chunk of a two-record purge (ack/resume=emit/Clean=3/Next=2/Queued=2) could be changed to resume=done and falsely complete after only one emitted record ID. It was reproduced before correction.

The implementer added ResumeDone => Next==Queued+1 and ResumeEmit => Next<=Queued to the job validator. Reading a job before advancement and again in finalization applies this guard. Independent final probe now observes ErrSchema before any sink callback (zero calls) and retains the active fence. Early phase corruption permanent tests also independently pass with race. Reachability checks remain semantic runtime obligations rather than claims that JSON Schema alone compares two persisted documents.

## Independent checks

Using `GOCACHE=/tmp/memy-go-build GOPATH=/tmp/memy-gopath`:

- `go test -race -run '^(TestPurgeContinuationRejectsCorruptFenceAndProgress|TestSweepRejectsForeignCanonicalRevisionKey|TestProofBindsEveryRelationAndTreeShape)$' . ./internal/membershipproof` — PASS, correctness-round2-targeted.txt.
- Own source-Forget fan-in probe for 100/200/400 parents — PASS with complete native purge and private work counters, correctness-round2-fanin.txt. Source in correctness-round1-probes.go.txt; run only TestReviewFanInPurgeCost against this snapshot.
- `go test -race -overlay <temporary overlay> -run '^TestReview(PrematureFinalAckResume|ProofCorruptionFailsClosed|ProofEvidenceSurvivesReopen)$' -v .` — PASS, correctness-round2-closures.txt. Exact source in correctness-round2-probes.go.txt, overlaid onto a nonexistent root review_round2_test.go; implementation/test files are not edited.

The round-2 probes are reviewer artifacts, not permanent production tests. Full race, all examples/vet, final baseline/after performance evidence and independent cross-handle idempotency are owned by the implementation/final acceptance pass. These must refer to the final code state, including the last Resume/Next guard.

Decision: R3/R4/R5 closed in this reviewed scope. Ошибок в проверенном объёме после исправлений не обнаружено. Whole task02 final acceptance remains pending; this round must not be used as a completeness percentage or a final-state global correctness certification.
