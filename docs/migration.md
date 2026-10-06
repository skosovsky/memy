# Persisted format v3 — clear break

Database metadata and all persisted envelopes use version 3. Only v3 is supported.
Versions 1 and 2 are rejected explicitly; there is no automatic migration, dual reader,
compatibility wrapper or destructive reset. Existing audit snapshots describe
their historical version, not acceptance of this implementation.

Provision a fresh v3 database or write a host-owned reviewed import using
Remember → Accept → Commit. Authenticate scope, register exact source revisions,
and supply current authority/retention/reconciliation policies. Do not forge
RecordedAt or reinterpret unknown valid-time. Consumer codec versions remain
independent of the library storage version. Restore/reapply the current durable
revocations before serving an imported/restored canonical database; an old backup
is not evidence of the current revocation ledger.

Record now carries AuthorityPolicyVersion and Reconciliation. The latter stores
the initial mode, resolver PolicyVersion, sensitive Basis and exact Related refs.
Record.Related and Record.PolicyVersion are removed. All callers and schemas
use the new contract directly. Basis is purged with knowledge, not retained in
content-free receipts. JSON wire rejects invalid UTF-8 and unpaired surrogates.

Executable JSON Schema 2020-12 contracts are in schemas/*-v3.schema.json.
Runtime additionally checks digests, source identity, scope, intervals, lineage,
retention, transitions and epoch fences. Passing shape validation is not host
acceptance. Core remains independent of the test-only schema validator.

State transitions contain only the exact decision revision reference. Sensitive
Basis is stored once on that decision's revision and is removed by its Forget.
Schema identity assertions require the documented custom `memy-identifier`
format (see design); schema_test registers it on the test-only validator.

Store uses bounded Scan pages with authenticated scope/prefix/generation cursors.
SQLite metadata now persists cursor authentication and scope generations; old
databases are rejected before serving callbacks. A cursor never owns a live
transaction and cannot be transplanted into a fresh/imported database.

Sweep now accepts SweepRequest with a required operation identity, Limit and
MaxBytes. Each result describes one bounded invocation. The host repeats that
identity until Complete, handling pending/failed managed sinks. The maintenance
fence makes Engine payload unavailable in that scope until the durable pass
finishes; reopen preserves it. Use a new identity for another pass.
