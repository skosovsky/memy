# memy design — contract v2

Status: implementation contract. No automatic consolidation,
network calls, scheduler, prompt execution, or permission escalation is part of
the runtime. Knowledge is data, including knowledge describing procedures.

## Ownership and packages

`memy` owns lifecycle metadata, small scoped envelopes, receipts and typed ports.
The consumer owns payload P, source reference R, extraction input I, search
query Q, projection output O and authenticated authority A. A payload never
assigns identity, scope, policy or permissions. No universal message/agent type
and no mandatory map[string]any representation is introduced.

The root package and its private helpers have only standard-library dependencies. `store/memory` and
`store/sqlite` implement the same transactional port. `reference` provides
deterministic offline authority, source, clock, index, projection sink and
scripted provider adapters. `conformance` supplies reusable suites for Store and every implemented port.
Consumer integrations are typed ports, not dependencies on neighboring ai-libs.

Go 1.27.1 is pinned in go.mod and CI. SQLite uses
github.com/mattn/go-sqlite3 v1.14.52 (isolated in store/sqlite), database/sql,
CGO and a C compiler. Supported verification targets: macOS arm64 locally and
Linux amd64 CI with CGO. Builds without CGO may use core and memory adapter;
they must not claim a functional SQLite backend. Driver requirements:
https://github.com/mattn/go-sqlite3#installation.

## Scope and authority

Scope is an exact tuple (tenant, namespace, subject), encoded structurally, not
by ambiguous delimiter concatenation. The authenticated host calls the engine
with A and Scope; an Authority[A] port evaluates Action and Purpose and returns
a versioned decision. Denied and missing records are deliberately indistinguishable
to an unauthorized reader. Required authority failures fail closed on every
read and mutation. No string comparison replaces the authority decision.

The engine rechecks policy before transaction completion and before returning
payload. A host decision includes version, actor, scope, allowed fields and
expiry, and acceptance binds those values. Policy changes require stale
acceptance to be reviewed anew. Authority is external: its decision is valid
at its check's linearization point, and a host requiring atomic cross-system
revocation must provide a decision lease covering the operation. There is no
distributed transaction with arbitrary IAM.

Record scope limits every read, source, derived artifact and query. The v2
reference profile rejects field-restricted records/requests as unsupported;
the consumer can split records. A future safe field-projection adapter must
authorize the input before extraction, ranking, summarization or export;
post-hoc hiding of fields is insufficient. Rejection is an implemented
capability boundary, not implicit full-field permission.

## Transactional store

Store is a low-level trusted port: consistent View, scoped atomic Update and
explicit exclusion through FencedView(ctx, scope, callback). Each callback gets a transaction-local Bucket
of versioned binary values. Get/Put/Delete/Scan use detached copies. Store
callbacks are synchronous and may not call the same store recursively.
An update commits the entire callback or rolls it back, including errors,
panics and canceled context detected before commit. No background goroutines
or unbounded queues are created. The store does not implement ACL: the engine
does so before touching a bucket. Direct store access is privileged host access.

Put/Delete require an expected per-key version; absent is version zero. Failed
CAS is ErrConflict. Delete leaves a version tombstone so deleting/recreating
a key cannot cause ABA. Versions are monotonically increasing and overflow
is rejected. Scan excludes deleted values and returns bounded ordered prefix pages.
Values cannot be nil or empty; arbitrary nonempty bytes are supported.

In-memory is a process-local reference with serialized transactions and
context-aware lock acquisition. SQLite uses BEGIN IMMEDIATE for mutations and WAL read transactions for reads,
WAL, synchronous FULL, busy timeout, foreign key checks and schema version
verification. It serializes writers across independently opened connections.
Driver busy waits are limited to 25 ms; the adapter retries busy/locked BEGIN
with context checks and a maximum five-second contention window. It never
turns a deadline into a successful transaction or an empty result. Database/sql
serializes the writer handle and permits bounded pooled WAL reads, passing
cancellation into the driver.
The transaction stores scoped keys, their versions and values together.
Receipts and lifecycle state are updated in the same transaction as records.
Rollback after fault injection must survive reopen. Post-commit fault means
an unknown outcome; retry with the same operation identity recovers the receipt.

Store capabilities explicitly report atomicity, conditional writes,
durability and schema version. The engine refuses a store missing required
atomic/CAS capabilities. Physical disk failure, disk encryption, off-host
backups and OS security are host concerns. Logical purge makes deleted data
inaccessible through the library; it does not promise forensic erasure of
SQLite journals, filesystem snapshots, or unmanaged backups.

Engine.Capabilities reports the store profile and engine-owned ValidTime,
RecordedTime and FieldProjection capabilities. Both temporal predicates are
implemented over canonical revision history for every admitted transactional
store. FieldProjection is false in the current profile; field-restricted authority decisions
are rejected before consumer policies see content. Search visibility remains
a separately advertised Search capability.

The implemented [remediation contracts](remediation-contracts.md) define the codec,
projection identity, cursor wire and confirmed partial Sweep reporting.
[Decisions](remediation-decisions.md) record the scope and measured tradeoffs.

## Codecs, keys and idempotency

Codec[T] supplies versioned Encode/Decode. Stock JSONCodec[T] uses json/v2,
orders string map keys and admits valid Unicode scalar strings only. Encode
checks strings/keys and custom emitted JSON/text before encoding/json replacement;
Decode rejects invalid UTF-8 and unpaired surrogate escapes. Generic numbers,
including source references and nested maps/arrays, decode as json.Number with
lexemes retained. Typed numeric fields follow their declared Go type. 1/1.0/1e0
can have distinct identities; arbitrary dynamic numeric types are not recovered.
The supported domain excludes NaN, infinity, cycles and duplicate object keys;
custom callback determinism is a host obligation, not runtime certification. Validation rejects malformed/unknown versions. Encode outputs
are canonicalized before digesting; the digest binds envelope metadata,
source revisions, scope, codec versions, lineage, epochs and payload. A
consumer codec must obey the deterministic representation contract.

Remember, Extract, Revise, Commit, Forget and Reintroduce have a nonempty caller
operation ID. Accept and Reject use the exact proposal ID/digest as their
natural review identity; acceptance additionally binds its revision and host
decision. They do not create canonical revisions. The scoped operation
ledger binds action plus canonical request digest and stores a content-free
receipt. Same identity/same input returns the receipt; identity/different
input returns ErrConflict. Internal receipt history never stores forgotten
payload. A repeated revoked operation cannot resurrect data. Exactly-once
arbitrary external effects are not promised. Extraction input/job identity
and epoch are captured before running a provider and checked before proposals
are persisted.

## Proposal and acceptance

Remember and Extract produce persistent proposals, separate from records.
Each proposal has ID, revision/digest, expiry, typed payload and provenance,
source revision references, extractor identity, evidence and epoch snapshot.
Providers return typed suggestions only; the engine stamps host-owned scope.
Proposal state is proposed, accepted, rejected or expired. Expiry is checked
against the injected clock, not a caller-controlled timestamp.

