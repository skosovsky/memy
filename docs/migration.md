# Schema versions and host-approved imports

The SQLite database metadata and persisted JSON document schema both start at
version 1. An unsupported database version fails on Open; an unsupported JSON
version fails with ErrSchema before returning any typed payload. The wire
contracts are `schemas/document-v1.schema.json` and seven per-kind data schemas.
They describe private adapter storage, not a network service or a universal
consumer payload format. Direct Bucket access is privileged.

Every persisted field is serialized and required. Nil metadata slices are
explicit JSON null. Time values are RFC3339 strings; Go zero time represents
an unset endpoint/deadline. An unknown valid interval has Known=false and zero
endpoints. It never means a fact is always applicable. Consumer payload and
reference bytes use base64 plus independent, explicit codec versions.

Schema tests compile all files locally with the pinned test-only JSON Schema
2020-12 validator and validate documents produced by the runnable lifecycle.
Core does not import that validator. JSON Schema checks structure, enum values,
required fields and numeric bounds; runtime checks cryptographic digest,
interval ordering, scope/identity, current authority, source versions, epochs,
transitive lineage and permitted state transitions. Passing a schema is not
acceptance or authority to commit.

There is no existing released schema to migrate and no automatic migration or
legacy transcript import. Future migrations must be explicit, transactional
within the store, and preserve record identity, operation receipts, revocation
ledger, source mapping, unknown valid time and recorded history. Keep schema
versions separate from the driver version and consumer codec versions. An old
updated-at timestamp must not be renamed valid-from. Document a new schema
and migration before adding code that reads it; unknown versions fail closed.

For an import into v1, the host performs these steps:

1. Authenticate authority and choose an explicit destination scope. Build a
   reviewed source-ID/revision/reference mapping; register it with Sources.
2. Convert the legacy value to its consumer-owned payload type. Declare known
   valid intervals only when supported by evidence. Record extraction/import
   identity and evidence; retain the external source reference instead of
   copying sensitive transcripts unnecessarily.
3. Remember each converted fact using a stable operation ID, then perform a
   separate host Accept and conditional Commit with an explicit reconciliation
   policy. Persist each exact request for retries. See examples/lifecycle.
4. Validate canonical revisions and recall separately. Publish managed derived
   artifacts with current epoch/lineage fencing, then acknowledge visibility.
   Imported knowledge remains data and cannot grant tool permissions or modify
   prompts/code.

This path imports reviewed facts into new recorded history; it does not forge
old RecordedAt timestamps. A full historical transfer needs a separate mapping
and audited schema migration, not a write into arbitrary Bucket keys. Already
trusted facts still use host-approved acceptance and the same durable receipts.

On rollback of consolidation, revert the host policy in a separate namespace
or use ordinary conditional reconciliation. Keep durable revocations current.
Never restore an old revocation ledger with a backup and expose reads before
reapplying subsequent forget decisions. Unmanaged backup deletion and forensic
filesystem erasure remain host responsibilities outside purge receipts.
