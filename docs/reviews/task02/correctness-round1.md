# Task 02 — independent correctness review, round 1

Reviewer: task02_correctness; did not implement this task. This is an interim review of a changing worktree, not final acceptance. No code changes or commits were made by this reviewer. Review preserves BYOT, explicit host scheduling, clear API break and library boundaries.

## Scope and evidence

Read `.cursor/task/02-bounded-storage-and-concurrency.md`, the storage/lifecycle design contract, `store/memory`, `internal/kv`, SQLite transaction and OS file-fence code, membership persistence/validation, bounded purge job/state transitions, bounded Sweep, Recall/Project final revalidation and retention handling. Examined native batch/corruption/graph fixtures and measurement instrumentation.

Independent runs passed:

- `go test -race ./store/memory ./store/sqlite ./internal/kv` — see correctness-round1-adapters.txt.
- `go test -race -run 'Test(Sweep|Purge|BoundedPurge|.*Callback|.*Managed|.*Snapshot|ConsolidationReads)' .` — see correctness-round1-lifecycle.txt.

Commands used `GOCACHE=/tmp/memy-go-build GOPATH=/tmp/memy-gopath`. Temporary probes use Go overlay, do not modify implementation/test files. Exact probe source is retained in correctness-round1-probes.go.txt. Reproduce by copying it to /tmp, creating an overlay mapping `<absolute repository>/review_overlay_test.go` to that file, then `go test -overlay <overlay.json> -run '^TestReviewFanInPurgeCost$' -v .` (or the relevant individual probe). Probes assert the faulty behavior observed in round 1, so a fixed implementation should reject/change that behavior; these are reproduction artifacts, not permanent acceptance tests.

Performance measurement files and full race gate were still being completed by the implementer. This report does not certify every requirement or the final code state.

## Findings

### R1 — authority policy rollout can strand an active Sweep (high; independently verified closed for this round)

Initial Sweep plan included the authority `PolicyVersion` in the durable request digest. A legitimate same-actor policy rollout changes that digest: original operation resumes with ErrConflict; another operation receives ErrRevoked from active_sweep. All normal reads/writes remain fenced and no public operation retires that sweep ownership. Current authorization can be valid while the scope remains unusable indefinitely.

Implementation changed during this review: plan now binds actor/scope/purpose/operation identity, while each call still authorizes and reauthorizes current policy. Added regression `TestSweepCanResumeUnderCurrentAuthorityPolicy`. Independent closure run verifies denial rejects continuation, a newly authorized version resumes/releases the fence, and old completed replay leaves a newer active fence intact (correctness-round1-closures.txt). The original overlay reproduction no longer sees ErrConflict after this change; independent targeted lifecycle race run passes.

### R2 — a corrupted done job reports completion while its own fence remains active (high; independently verified closed for this round)

`Sweep` previously returned Complete for job.Phase==done before checking active_sweep. `validDocument(sweepJobDisk)` accepted this phase with existing active ownership. Probe TestReviewSweepDoneCorruption starts Limit=1 pass, changes its durable job phase to done through privileged fault injection, and observes Complete=true with no error, while Fence still returns ErrRevoked. Durable progress was accepted in a state impossible for an atomic legitimate completion.

Implementer added a done/live-pointer check and regression `TestSweepRejectsFalseCompletedJobWithLiveFence`; independent targeted lifecycle race run passes. Independent regression with race returns ErrSchema for this contradiction; closure probe also verifies legitimate completed replay and an unrelated newer active pass (correctness-round1-closures.txt). These checks must be rerun against final code.

### R3 — fan-in membership traversal repeatedly decodes whole child adjacency (high; open)

`purge_job.go` creates `map[RevisionRef]membershipEvidence` per advancePurge call. `membership.go:validateMembership` reads/decode-validates the whole canonical child revision and reconstructs maps of all its Sources/Lineage when processing each edge. A child with n parents encountered across bounded calls is decoded n times, each decoding O(n) adjacency. Even within one operation, this produces O(n²) bytes/allocation work for O(n) vertices/edges. Small explicit budgets cannot fix the total complexity; the contract requires traversal cost to correspond to visited vertices/edges.