Accept/Reject is a distinct host-authorized operation. Acceptance binds one
proposal's digest/revision, actor, scope, authority policy version and expiry.
Commit accepts an acceptance identity, expected record revision and reconcile
decision; it revalidates all source references, policy, proposal expiry and
revocation fences in the canonical transaction. Changed proposal, policy,
source revision or epoch is stale. No unaccepted sibling proposal commits.
Accepted proposals alone do not alter canonical memory. Source verification
is a port: unavailable is an error, not valid evidence.

## Canonical revisions and reconciliation

Records have stable IDs and append-only revision identity. Each revision holds
payload, provenance, observed-at, recorded-at, valid interval and state.
States: active, superseded, conflicted, revoked. Proposals are a separate kind.
The typed domain resolver supplies claim mapping and the chosen reconcile
policy: append, duplicate, supersede or conflict. Core never infers identity
from embeddings or automatically prefers the latest observation.

Resolver[P,R,K] accepts a consumer-owned claim key and an authorized Snapshot;
reference.ResolverFunc supplies an offline policy adapter. The host resolves
once, creates a CommitRequest from its CommitTarget and retains that request
for retries. Re-running a resolver against newer state is a new reconciliation
decision and must use a new operation identity, rather than silently changing
the input of an existing operation.

Proposal content revisions are immutable and stored separately from lifecycle
state. Operation replay returns its exact original proposal revision, even
after later edits. A replayed old digest still needs current acceptance and
source checks before commit. Forget removes every affected proposal revision;
the operation ledger contains references and content-free receipts only.

Canonical lineage includes duplicate inputs and consolidation inputs. Reads
validate the full dependency chain, including present status, exact source
revision, current revocation ledger and retention deadline. Managed purge
computes transitive dependencies across current and historical revisions.
Proposal expiry remains a canonical evidence deadline after acceptance. Reads,
lineage checks and Sweep use the earlier nonzero proposal/retention deadline;
Record.ExpiresAt and projection report this effective expiry; Retention retains
the original host decision separately. Consolidation carries the effective
deadline of its actual lineage inputs into its proposal, including finite evidence lifetime
under otherwise unlimited host retention. Retention is a record-level boundary:
expiry of any retained revision revokes
that record ID and dependent artifacts. A corrected record needing an
independent retention lifetime uses a fresh ID under explicit host policy.

Expected revisions guard every affected record. Duplicate links preserve
source lineage without destroying originals. Supersede and conflict links
are committed atomically with the new revision; resolution records the host
policy and basis. Invalid intervals, incomparable claims and unresolved
conflicts have distinct errors. Intervals are half-open [from,to); a known
interval may have open endpoints. Missing interval means unknown and is
excluded by a valid-as-of predicate unless explicitly requested as unknown.

Recorded-as-of queries evaluate state transitions known at that timestamp,
including supersession/conflict; valid-as-of selects applicable payloads.
Privacy revocation is always evaluated at the present ledger epoch and cannot
be bypassed with a historical timestamp. Revocation purges historical payloads.
Clock must be monotonic for recorded time within a scope; otherwise fail or
advance the ledger timestamp by one nanosecond beyond the previous timestamp.
This is logical monotonic recording order, not a claim that the regressed wall
clock advanced. Revision identity remains explicit ordering metadata.

## Recall and derived views

Get reads canonical state. Search[Q] is a metadata-only, exact-scoped host port;
query Q and semantic relevance remain consumer-owned. Candidate scores are
ranking inputs, never truth, authorization or tool permission. Core validates
canonical state, ACL, temporal predicates, expiry, sources/lineage and current
revocation before a ranker or projector receives any payload. Ranker and output
selection can reorder/remove permitted exact revisions, never add a revision or
replace canonical payload. Expensive callbacks execute outside transactions;
fresh canonical checks after all callbacks define the delivery boundary described below.

### Retrieval composition and work budget

Backend availability/index visibility, candidate truncation, canonical filtering,
ranking selection and output-budget omission are separate dimensions. The old
RecallResult.Complete field is removed. Coverage.Status uses ready, eventual,
pending, degraded or unavailable; ready means the declared index visibility
profile is available, not that every relevant fact was retrieved. MinimumSatisfied
is the separate exact-token guarantee. Per-backend coverage identities are unique; at most 64 backends/signals per candidate are supported.
A limited or empty result makes no relevance-completeness claim.

SearchOptions.MaxCandidates is a mandatory positive bound, at most 10000. It is
also the Recall bound before expensive canonical work; RecallOptions.Limit is
the separate maximum selected result count. SearchCapabilities declares support
for the candidate-return bound. Unsupported requests fail before the external
call. An adapter returning more than the requested bound yields ErrBudget before
candidate iteration, record decode or canonical validation; core does not silently
slice a malformed oversized result. CandidatesTruncated reports intentional
backend/composition truncation. This constrains returned metadata/core processing,
not the internal CPU, network or billing of a host backend. Context deadline and
cancellation propagate to every port, without rolling back earlier commits.

Search candidates carry typed signals: backend identity, positive rank and finite
raw score. Signals cannot contain vendor payload or mandatory query strings.
Final composed candidate refs are unique; core rejects duplicate refs, malformed
identities/ranks, non-finite scores or signals bound to absent coverage. Ranked
results preserve the declared signals alongside the policy score/explanation.
Only the composition policy combines independent backend signals.

The reference Composite uses explicit named backend entries and explicit RRF
configuration. Each entry returns one coverage identity matching its configured
name; this minimal adapter does not support nested composites. Inputs are visited
in stable identity order, independent of slice order. Weighted RRF sums
weight(identity)/(K+rank), with finite positive K/weights; omitted weights mean 1
and unknown/duplicate identities are invalid. Within each backend, duplicate exact
refs are removed before assigning distinct ranks and cannot add weight or shift
later ranks. Raw score scales are never added. Ties use record ID then revision.
Provided child signal scores are retained; a child without a signal uses its
Candidate.Score as the raw score. Signal ranks are the distinct child list ranks.
All component signals survive fusion. Each backend receives MaxCandidates;
merged output is truncated to the same global bound only after fusion, with an
explicit truncation flag. Degraded mode may preserve successful backends, but all
unavailable returns ErrUnavailable, cancellation/deadline abort the operation,
and minimum visibility cannot be weakened by degraded mode. Host policies other
than RRF implement the same Search[Q] port outside lifecycle core.

Recall reports processing counts: returned candidates, canonical checks/filters,
and ranking omissions, plus the separate truncation flag and backend coverage.
Only unique bounded candidates reach canonical checks. Stale refs may be filtered;
a forged foreign canonical binding is an error. Unauthorized data never enters
ranking, cost estimation or output selection. Historical exact refs obey the
same ReadOptions eligibility contract as canonical snapshots.

### Projected output budget

