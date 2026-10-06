# Review remediation contracts

This is the normative target for task2, approved before implementation. Until
its implementation stages are accepted, the baseline at e12c8fc does not satisfy
these contracts. Existing persisted schema remains v3; representation identities
below change explicitly rather than enabling permissive compatibility readers.

## JSONCodec: json/v2

Both Encode and Decode admit only valid Unicode scalar strings and object keys.
Reject invalid UTF-8 and unpaired UTF-16 escape surrogates before replacement or
normalization, including nested values, map keys, RawMessage and custom JSON
marshal output. Literal U+FFFD, emoji and paired surrogates are valid. Encode must
validate the Go strings that encoding/json would otherwise replace. This is a
Unicode check on domain data, not the closed service-metadata identifier policy.
Reject malformed JSON, duplicate object keys, unknown struct fields, trailing
values, nonfinite numbers and cycles. Deterministic custom MarshalJSON/MarshalText
callbacks are a host admission obligation: finite runtime checks cannot prove
their determinism. Any observed inconsistent output violates the codec contract;
repeated encoding is not certification of arbitrary callbacks.

Interface-valued numbers decode as json.Number, including nested arrays/maps
and source references. Explicit typed int64/float64 fields use the host's declared
Go type; arbitrary original dynamic numeric Go types cannot be recovered.
Canonical numeric lexemes are retained: 1, 1.0 and 1e0 may have distinct digests.
For admitted generic json.Number data, Encode → Decode → Encode must retain
canonical bytes and therefore the accepted digest. Explicit typed numeric fields
follow encoding/json conversion rules; they do not promise lexical preservation.
Encoding orders object keys; decoding returns independently owned mutable data.
Consumers must use deterministic, concurrency-safe custom codecs with clear
buffer ownership; core does not sandbox custom codecs.

The stock codec identity changes from json/v1 to json/v2 for both payload and
reference codecs. Existing mismatched proposal/record codec identities fail
explicitly with ErrSchema. No relabeling of stored bytes or digest, float64 fallback,
or dual stock reader is allowed. A host may inspect old data offline and perform
a reviewed import through Remember → Accept → Commit with current scope, source,
authority and retention checks. Already rounded numbers or replaced Unicode
cannot be reconstructed without an authoritative source. Preserve and reapply
current revocations; do not copy historical acceptance into new proposals.

## Effective projection snapshot and cache identity

Before calling the projector, capture an owned effective Record snapshot. Its
identity includes all fields visible to Projector.Project: codec-encoded payload
and source references, provenance, state, expiry, reconciliation, policy metadata,
identifiers, versions, timestamps and every other Record field. Use the configured
codecs for BYOT values, never assumptions about their JSON shape.

At the final canonical delivery boundary, compare that snapshot with the newly
resolved effective Record using the same ReadOptions. If it changed, return
ErrStaleInput and no output. Never automatically repeat a projector/model call.
Existing permission, source, retention, epoch and revocation checks still apply.
Project and both RecallProjected paths (with or without packing) obey this rule;
selected and omitted refs remain subject to final delivery checks.

RecordedAsOf compares the effective historical record. A later current-head
conflict alone does not invalidate an unchanged historical input; authority and
revocation remain current checks. Content Revision alone is insufficient to
identify mutable state.

CacheKey uses a new projection/v2 identity domain and binds the complete effective
snapshot plus authenticated actor, scope, authority policy version, purpose,
projector version, epoch and all read options. Equal bindings yield equal keys;
a changed effective state yields a different key. Hosts must invalidate old keys
on upgrade. Core owns no cache storage. A key or cached result never replaces
canonical authorization/revalidation, even for historical reads.

## Store continuation: cursor v2

Use a fixed-size SHA-256 binding of exact scope, prefix, initial After and Plan,
with unambiguous length framing. Authenticate this binding, the scoped generation
and raw last-key bytes with the store-owned MAC secret. Preserve arbitrary admitted
binary keys without Unicode normalization. Instance/database identity stays bound
by the existing secret and database identity mechanisms.

The wire is base64url-without-padding of binary payload followed by a dot and a
base64url SHA-256 MAC. Payload: one version byte (2), 32 binding bytes, eight
big-endian generation bytes, two big-endian last-key-length bytes and 1..4096 raw
last-key bytes. Maximum payload is 4139 bytes; encoded length is ceil(4*4139/3)
= 5519, plus one separator and 43 MAC bytes = 5563 bytes. Every issued cursor
must satisfy this bound and be accepted at unchanged binding/generation. Scope,
prefix, After and Plan maxima do not expand the wire. Limit and MaxBytes are
transport budgets and may change on continuation.

Oversized input is ErrInvalid before authentication, including oversized old
cursors. Old authenticated formats within the new bound are explicitly stale; callers restart a scan
or resume a durable lifecycle job. Oversized/malformed external cursors are
ErrInvalid; changed authenticated bindings/generation or MAC are rejected without
continuation. No database schema migration is required. Test both Memory and
SQLite across maximum escaped scope, prefix/After/Plan and binary key boundaries,
traversing only issued cursors exactly once in byte order.

## Sweep partial progress

SweepResult describes this invocation, including confirmed progress when it
returns an error. Each storage Update stages a separate result delta; merge it
only after Update reports known success. Rollback and unknown commit outcome do
not count that transaction as confirmed progress. Preserve all earlier confirmed
transactions on a later retention, storage, cancellation or sink error.
Complete remains false on interruption or unknown outcome. ErrUnknownOutcome
is returned honestly; retry the original operation identity to recover durable
state, without claiming exactly-once counters after a lost response.

Purge receipts already obtained from committed continuation steps remain in the
partial result, including pending sink receipts. Acknowledgement/purge completion
must not be fabricated from an attempted callback. Work is charged budget, not a
measurement of actual backend effort; document or rename it in the subsequent
API cleanup stage. MaxBytes bounds each storage page, not the whole invocation.
These reporting changes leave the durable pass schema and operation identity
unchanged. Hosts must consume result plus error and aggregate confirmed progress;
never discard the result solely because error is nonnil.