Reproduction uses existing seedCostLineage (two children each with n independent parents), native full Forget with Limit=1 and private workcost counters:

| Parents n | Edges | Decoded documents | Decoded bytes | Copied bytes |
| --- | --- | --- | --- | --- |
| 100 | 200 | 6,097 | 2,875,289 | 2,993,982 |
| 200 | 400 | 12,097 | 7,242,288 | 7,477,481 |
| 400 | 800 | 24,097 | 20,536,486 | 21,004,679 |

Probe TestReviewFanInPurgeCost passes because it measures current behavior; native purge completes. Document count alone conceals the growing decode size. Required closure: a structural high-degree/fan-in regression that measures bytes as well as row counts and proves cost grows with adjacency actually visited, including small budgets/reopen. Do not bypass canonical binding validation or replace this with wall-clock thresholds.

### R4 — Sweep omits exact revision-key binding (high; open)

`retention.go:expiredRecord` decodes a canonical record and checks Scope, but does not verify `entry.Key == revisionKey(stored.ID, stored.Revision)` before evaluating retention and choosing its head. Other bounded canonical scans verify this binding. A visited foreign revision key can trigger purge of a different canonical record and still certify the pass complete without a schema error; malformed content remains in the scanned canonical range.

Probe TestReviewSweepForeignRevisionKey copies a valid expired record envelope under `record/000-foreign/00000000000000000001`, then runs bounded Sweep to completion. It reports no error and the foreign row still retains 1,431 bytes. This is privileged corruption injection, not an untrusted public ID exploit; the violation is acceptance of a malformed visited canonical binding, which should fail closed before mutation. Required closure: exact key comparison before policy evaluation/target selection, regression proving ErrSchema and no committed deletion/progress from that page.


### R5 — purge phase can bypass canonical cleanup or strand progress (high; open)

`validPurgeJob` checks enum values and broad counter ranges, but does not bind phase/counters to the corresponding durable receipt. Starting a record purge with Limit=1 leaves Queued=1, Clean=1, Next=1. Changing only job.Phase from expand to emit is accepted. The next native Forget sets CanonicalComplete and completes the operation while the original canonical revision (1,431 bytes) remains. Changing Phase to done instead causes repeated successful RevocationCommitted responses with zero progress and a retained active fence.

Reproduced by TestReviewPurgeEmitCorruption and TestReviewPurgeDoneCorruption. The former certifies false cleanup; the latter silently accepts an impossible stalled state. Privileged corruption injection is the stated review fault model, not a public selector exploit. Required closure: validate stage/counter/receipt invariants before effects (cleanup exhaustion before emit/ack/done; done must correspond to complete receipt and retired ownership), reject impossible combinations with ErrSchema, and retain atomically frozen canonical state/fence when rejecting. Schema definitions and runtime validator should express the same reachable-state constraints where JSON Schema permits them.

## Additional review observations and limits

Present membership hits are checked against canonical revisions, and membership writes/deletes are transactional. Complete absence of an index entry after arbitrary privileged Store modification cannot be detected from present hits alone. I do not claim arbitrary privileged index deletion is a public API defect without an explicit adapter integrity guarantee or a normal-write reproduction. A manifest/checksum/rebuild design would be needed for a stronger fault model; full-scope fallback scans would conflict with the requested cost contract.

Read-return checks occur in fresh FencedView transactions; trusted Ranker/Projector input already delivered to the callback remains an explicit boundary. Managed external writes hold exact-scope fencing; Unix flock ownership, independently opened handles and kernel cleanup avoid a process-local-only guarantee. SQLite keeps short global writer serialization, as permitted. None of these observations prove correctness outside tested/reviewed scopes.

Decision: task 02 not accepted in round 1. R3, R4 and R5 remain confirmed open findings; R1/R2 closure independently verified for this round and needs final-state verification. Full requirements, final race/examples and final performance evidence still need both reviewers' acceptance.