One flow, RecallProjected, composes Search -> canonical filtering -> Ranker ->
exact-revision Projector -> optional consumer budget selection -> final canonical
revalidation. Existing standalone Recall and Project remain basic operations;
there is one budgeted composition path, not competing budget wrappers. Projection
keeps provenance, source references, uncertainty/losses, reconciliation/conflict,
scope, revision, expiry and Trust=data. Packing removes whole projections; it
cannot edit their payload or promote trust. Foreign/duplicate selected refs or
invalid omission decisions fail ErrInvalid. Selection/measurement receive detached
values via consumer output codec and the configured reference codec; core retains
its own originals. The host port must be deterministic, bounded and cancellation-
aware, and must not retain/mutate callback values after return.

The consumer supplies a versioned output policy, unit, positive maximum cost and
output Codec[O]. Its policy chooses a subset and distinguishes budget omission
from an individually oversized projection. Measurement evaluates the final typed
body, including coverage, progress, omissions and complete projection envelopes.
Budget usage is an out-of-band receipt, excluded from the JSON body to avoid a
self-referential byte count; a host embedding that receipt in another envelope
must measure that additional representation itself. Exact and estimated costs
are explicitly distinguished. A body with zero/invalid cost is rejected;
cost comparisons use checked integers without wrapping. If even the empty body
cannot fit, return ErrBudget. A selected body above the limit also returns
ErrBudget, never a successful oversize output. Estimate mode guarantees only the
reported host units, never model-specific tokens or final byte size.

The offline reference policy greedily packs ranked projections using exact JSON
body bytes, including provenance/metadata overhead and omission explanations.
It measures each resulting body rather than summing payload bytes. A record larger
than the budget is explicitly omitted or rejected according to policy. No tokenizer,
embedding, vector database, prompt builder or vendor SDK enters core. Typed output,
query, source reference, payload, authority and meaningful cost units remain BYOT.
Source/authority/expiry/revocation changes during selection/measurement invalidate
delivery; revalidation errors never return a partially certified body.
If a still-readable revision changes its projected state during a policy callback,
delivery returns ErrStaleInput rather than changing the already measured body.
Input already passed to a trusted host callback remains the disclosure boundary described below.

The reference index has explicit Publish/Acknowledge steps and controllable
lag/failure; it never silently indexes canonical commits. Waiting for an exact
visibility token requires a deadline, respects cancellation and never rolls back
a canonical write. Acknowledgement promises the declared matching-query visibility,
not relevance completeness. Sparse/dense adapters connect through Search[Q]; the
runnable offline example must include divergent rankings, stale metadata, partial
failure, budget omission, final serialized-size verification and nontext queries.

Projection, provenance and source metadata serialize with explicit snake_case
JSON field names. Typed Record and RecallResult metadata use the same naming
convention for host exports; consumer payload/output/reference objects retain their own codec
contracts. Projection/cache identity binds authority, scope, policy version, purpose,
complete effective projector input snapshot, codec identities, input revisions/epochs,
read options and projection version under the projection/v2 cache domain. Final
revalidation compares all effective Record fields through configured BYOT codecs;
changed input returns ErrStaleInput without output or repeated projector calls.
Historical reads compare effective historical state while retaining current
permission/retention/revocation checks. Packing revalidates selected and omitted
snapshots after callbacks. Derived sinks have lineage,
deletion handles and purge acknowledgements. The reference index and
summary/cache sink have independently injectable failures. Index contents
restored from stale backup are still filtered canonically. Hosts must restore
the current durable revocation ledger before exposing restored canonical state.

## Forget and retention

Forget accepts a typed selector (record, source, subject or exact scope),
reason, operation ID and expected versions. Its first atomic durable change
advances scope/record/source epoch and installs content-free tombstones;
recall and new acceptance are immediately fenced. Canonical payloads,
proposals and all transitively dependent artifacts in the selector are purged.
Deletion receipts contain IDs, epochs, required sinks and acknowledgements,
not deleted content or source text. Scope fencing conservatively invalidates
all in-flight jobs in that scope; fresh unrelated records remain usable.

Purge runs outside the canonical transaction and stores each acknowledgement
durably. States: revocation_committed, purge_pending, complete or failed.
Partial failure returns pending with sink errors. Retry uses the same operation
ID and only incomplete sinks; it never creates another canonical revision.
Missing required sinks, an explicitly unsupported purge or an invalid
acknowledgement produce failed rather than complete/pending. Failed remains
retryable after the host repairs the adapter; it never retires the durable
revocation fence. External error text is not persisted. Unavailable sink
errors remain pending. Receipt error codes distinguish these conditions.
Index/projection writes capture lineage epochs, check the ledger under the
same managed-write boundary and cannot race purge to reintroduce stale data.
Retention decisions bind a concrete deadline and policy version to reviewed
content. Evaluate must reproduce that decision for unchanged payload and policy;
relative TTL policies derive deadlines from immutable consumer observation
metadata, rather than extending expiry on every check. Commit, canonical reads,
recall/projection and lineage re-evaluate the decision. A changed version or
deadline returns stale input; port failure returns policy denied without payload.
The host reviews/reconciles under the new policy. Sweep additionally evaluates
current retention and purges when either stored or current deadline has elapsed;
a policy change never extends already committed evidence lifetime.

Retention is an injected policy, executed by host-triggered Sweep; expired
records go through the same revoke/purge path. No scheduler lives in core.

Tombstone retention and reintroduction require explicit versioned host policy.
Retired IDs cannot be silently reused. Existing receipts remain content-free.
Already delivered responses and unmanaged backups are outside purge coverage.

## Consolidation and quality

Consolidate takes an authorized scoped revision set, versioned policy, budget
and utility threshold. Exact dedup groups canonical-equal payloads, intervals
and compatible scope; it preserves negative constraints and union lineage.
Domain merge uses an injected typed deterministic provider. Semantic merge
uses an injected typed provider and returns loss/uncertainty annotations.
All outputs are proposals and pass normal acceptance/reconciliation. Originals
are retained. Scope is no broader than the intersection of input permissions;
v2 exact-scope restriction rejects incompatible inputs. Source revocation or
revision change invalidates proposal and existing dependent summaries until
rebuild. Budget exhaustion leaves originals untouched.

Auto-apply is absent from the default runtime. The versioned offline corpus
compares baseline, exact dedup, domain merge and scripted semantic proposals
on expected records, false memories, privacy leakage, mandatory negations,
intervals, lineage, bytes/provider cost and measured latency. A negative
semantic result is saved with auto-apply disabled. Eval runner and telemetry
are consumer responsibilities. Snapshots and export use the same authorized
typed projection path as recall.

## Errors, cancellation and migration

Stable errors support errors.Is: unauthorized, scope violation, conflict,
unsupported capability/schema, invalid input/interval, not found, unavailable,
stale acceptance/input, policy denied, missing evidence, source unavailable,
unresolved conflict, incomparable claims, visibility pending, budget exhausted,
revoked and unknown outcome. Wrapping adds operation context without payload
or unauthorized existence disclosure. Context cancellation/deadline remains
inspectable with errors.Is. A timeout after a durable write does not certify
rollback; retry its operation ID.

