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

## Retrieval API break (task03)

The retrieval API changes directly; there are no compatibility aliases or
implicit defaults. These runtime changes leave persisted schema version 3
unchanged. Update each Search adapter and every Recall caller together:

- Set SearchOptions.MaxCandidates to 1..10000; zero is ErrInvalid. Declare
  SearchCapabilities.BoundedCandidates and honor the requested returned count.
  Missing support is ErrUnsupported; excess results are ErrBudget before core
  decodes or validates canonical records. Report intentional truncation through
  SearchResult.CandidatesTruncated. RecallOptions.Limit remains a separate
  maximum for selected records, also 1..10000.
- Replace RecallResult.Complete checks with the specific guarantee needed:
  Coverage for backend availability/visibility, MinimumSatisfied for an exact
  visibility token, and RecallProgress for returned candidates, canonical
  filtering, ranking omissions and truncation. Coverage status `complete` is
  replaced by `ready`; neither `ready` nor a short/empty result promises that
  all relevant knowledge was found. All-unavailable is ErrUnavailable.
- Return unique exact candidate refs with finite scores and typed SearchSignal
  values: backend identity, positive rank and finite raw score. Signals must bind
  to declared coverage identities. Ranked preserves these signals. At most 64
  backends/signals are supported. Core rejects duplicate final refs instead of
  discarding the second backend's evidence.
- Construct reference.Composite with []reference.Backend[Q] containing explicit
  IDs/Search ports and a positive finite RRFConfig.K. Each child has one coverage
  identity matching its ID. Optional positive finite weights bind to IDs, not
  slice positions. RRF deduplicates each backend before distinct ranks, sums
  weight/(K+rank), preserves signals and applies the final candidate bound.
  Raw scores are not summed; backend permutations and ties are deterministic.
  AllowDegraded does not weaken minimum visibility or cancellation. Nested
  composites are outside this reference adapter's contract.

Use RecallProjected for the optional ranked/projected output-budget flow.
Standalone Recall and Project remain available. A nil budget selects every
ranked projection; ProjectionBudget[O,R] supplies a positive uint64 Max, an
output Codec[O] and a versioned OutputPolicy[O,R]. Selection returns identities
and one valid omission per unselected projection, never replacement content.
Selection and measurement receive detached values. Final canonical checks
cover selected and omitted refs before any successful body is delivered.

Measure the complete final ProjectedRecallResult body, including provenance,
coverage, progress and omissions. BudgetUsage is an out-of-band receipt and is
excluded from JSON; measure any host envelope that includes it separately.
Zero cost is ErrInvalid; reported cost above Max is ErrBudget without integer
wrapping. Unit and Exact belong to the host policy. Estimated units do not
promise final bytes or model-specific tokens. reference.JSONPacking measures
exact json.Marshal body bytes, reports individually oversized projections
(or rejects them with RejectOversized), and rejects an unfit metadata-only body.
The offline examples/retrieval command demonstrates this representation.

Neither the candidate count nor the output budget controls a remote backend's
internal CPU/network/billing. Production sparse/dense integrations implement
Search[Q]; tokenizer, model-specific cost, relevance and query types remain
consumer-owned. No embedding, vendor SDK or prompt builder is required by core.

## Persisted envelopes and maintenance

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
