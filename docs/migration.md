# Persisted format v2 — clear break

Database metadata and all persisted envelopes use version 2. Only v2 is supported.
Version 1 is rejected explicitly; there is no automatic migration, dual reader,
compatibility wrapper or destructive reset. Existing audit snapshots describe
their historical version, not acceptance of this implementation.

Provision a fresh v2 database or write a host-owned reviewed import using
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

Executable JSON Schema 2020-12 contracts are in schemas/*-v2.schema.json.
Runtime additionally checks digests, source identity, scope, intervals, lineage,
retention, transitions and epoch fences. Passing shape validation is not host
acceptance. Core remains independent of the test-only schema validator.

State transitions contain only the exact decision revision reference. Sensitive
Basis is stored once on that decision's revision and is removed by its Forget.
Schema identity assertions require the documented custom `memy-identifier`
format (see design); schema_test registers it on the test-only validator.