The persisted envelope and each document kind have JSON Schema 2020-12
contracts in schemas/. Every serialized field is required, including explicit
null lists and zero timestamps; unknown fields and omitted fields fail closed.
Payload/reference bytes are base64 values owned by versioned consumer codecs.
Schemas constrain wire shape; runtime additionally checks digest equality,
interval ordering, scope binding, lineage, policy and state transitions.
The executable schema checks use a pinned test-only validator; core runtime
continues to use the standard library only.

Storage schema v3 has an explicit metadata version. Unsupported versions
fail on open. Host-owned reviewed imports are explicit; never reinterpret
unknown valid-time as always or updated-at as valid-from. Payload/reference
codec migration is consumer-controlled, with recorded source mapping and host
acceptance. Import uses the ordinary host-approved proposal/commit path. No
hidden transcript import, legacy shims or prompt mutations.

## Verification contract

`docs/acceptance.md` fixes granular requirement IDs before implementation.
AAA tests compare canonical revisions separately from selected recall and
rendered consumer responses. Both store adapters run common conformance;
SQLite additionally runs close/reopen, independently opened concurrent writers,
before-commit rollback, after-commit unknown outcome and recovery tests.
Malformed wire input, adversarial providers, authority outages, lagging indices,
purge failures, delayed jobs and stale backups are required cases. CI runs
gofmt, go vet, tests and race tests on the pinned toolchain. Optional adapters
and examples are compiled and exercised offline.

After implementation, two independent reviewers check completeness and
correctness against the original task. Their reports must identify the exact
state reviewed. No percentage is claimed until evidence covers every row.

### Source, acceptance and storage integrity

Canonical reads, projection, and every transitive canonical dependency validate
exact source revisions through the host Sources port. Temporal predicates do not
bypass this gate: a host that retains immutable historic evidence must validate
that revision explicitly. Missing/stale evidence and source outages return
ErrSourceUnavailable with the original cause; they never produce successful
payload or certified empty results. Consumer rank/project callbacks are followed
by source and dependency revalidation before returning content.

Acceptance deadlines compare instants (`time.Time.Equal`), independent of
monotonic clock data or time location lost during serialization. Every history
entry binds its physical key to its declared record ID and revision. Wire field
names are case sensitive; additional fields, including case aliases, are invalid.

Consolidation InputBytes covers the sum of encoded complete canonical record transfer
envelopes (public record metadata with codec-encoded payload and references). OutputBytes covers complete stamped suggestion documents, with
payload/reference bytes produced by the configured consumer codecs, including
sources, evidence, expiry, lineage, loss and uncertainty annotations. Runtime
proposal identity/acceptance bookkeeping added later is outside this suggestion
budget. Provider allocations and external side effects remain outside core
control. Payload-volume quality metrics remain a separate measurement.

Reference Index has no global canonical watermark. Without a minimum token it
reports eventual coverage, or pending when matching staged metadata is known;
it never certifies canonical absence. With an acknowledged minimum token and no
matching pending entries, ready reports the available index profile and
MinimumSatisfied certifies that requested minimum, not a global guarantee that
every canonical revision has been staged.

### Reusable port conformance

The conformance package accepts consumer-owned typed fixtures and provisioning
hooks. StoreSuite remains the transaction contract. SearchSuite checks exact
scope isolation, visibility capability honesty, cancellation, known staged lag,
and explicit backend failure. SinkSuite checks scoped deletion, bound receipts,
repeat purge, cancellation and failed acknowledgement. CodecSuite checks
versioned deterministic round trips and malformed input using host samples.
AuthoritySuite and SourcesSuite exercise grants/revision replacement/outages.
ClockSuite checks host-provided fixture instants.
CallbackSuite checks context propagation, healthy typed invocation and explicit
failure for retention, extractor, resolver, ranker, projector, consolidator and
reintroduction adapters. Host fixtures supply domain-specific result assertions;
the suite never imposes payload/query/reference types or model semantics.

### Final wire and deadline gates

Consumer byte fields in persisted envelopes require canonical padded base64 JSON
strings. Numeric byte arrays and line breaks/noncanonical padding are schema
errors; json.RawMessage remains structural JSON. Content-free revoked tombstones
retain the separately specified null payload. Shape validation does not decode
consumer payload types through generic JSON.

All effective deadlines are checked after synchronous host/source/retention/
authority calls, before returning canonical content or persisting acceptance
or canonical commit. The final gate traverses exact transitive dependency heads
without invoking host callbacks, gathers all effective deadlines, and compares
them to one final injected Clock instant. Multi-record delivery checks every
selected record and its dependency chain, including records selected before
later I/O. Commit also checks the separately bound acceptance deadline. Expiry
crossed during I/O returns ErrStaleInput with no delivered content or successful
acceptance/commit. Atomic rollback preserves previous canonical revisions.
External IAM/source changes still require host leases for cross-system atomicity.

Persisted timestamps must satisfy the RFC3339 wire date-time profile as well as
Go's time parser. Decimal comma and invalid timezone hour/minute offsets are
schema errors, even when Go can normalize them to an otherwise usable instant.

### Acceptance lease and prospective lineage

Accept binds the initial host decision for that review operation. A renewed
reauthorization with the same actor/policy does not silently replace its bound
expiry. If the initial decision expires during validation, Accept rejects with
ErrStaleInput and persists no acceptance. A retry may acquire a new decision.

Commit rejects ErrInvalid if its target RecordID occurs anywhere in mandatory
transitive lineage, not merely in immediate inputs. Under the live exact-head
model, replacing that target would invalidate the candidate's own required input
chain. This is a prospective-state check, not a claim that the historical exact
revision DAG is cyclic. Rejection atomically preserves all previous records and
receipts. Independent ancestor updates without such a dependency remain allowed
and invalidate existing dependent summaries as documented.

### Reviewed lineage consistency

Every non-revoked canonical record must retain every exact RevisionRef from
its accepted, digest-bound Proposal.Lineage in Record.Lineage. Reconciliation
may add dependencies (for example Duplicate related revisions), so equality
is not required; mandatory reviewed dependencies cannot be removed or replaced.
A persisted envelope violating this subset invariant is malformed and returns
ErrSchema before content delivery or dependent mutation. Revoked content-free
tombstones remain governed by their separate empty-proposal/lineage contract.
This detects structural corruption relative to the reviewed proposal; it does
not promise cryptographic integrity against arbitrary privileged store rewrites.

### Managed write deadline boundary

WithDerivedWrite rechecks every exact lineage dependency deadline after final
host reauthorization and immediately before invoking the synchronous managed
write callback. If input or transitive evidence expires during that host I/O,
the callback is not invoked and ErrStaleInput is returned. Explicit FencedView
excludes concurrent revocation during a started callback. Time passing or an
external effect after callback entry is not rolled back by a timeout; callbacks
remain responsible for context and their declared external-effect boundaries.

## Identity and reconciliation contract v2

