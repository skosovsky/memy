# Managed projection contract

## Retrieval scores and evidence

`Score` is a detached value `{present: bool, value: number}`. The zero Go value
is absent (`present=false,value=0`). `ScoreOf(x)` records an observed or computed
finite value, including zero and negative values. `Validate` rejects NaN,
infinity, and a nonzero value marked absent. Native backend evidence lives only
in `SearchSignal`; candidate/ranked scores are host ranking results. A rank-only
adapter must leave native score absent. Fusion never infers native score from
candidate score. RRF computes a present aggregate score from deduplicated ordinal
positions; a supplied native rank is preserved separately. Missing signal metadata
in an ordered child means positional rank and absent native score, not an observed
numeric score. Native rank is positive and need not fit a truncated candidate count.

`ScoreRanker` orders present scores descending, then absent scores, with exact
record/revision tie-break. Rankers may replace ranking score/explanation and select
only canonical eligible references; they cannot replace native signals or payload.

`Projection.Retrieval` is optional `RetrievalEvidence{score, explanation, signals}`.
`RecallProjected` stamps it from the selected canonical ranked item; the containing
projection binds it to scope/record/revision. `Project` has no search evidence and
leaves it nil. Output policies receive detached evidence and select identities
only. Packing measures the entire serialized result, including retrieval evidence,
provenance and omission metadata. Revalidation covers omitted identities too.

## Host persistence and recovery

Ephemeral context is rebuilt from fresh recall. Persistent context requires an
explicit registered `Sink`, durable deletion handles, full reverse lineage and a
host-owned storage protocol. The runnable managed-projections example uses SQLite
for canonical data and a separate SQLite checkpoint database. Its backend contract
is local transactional durability; it does not claim replicated or distributed
storage. An optional HTTP transport integration tests the same durable backend
across a process boundary: acknowledgements are emitted only after commit and
retries reconcile unknown transport outcomes. That guarantee does not extend to
other backends, HA, backups or forensic erasure.

Host authenticates scope independently of query/payload. Artifact identities use
structural scope plus deletion handle and exact lineage. A multi-input checkpoint
stores every dependency; revocation of any input deletes every dependent copy.
Payload, lineage and handles commit in one checkpoint transaction. `Fence` precedes
projection preparation; `WithDerivedWrite` synchronously validates the exact
references and completes persistence while canonical scope revocation is excluded.
The callback must obey context and must not call the same canonical store recursively.
Callback failure can follow an external commit: reconcile; never assume rollback.

Before serving a checkpoint, load and detach all metadata/bytes, then perform a fresh eligibility
check for all exact lineage. Do not call the canonical store from inside the write
callback. There is no storage or network I/O after the final canonical gate.
Recheck at delivery using the engine gate; checkpoint bytes are not an
independent authority. Missing, stale, revoked, expired or unavailable dependencies
fail closed and invalidate the checkpoint; the host may recompile with fresh recall.
Already delivered answers cannot be recalled. Restoring old canonical storage
requires the current revocation ledger before serving any content.

`Forget` commits revoke before invoking sinks. Pending is not complete; a sink
acknowledges only after durable deletion of every dependent artifact in its scope.
Purge is idempotent; close/reopen and repeated batches preserve deletion.
The checkpoint backend stores each artifact write epoch and a monotonic scope
fence. Stale remote write retries are rejected; older purge retries delete only
pre-revocation artifact epochs and cannot delete newly fenced artifacts. Host
schedules retries/reconciliation. Test canonical SQLite and checkpoint SQLite as
separate resources: no distributed transaction or exactly-once external effect is
promised.

## Verification and publication

Core conformance runs offline and has no consumer dependency. Optional consumer
fixtures run in a temporary module: local checkout lane and a published-artifact
lane (`GOWORK=off`, no local replacements). Real external composition belongs to
that suite. The release contract stays single-module. No backend not exercised by
an explicit integration test receives distributed purge guarantees.

## Remote checkpoint example backend

`checkpoint.Remote` and `checkpoint.Handler` implement the explicitly selected
HTTP/SQLite host backend. The service credential authorizes privileged adapter
access, not a tenant: the caller must supply authenticated scope and use canonical
engine gates. Use TLS and credential management outside loopback fixtures. JSON
requests and responses are bounded; late effects and transport failures have
unknown outcomes, reconciled through deterministic handles and idempotent purge.

`TestRemoteDurablePurgeUnknownReplyRestartAndFence` launches a separate OS process
with a real TCP listener and durable SQLite file. It tests post-commit reply loss,
process termination/restart, persistent deletion, exact retry acknowledgement,
late write rejection, cancellation and cross-tenant isolation. This is a real
network participant test of this backend. It does not certify unrelated production
backends, replicated storage, HA or global distributed transactions. Production
adoption of another backend requires its own integration suite.