Framework-owned identifiers (scope components, record/proposal/operation/source
IDs and revisions, actor, codec/provider/policy/sink versions and names) are
nonempty valid UTF-8, at most 1024 bytes, contain no NUL, and are not entirely
whitespace. Values compare exactly: no trim, case folding or Unicode normalization
is performed. Purpose may be empty but otherwise uses the same byte/text limits.
Free-text evidence, rationale and uncertainty annotations must be valid UTF-8;
they are not identifiers. Consumer payload/reference representations remain owned
by their codecs. JSON wire rejects malformed UTF-8 and unpaired escaped UTF-16
surrogates before decoding can replace them with U+FFFD. Literal U+FFFD is valid.

Each non-revoked canonical revision stores its immutable Reconciliation decision:
mode, resolver policy version, sensitive basis and exact related revisions.
AuthorityPolicyVersion is separate. The decision is returned with authorized
Record reads, including recorded-as-of reads of that revision; future transitions
change state as-of but never overwrite the initial decision. Transitions keep
only a content-free exact decision revision reference, not a second copy of
sensitive Basis or resolver policy. A forgotten decision leaves its historical
state transition intact but makes its rationale unavailable. There is no second
public Related list or ambiguous PolicyVersion field on Record. Decision slices
are detached before injected callbacks. Projection carries the same initial
decision as data. Basis is content-bearing and is purged with historical payload;
tombstones have a null decision and no authority policy content. The permanent
operation ledger continues to store content-free receipts, never CommitRequest.

Identity/decision behavior was introduced with schema 2; the current persisted
contract uses schema 3. Earlier databases and envelopes are explicitly rejected, never silently reset or
migrated. A consumer can provision a fresh database and import reviewed facts
through normal host-controlled lifecycle, maintaining its current revocations.

The executable schema validator registers the custom `memy-identifier` format
with format assertions enabled: valid UTF-8, 1–1024 bytes, no NUL and not all
Unicode whitespace. JSON Schema maxLength alone counts codepoints, not bytes.
Consumers validating these schemas must implement this documented format; an
unconfigured validator checking shape alone is not identity conformance.

## Storage and delivery contract

This section specifies the implemented storage and delivery contract. Historical
acceptance reports certify only their recorded snapshots and review scope.

### Transaction and exclusion boundaries

Store.View provides one consistent, read-only transaction. It does not promise
exclusion of revocation during arbitrary external callbacks. Store.Update is a
scoped atomic mutation with conditional per-key versions, persistent deletion
versions, rollback on callback error/panic/cancellation before commit, and an
explicit unknown-outcome error when durability cannot be disproved.

Store.FencedView is the explicit synchronous managed-write/delivery exclusion
boundary. Updates in its exact scope wait until its callback exits; independent
scopes do not wait for that callback. FencedView is a declared capability checked
before any managed external effect. SQLite must enforce exclusion across
independently opened handles and processes, not only inside one Engine. Short
SQLite write transactions may serialize across scopes because SQLite has one
physical writer. A remote callback must not retain that database writer lock.
Nested updates are forbidden inside a fenced callback. Callback context and
latency obligations belong to the trusted host; arbitrary Go callbacks are not
forcefully interrupted or detached in a background goroutine.

Ranker, Projector and merge callbacks receive detached, initially authorized
records outside the database transaction. Before final return, canonical exact
revisions, authority, source identity, current retention, lineage and deadlines
are checked again under a delivery fence. Revocation that committed before that
final boundary prevents newly delivered forbidden payload. Data already handed
to a trusted callback cannot be recalled: callbacks are host data recipients and
must follow the host's deletion policy. Managed external writes hold the explicit
scope fence through callback completion; a subsequent Forget purges their
artifacts. Failure or timeout can leave an external effect and is never an
exactly-once promise.

### Bounded traversal

Bucket.List is replaced by ordered prefix pages. A request supplies a positive
entry limit, positive returned-byte budget and an optional opaque cursor. A page
returns detached live entries in ascending bytewise key order, a continuation
cursor when more entries remain, and explicit completion. Tombstones do not
consume unbounded hidden scan work. A value exceeding the requested byte budget
returns ErrBudget rather than an empty page that claims progress. Cancellation
invalidates the current attempt without certifying completion.

Cursors bind the store instance/database identity, exact scope, exact prefix,
last emitted key and scoped mutation generation. A changed generation, reopened
in-memory store, replaced database, mismatched scope/prefix or invalid cursor is
explicitly rejected; stale traversal never silently skips new/removed keys.
Within one consistent transaction all pages use the same generation. A cursor
retains no live transaction and no callback-owned bucket. Writes after a page
invalidate its continuation; hosts restart or resume a durable lifecycle job,
not a silently inconsistent listing. Bucket lifetime, read-only enforcement,
CAS overflow and detached byte ownership remain executable store guarantees.

Addressed Get reads only the requested key and required canonical dependencies.
Consolidate loads only its exact input revisions and their required evidence;
unrelated source failure cannot veto independent inputs. Memory uses per-scope
state and addressed overlays; SQLite uses indexed addressed SQL operations.
Neither adapter materializes/copies the entire scope for an addressed read.

### Revocation and host-driven continuation

Source membership and reverse lineage include historical revisions and are
updated atomically with canonical state. They are traversal indexes, not
independent knowledge; canonical envelopes remain authoritative. Dependency
selection visits each reachable vertex/edge once instead of repeatedly scanning
history to convergence. Scoped graph cycles, overlapping ancestry and retries
must terminate without creating extra canonical revisions.

Mass Forget installs a durable fence before bounded content cleanup. Pending
jobs remain unreadable/unwritable through the fence after reopen. The same
operation identity resumes cleanup and exact sink acknowledgement; complete is
reported only after every required canonical page and managed sink acknowledgement.
An interrupted job cannot publish a partially revoked scope as complete. Stale
jobs, source evidence and cursors cannot bypass the installed fence.

Snapshot and Sweep expose bounded host-driven continuation, without an implicit
scheduler. Full enumeration necessarily visits the selected scope. Sweep checks
current retention policy for visited content even when a stored deadline is
later: a deadline index alone cannot prove eligibility. Authority/policy changes
and changed snapshots yield explicit retry/failure rather than false completion.

The old Store API is replaced directly. Persisted structural changes require a
new schema version and explicit incompatible-database rejection; no compatibility
aliases, automatic imports or parallel old/new paths are retained.

The persisted format is schema 3. Database metadata stores a
random cursor authentication secret and per-scope mutation generations. Only
schema 3 is supported; earlier envelopes/databases fail
with ErrSchema. Cursor authentication prevents hosts from accidentally forging
progress or transferring cursors between databases. Prefix/key comparisons use
exact bytes; cursor encoding preserves key bytes without JSON normalization.
Scan limits are 1–1024 entries and a positive byte budget covering key and value
bytes. A mutation within a transaction invalidates a previously issued cursor
as well. Failed/rolled-back mutations never change the committed generation.

The optional SQLite adapter supports fenced local databases on Darwin, Linux,
FreeBSD, OpenBSD, NetBSD and DragonFly. Other platforms reject Open with
ErrUnsupported before database effects; core and memory remain portable. Its
scoped shared/exclusive advisory file locks are kernel-owned open-file locks,
not expiring leases: a slow callback cannot outlive a lease and bypass revoke.
Process exit releases the lock. Independently opened handles use the resolved
database path. Hard-linked aliases, network filesystems and privileged removal
or replacement of live fence files are outside the local-adapter contract.
The `.memy-fences` directory must remain beside an active database; fence files
are not deleted during use, because unlinking a locked inode defeats exclusion.
Cleanup is host-owned after all database handles/processes are closed.

Cursor authentication additionally binds the resolved physical database identity
(path/device/inode). A copied or replaced database cannot accept cursors from the
original, even if its persisted secret/generation were copied with the file.
Reopening the same unmodified database preserves continuation. Import/restore
revocation responsibilities remain host-owned and are not solved by cursor MACs.

Ordinary SQLite View uses a consistent WAL read transaction and may overlap
mutations. FencedView holds exact-scope read exclusion; Update holds exact-scope
exclusive exclusion. Engine canonical delivery uses FencedView. Memory may
serialize these callbacks within a scope, but never across independent scopes.
Ranker/Projector receive detached permitted input after the preparation fence
has been released. A revoke may finish during their callback; final canonical
revalidation then rejects the result instead of returning its output. A callback
may already have consumed the earlier permitted input, which is a host boundary.

Historical membership indexes use schema-3 `membership` envelopes containing
only Scope, exact RevisionRef, relation kind (`source`/`lineage`) and MatchID.
Canonical Commit writes both relation and record lookup keys in the same atomic
transaction as the revision. Reconciliation state changes preserve historical
edges. Revocation removes relation entries through the record lookup, alongside
canonical content cleanup. Selection revalidates every traversed membership
against the referenced canonical revision and exact key/scope before following
it; an index hit cannot authorize payload or independently define knowledge.

Engine.Snapshot returns one SnapshotPage, using SnapshotOptions with Read,
Cursor, Limit and MaxBytes. A page bounds scanned canonical envelopes, not just
eligible returned records: an empty eligible page may still have continuation.
The cursor binds the read options and authenticated actor/policy version through
an authenticated Scan plan tag. Changing read semantics or policy during a walk
is rejected rather than presented as one complete snapshot. Host code explicitly
continues pages; no unbounded Snapshot wrapper remains in the root API.

### Durable bounded Forget

ForgetRequest requires Limit (1–1024 work entries/callbacks) and positive
MaxBytes for each scanned storage page. Transport budgets are excluded from the
semantic request digest, so a host may increase them while resuming the same
operation identity. Each call performs bounded work and reports current progress;
the host repeats the exact semantic request until complete or a sink requires
retry. No implicit scheduler or unbounded compatibility wrapper remains.

The first short transaction checks expected revisions, advances the scope epoch,
installs the selector ledger and an active-purge scope fence, and stores a
content-free durable job/receipt. It performs no history enumeration. While the
job is active, Engine canonical reads and mutations fail closed for that scope;
other scopes remain usable. Only the same operation identity may advance it.
The temporary scope-wide fence also protects dependencies not discovered yet.

Durable phases enumerate seed membership, expand indexed dependencies, clean
record history/indexes, discover affected proposal origins, and clean proposal
history. Each cursor is a stored exact key under the installed durable fence.
Scan.After is an explicit new suffix-range request, distinct from snapshot
continuation: it never validates an old snapshot cursor. Snapshot uses only
authenticated cursors; jobs use fresh bounded suffix scans of frozen canonical
state. Job progress and canonical deletion commit atomically. Interrupted work
continues after reopen without inventing another canonical revision.

PurgeBatch contains one bounded records chunk and a monotonic Chunk identifier.
Sink acknowledgement binds sink, operation, epoch and chunk. Late acknowledgements
cannot accept a different chunk. CanonicalComplete is true only after every
canonical cleanup phase; PurgeComplete additionally requires all registered sink
acknowledgements. Earlier chunk completion does not certify whole-operation
completion. Receipts never accumulate an unbounded list of all removed record
IDs; hosts may collect observed chunks themselves. Failed external callbacks may
have effects and remain idempotent per exact chunk, without exactly-once claims.

A purge continuation validates the active scope pointer against its exact
operation before advancing or accepting a completed chunk. Invalid job counters,
phase/resume combinations and foreign proposal history fail with ErrSchema;
the containing transaction rolls back. All canonical cleanup finishes before
managed sink chunks are emitted. The active fence remains until the last chunk
has every required acknowledgement.

### Durable bounded Sweep

Sweep is a host-driven maintenance pass with `SweepRequest{OperationID, Limit,
MaxBytes}`. Limit is 1–1024 work entries or sink calls per invocation; MaxBytes
bounds each storage page. Each result contains only the current bounded receipts,
this call's proposal count, work counters and Complete. Hosts repeat the same
operation identity. Results never accumulate all receipts inside the library.
Each Update stages an independent result delta, merged only after confirmed commit.
On a later error, the response preserves earlier confirmed BudgetCharged, proposal deletions
and purge receipts, including pending sink receipts; Complete remains false.
Rollback and ErrUnknownOutcome exclude that transaction from confirmed counters.
Hosts must consume result alongside error and retry the same operation identity.
A lost response or unknown commit outcome cannot guarantee exactly-once metrics.
BudgetCharged is charged budget: a successful sink continuation conservatively charges its
supplied callback allowance. On a failed continuation, only sink callback steps
with confirmed acknowledgement transactions are charged, alongside earlier storage
progress; attempted callbacks do not fabricate durable acknowledgements.
Actor, purpose, scope and operation identity bind the durable pass; transport
budgets may change on resume. Each call requires current ActionForget permission
and fresh reauthorization under that call's authority policy. A host authority
policy rollout does not change the durable request identity or trap its fence.
A completed pass remains complete on replay; a
new pass requires a new operation identity.

The first transaction installs a content-free active-sweep pointer and durable
job, and advances the scope epoch. Canonical Engine reads/mutations fail closed
in that scope until the pass completes. Independent scopes remain usable. The
explicit maintenance fence freezes the canonical key set so exact durable suffix
positions cannot silently omit writes behind the cursor. It also prevents
proposal resurrection during multi-call history deletion. Store access remains
privileged. An interrupted pass requires host continuation after reopen; there
is no background scheduler and no timeout that silently removes its fence.

BudgetCharged progresses through record revisions, proposal revisions and selected
proposal history. Each record revision evaluates both stored expiry and current
host retention policy; no stored-deadline index replaces that evaluation. An
expired record starts the existing bounded Forget protocol with exact head
revision and a deterministic operation identity derived from the pass and target.
Sweep pauses its record cursor until that purge, including every required managed
sink acknowledgement, finishes. An already active purge is continued before
canonical enumeration. Purge advancement uses the raw maintenance transaction
path; public Forget cannot bypass an active sweep. Purge receipts continue to
identify their own epoch and chunk. Proposal cleanup validates exact origin,
scope and revision binding before deletion and advances atomically with job state.

The pass scans frozen canonical entries; host policy may change during a pass and
is checked at the time each entry is visited. Complete certifies completion of
that pass and its deletions, not a claim that no deadline can expire or policy can
shorten after an earlier entry was visited. Hosts schedule another pass for such
changes. Policy failures return an error without advancing the current page.
Durable progress and canonical deletion commit together; external sink effects
remain retryable and idempotent rather than exactly once.

### Bounded validation of historical membership

A membership edge carries an inclusion proof over the exact revision's unique
source/lineage relations, in canonical array order (sources first). Leaf identity
binds scope, child revision, relation and match ID. Hashes separate leaves,
branches and the counted root. Proof positions/counts and a maximum depth of 64
are validated. Duplicate relations collapse deterministically at construction.

A purge first reads/validates the exact immutable canonical revision and computes
its counted membership root. It persists a content-free purge-evidence document
bound to scope, operation and exact RevisionRef in the same active-fenced job.
Subsequent batches validate edge proofs against that durable root. No host-global
cache survives outside the deletion fence, and an index never authenticates its
own arbitrary relation. Each canonical revision body is decoded at most once for
membership validation over the whole interrupted/reopened purge. Each edge has
fixed bounded proof validation work (at most 64 branch hashes), so batching does
not multiply full child-lineage parsing by parent degree.

Transaction-local host ports (Authority.Check, Sources.Validate, Retention.Evaluate,
Clock and codecs) must be bounded local operations and obey context cancellation
where their interface supplies a context. The host prepares network-backed policy
and source state before entering the engine. These callbacks may execute inside
Store.Update or FencedView; they must not perform remote I/O or recursively enter
the same store. Ranker, Projector and consolidation merge callbacks execute outside
store transactions and are followed by fresh canonical validation.


## Internal offline quality harness (v2 contract)

This section specifies the internal consumer protocol; it is not an
acceptance claim. The harness is a consumer of public memy APIs in
`internal/quality`. It adds no evaluation types, model SDK, natural-language
classifier, executable procedure dispatcher or new policy to the root package.
All inputs are synthetic. Default runs use scripted providers and deterministic
search; results are not an LLM benchmark or proof of general poisoning resistance.
The old tea-only global protocol and `Trial.Accepted` report are replaced without
compatibility aliases.

### Typed fixture plans and pinned manifest

The JSON manifest `testdata/quality-v2.json` selects a closed, versioned set of
Go-owned fixture plans. A plan has setup, ordered events, typed query, checkpoints
and expected outcomes. These are ordinary fixture functions and domain types,
not a public DSL or an interpreter of provider text. At least `Preference` and
`Procedure`/`Outcome` payload domains execute through independent typed engines.
Procedure steps and provider instructions stay data. Host acceptance is an
explicit versioned consumer callback; it cannot be inferred from provenance.

The manifest pins schema/corpus version, each fixture-plan version, extractor
provider and model identity (`scripted`, explicitly not a commercial model), host
review and retention policies, resolver/consolidation policy, search/composition,
projector/output-packing and optional answer-grader versions. Budgets distinguish
candidate count, recalled record count, full context JSON bytes, consolidation
input/output payload bytes and abstract provider cost units. Validate supported
identities, unique scenario IDs, required group coverage, positive bounded budgets,
seed and bounded positive repeat count before allocating stores. Unknown fixture
or unsupported version is invalid execution, not an empty successful corpus.
No global validator requires a domain key, English negation, source ID or identical
interval. Domain-specific prerequisites belong to their owning plan.

Each scenario/repeat starts with isolated canonical state. A deterministic PRNG
derived from the pinned seed, scenario identity and repeat number varies insertion
order/distractor placement or backend order while preserving the expected semantic
outcome. Record the derived seed and variation identity. Seed/repeats must affect
actual fixture execution; they are not decorative metadata or statistical quality
claims. Re-running the same configuration produces the same gates and deterministic
evidence; wall-clock timings, if retained, are labelled local diagnostics.

### Report stages and final verdict

Report schema identity is `memy-quality/v2`. Report contains the validated manifest
and actual executed port versions, per-scenario/repeat stage results, evidence and
one final aggregate verdict. Scenario IDs and diagnostic codes are fixture-owned
safe identifiers. Never serialize raw payload, rendered model text, source
references, privacy-fixture secrets, arbitrary provider error strings or raw query
text. Even malformed manifest/load failures produce a minimal safe report when an
output destination is writable. Evidence uses counts, synthetic public revision
aliases, expected/observed safe outcome classes and fixed diagnostic codes. A hash
of a secret is not a replacement for redaction.

The following stages are distinct:

1. **Candidate quality** evaluates proposed facts, qualifiers/negation, validity,
   source scope and exact lineage before review. A bad candidate may be recorded
   as failed candidate quality without making an expected-rejection scenario fail.
2. **Host review** records `accept`, `reject` or `not_applicable`, its expected
   decision, version and assertion status. A provider/policy error is `unknown`,
   never an observed rejection. Acceptance must use ordinary Accept/Commit;
   rejection must leave the expected baseline intact.
3. **Effective memory** compares the actual canonical recall against fixture-owned
   expected exact revisions, payload equality, validity, conflicts and provenance.
   Record count alone never proves correctness. Expected identities after apply
   derive from checked commit receipts; rejected proposals must not enter them.
4. **Canonical invariants** independently check originals/history, scope/privacy,
   source validity, lineage and current retention/revocation. Read all bounded
   Snapshot pages when a complete snapshot is required. Snapshot, authorization
   or assertion errors are not interpreted as zero violations.
5. **Rendered output** checks the actual output of RecallProjected and any host
   answer adapter independently of effective recall. Exact final JSON bytes are
   measured with the full-body output policy, including projection envelopes,
   provenance, coverage, progress and omissions. Check that only permitted exact
   revisions survive packing and that omitted required facts are accounted for
   by the scenario's budget expectation. A scripted answer's abstention does not
   establish LLM abstention quality.
6. **Execution** records whether all planned events/checkpoints ran, expected
   error classes were observed and resources/cancellation completed. Unknown
   provider/search/policy/grader or evaluator errors mark execution unknown.
7. **Final verdict** is computed once after all stages, including late privacy
   and rendered-output checks. It is not host review acceptance.

Each checkpoint has `pass`, `fail` or `unknown`, a mandatory flag, fixed evidence
codes and expected/observed outcome classes. Every applicable required checkpoint
must be present; missing evidence is unknown. Non-applicable optional stages are
explicitly declared by the plan rather than silently reported as passed. Never
publish zero violations for a checkpoint that did not run or returned an error.
A scenario passes only when all mandatory protocol assertions pass and execution
is known. An intentionally invalid candidate can pass the protocol if it was
rejected and the effective baseline and all subsequent invariants are correct.
Candidate quality is still reported as bad.

Unknown or invalid execution takes precedence over a known failed mandatory gate
in the aggregate exit status: CLI exit **2** means execution/evaluation is unknown
or invalid; exit **1** means execution is known and at least one mandatory gate
failed; exit **0** means every mandatory protocol gate passed. Exit 0 includes
expected semantic rejection. Known expected port failures are checked explicitly
against the plan's allowed error class and require correct fail-closed state;
they do not turn an arbitrary error into a passed expected-failure check.
Unexpected errors preserve already observed safe stage evidence in the report.
The command writes the safe report before choosing the exit code. Serialization or
report-write failure exits 2 and emits only a fixed safe stderr diagnostic; a
write failure cannot claim that a report was saved. No raw error is printed.

### Required scenario groups

| Group | Required observable checkpoint |
| --- | --- |
| Sessions/corrections | A new session reads accepted corrected memory; superseded, unknown and conflicted records follow explicit read options |
| Temporal | Valid-time and recorded-time select distinct expected revisions; historical reads still obey current revoke/retention |
| Retrieval | A typed query distinguishes relevant records from distractors; duplicate backend hits, stale revisions and bounded packing have explicit expected outcomes |
| Abstention/unavailability | Healthy no-match, conflict and unavailable backend are distinct; provider/search/policy failure and optional grader failure retain safe known/unknown status |
| Poisoning | Rejecting host policy blocks a synthetic false privilege rule; procedure/instruction data never alters authorized harness event execution |
| Forget/derived state | Interrupted managed purge and retry, late derived write, source revocation and stale index cannot expose forbidden final context |
| Consolidation | Baseline, exact/domain and deliberately bad semantic proposals are evaluated after apply/reject; originals, negation, validity and exact lineage are independent checks |
| Evaluator failures | Same count/wrong record or payload, missing lineage, late privacy leak and false healthy-empty during outage change final verdict to failed or unknown as appropriate |

The poisoning suite also includes a permissive host accepting a false rule or
instruction-bearing payload. Show explicitly that the library stores accepted
false data as data and does not discover the lie from provenance. This boundary
case has descriptive failed semantic-quality evidence and mandatory assertions
that the weakness was observed and no executable dispatch occurred. It cannot be
labelled successful semantic protection. The guarded and permissive policies must
have different recorded identities.

Evaluator mutation probes run as isolated negative scenario reports. Each probe
modifies an observation/checkpoint result rather than the expected oracle, then
must produce its own non-passing final verdict and nonzero exit classification.
A parent self-check passes only if it observes that failure. Do not insert an
expected-failure flag into normal final aggregation that could launder a real
late privacy leak or unknown execution. Mutation probes never become the saved
normal effective output, and their safe nested outcomes remain visible.

### Ports and measurement boundary

A small fixture-owned generic bundle allows substitution of a real
`memy.Extractor`, `memy.Search` and optional typed answer grader. Version identity
and execution mode are reported from the actual selected bundle. The runner and
checkpoint oracles remain the same under substitution. Ship an offline integration
example supplying custom scripted ports through that seam and label it scripted.
External network/model evaluation is optional and not required by CI. A model SDK,
tokenizer, judge prompt, credentials and task-specific answer rubric belong to the
consumer, not memy or a new universal evaluation framework.

Report consumer payload bytes separately from exact final context JSON bytes.
Canonical serialized document bytes may be measured through addressed raw store
inspection of the isolated synthetic fixture; label these as serialized canonical
footprint, excluding adapter/index/database/WAL overhead. If not measured, report
explicitly unavailable with a reason, never payload bytes under a storage label.
Real provider tokens/currency are unknown for scripted runs. Abstract scripted
cost units and configured cost budgets retain their unit identity; they are not
converted into tokens or money. No throughput/speedup claim is made; the historical storage review
performance methodology remains the reference and is not duplicated here.

### Internal implementation outline

The following division keeps core unchanged and permits disjoint implementation:

- `internal/quality/report.go`: stage/status/evidence/report types, mandatory
  finalization, aggregate/exit classification and safe error classification.
  `Finalize` consumes completed stage evidence, never a host acceptance boolean.
- `internal/quality/corpus.go`: v2 manifest types, strict decoding/validation and
  closed fixture-version registration metadata; replace the old tea assumptions.
- `internal/quality/quality.go`: `Load(path) (Corpus, error)` and
  `Run(ctx, corpus) (Report, error)`, orchestration, safe partial report and actual
  version/configuration capture. An error never discards report evidence;
  `Report.ExitCode()` accounts for execution errors as well as mandatory failures.
- Fixture files own `ScenarioPlan[P, Q, O]`, typed payloads, port bundles, public
  lifecycle setup/events and domain checkpoint equality. Domain plans yield safe
  stage evidence to the report layer; neither JSON nor report types carry P/Q/O.
  A shared fixture helper may wrap extraction, acceptance, commit, acknowledged
  indexing, bounded Snapshot, RecallProjected and purge continuation.
- Dedicated evaluator tests inject the four observation faults and exercise safe
  diagnostics/unknown execution; fixture tests verify actual public API outcomes.
- `examples/quality` owns command flags/report persistence/exit mapping;
  an offline port-integration example invokes the same registered checkpoints.
  `docs/quality.md` and the saved corpus/report are updated after implementation.

A concrete shared orchestration seam is:

```go
// These types live only in internal/quality. Domains keep their typed ports.
type CaseRun struct { Repeat int; Seed uint64 }
type RequiredCheck struct { Stage, ID string }
type CasePlan struct {
    ID, Domain, Version string
    Groups []string
    Required []RequiredCheck
    OptionalStages []string
    Versions PortVersions
    Run func(context.Context, Corpus, CaseRun) (ScenarioReport, error)
}
// ScenarioReport carries Candidate, HostReview, Effective, Canonical, Rendered,
// Execution StageResult and Final Verdict; stage evidence never contains payload.
// StageResult owns []Check. A Check owns ID, Mandatory, Status,
// Expected/Observed fixed outcome classes and []Evidence.
```

The registry verifies manifest identities before dispatch, and the orchestrator
enforces declared mandatory check IDs, allowed optional stages and actual port
versions before finalizing each returned ScenarioReport and its execution outcome.
Domain runners do not set aggregate success. A measurement has explicit
known/unavailable status, unit and optional value; absence or an errored check
cannot be serialized as a measured zero. Optional answer-grader absence is
explicitly not applicable, while a configured grader error is unknown execution.

Types and helper names may be adjusted coherently during implementation, but the
stage separation, bounded public-API execution, safe report, typed fixture boundary
and final-verdict semantics above are mandatory.

### Exact deduplication deadline groups

ExactDedup retains originals and emits one proposal per equal payload/Interval/scope
group, with exact revision lineage for that group. Its expiry cap is the earliest
nonzero effective expiry among only those lineage inputs; an unrelated singleton
or another duplicate group cannot shorten it. An earlier explicit output deadline
is preserved. DomainMerge and SemanticMerge retain all-request-input lineage and
cap output expiry against every input. These rules do not extend ancestor evidence
or bypass current lineage/retention checks when accepting or reading a derived record.
