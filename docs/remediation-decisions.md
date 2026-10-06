# Remediation decisions — 2026-10-06

These decisions close D01–D70 from the review of `e12c8fc`. They describe the
implemented contract and deliberate scope choices; historical audit percentages
are not current acceptance. Findings F01–F10 have their separate regression cases
and stage acceptance logs. No background scheduler, distributed transaction,
provider client, general evaluation framework or permissive compatibility reader
is introduced.

| ID | Decision | Result and reason |
| --- | --- | --- |
| D01 | Preserve | Remember creates proposals only. Onboarding explicitly uses Remember → Accept → Commit; naming does not imply automatic canonical writes. |
| D02 | Preserve | Operation replay returns immutable proposal content; Proposal reads current mutable review state. A second view wrapper would obscure this existing distinction. |
| D03 | Preserve | ExactDedup proposes duplicate groups and retains originals. Acceptance/commit/reconciliation remain host decisions. |
| D04 | Preserve | Equality uses scoped encoded payload plus Interval. The first selected representative supplies ObservedAt; no domain-free timestamp aggregation is inferred. |
| D05 | Preserve | Empty consolidation remains ErrMissingEvidence. Hosts may treat no duplicate groups as benign no-change; an empty provider result cannot silently satisfy evidence. |
| D06 | Preserve | Consolidation uses Read, Consolidate and Propose permissions and the shared proposal operation-ID namespace. Explicit host operation IDs prevent accidental aliasing. |
| D07 | Preserve | Domain/Semantic summaries conservatively depend on all inputs. Narrower lineage requires a separate verified contract; ExactDedup already identifies its own group. |
| D08 | Preserve | Missing provider Sources are completed from input provenance. This is dependency evidence, never factual verification or acceptance. |
| D09 | Preserve | Derived lineage requires active current heads. A historical ancestor is insufficient; superseding one's own ancestor invalidates that dependency. |
| D10 | Preserve | Content-free commit replay recovers a durable receipt without new mutation. Receipt recovery does not assert current readability. |
| D11 | Preserve | Reject invalidates review of that proposal revision; Revise creates another revision. Record revocation still requires Forget. |
| D12 | Change docs | Required Config ports are enumerated; Sinks are optional and absent Reintroduction denies retired identity reuse. No allow-all defaults. |
| D13 | Preserve | Host owns identity/truth and Append/Duplicate/Supersede/Conflict decisions. Embeddings and provenance cannot resolve business truth. |
| D14 | Preserve | Scoped sweep/purge exclusion remains fail-closed. Host continues durable operation after interruption; no TTL silently drops maintenance ownership. |
| D15 | Change | Active maintenance satisfies both ErrMaintenance and ErrRevoked. Inspect ErrMaintenance first; retired identity failures lack that discriminator. |
| D16 | Preserve | WithDerivedWrite holds scoped exclusion during the synchronous callback. Prepare expensive work before it, bound callback duration and avoid reentry. |
| D17 | Change | SweepResult.Work becomes BudgetCharged, without an alias. It measures charged allowance, including conservative callback reservation, not CPU/I/O. |
| D18 | Preserve with cost | completedPurges uses collectAll over purge history: O(history) time/memory. Reintroduction is privileged rare administration; no speculative secondary lifecycle index. |
| D19 | Preserve | Any expired historical revision can retire the record identity. This conservative privacy rule is explicit; changing it requires a new retention contract. |
| D20 | Preserve with contract | MaxBytes bounds each scanned storage page. ProjectionBudget.Max is an independent output-body bound; neither claims whole-operation memory or backend billing limits. |
| D21 | Preserve | Sweep continues an existing active purge under its original purge ID before enumeration. The sweep pass has a separate operation identity and exact scope authorization. |
| D22 | Preserve with cost | Receipts, tombstones, generations and fence files retain lifetime identity/idempotency history. Hosts budget this metadata; no timer deletes live locks or retired identities. |
| D23 | Preserve | CanonicalComplete reports canonical deletion; PurgeComplete additionally requires sink acknowledgement. RevocationCommitted still requires cleanup continuation. |
| D24 | Change | Empty-file bootstrap is separate from existing schema3 validation. Missing tables/row/secret or incompatible column/PK layout reject with ErrSchema; secondary indexes can be rebuilt. |
| D25 | Preserve | SQLite busy timeout 25ms and lock retry ceiling 5s remain explicit context-aware reference policy. Scoped fence waiting is separately cancellable; no memory fallback. |
| D26 | Define minimum | Repeated Close is supported; subsequent operations return ErrClosed without callback entry. Hosts drain accepted work; the common contract does not force cancellation or identical barriers. |
| D27 | Preserve | Memory serializes scoped callbacks as a reference tradeoff. Independent scopes remain independent; parallel-reader optimization requires workload evidence. |
| D28 | Preserve | Persistent AVL indexing retains ordered bounded ranges and path-copy rollback. Map-and-sort would restore O(scope) traversal for short pages. |
| D29 | Change and measure | Memory Put checks context/key/read-only/CAS/overflow before cloning accepted bytes. Rejected 1MiB puts allocate zero; accepted ownership stays detached. |
| D30 | Preserve | Static capabilities declare semantics, not current health. Errors report outages; there are no implicit probes or capability downgrades. |
| D31 | Preserve | Store keys are bounded non-NUL byte strings; Scope identifiers are valid Unicode. Neither is normalized, preventing silent identity changes. |
| D32 | Preserve | Search supplies metadata refs/signals. Core retrieves canonical payload, validates scope/state/authority and filters before ranking/projection. |
| D33 | Preserve | RRF stays an optional pure reference policy. Network retries, pools and production retrieval federation belong to consumer adapters. |
| D34 | Define matrix | Composite discards candidates from errored/degraded/unavailable children even with AllowDegraded; usable healthy children may survive. Minimum and cancellation never degrade. |
| D35 | Change docs | ScoreRanker sorts a shallow slice copy; nested payload/provenance/signals retain their ownership. Engine separately detaches callback inputs and validates canonical selections. |
| D36 | Measure and preserve | JSONPacking benchmarks at 100/1000/10000 expose full-body quadratic cost. Keep the exact offline policy; no unproven payload-sum optimization or production throughput claim. |
| D37 | Preserve with cost | Reference Index gathers matching metadata under its mutex. MaxCandidates limits returned data; internal CPU/heap is not bounded by it. Production top-k belongs to adapters. |
| D38 | Preserve | Composite calls children sequentially in deterministic ID order. Latencies accumulate; each child receives the candidate cap. Parallel calls are not a required guarantee. |
| D39 | Preserve profile | Reference minimum visibility requires a caller deadline, including already-ready tokens. Eventual rejects minima; other adapters may expose their own documented compliant waiting policy. |
| D40 | Preserve | Omitted refs are revalidated because omission metadata is disclosure. A forgotten omitted ref invalidates the complete measured delivery. |
| D41 | Preserve with cost | Read/source/retention checks guard different disclosure boundaries. Existing lifecycle/callback work counters remain the measurement harness; no deletion of security checks to optimize fixtures. |
| D42 | Preserve | Unknown validity and conflict are explicit states/options. Unknown is not always-valid; newest revision is not automatically truth. |
| D43 | Define | Consumer JSON null follows encoding/json zero/no-op semantics. Distinct case-alias keys remain consumer decoder semantics after canonical ordering; strict service metadata is separate. Use a custom Codec to forbid aliases. |
| D44 | Define | JSON numbers retain deterministic lexemes. 1, 1.0 and 1e0 may differ in encoded identity; this is not universal semantic numeric equality. |
| D45 | Change | CodecSuite adds mutable decoded-tree and retained Encode-buffer probes; docs require fresh ownership and concurrency safety. Arbitrary host callbacks remain trusted code. |
| D46 | Preserve | Any nonnil field ACL, including an empty slice, is unsupported and fails closed. Consumers partition records rather than request implicit redaction. |
| D47 | Preserve | Current authority/source rechecks stay. Cross-system linearizable revocation requires host leases/transactions; core does not promise distributed IAM atomicity. |
| D48 | Preserve | Merkle membership proofs support bounded purge/corruption detection. They do not authenticate hostile database modifications. No replacement without equivalent guarantees/cost. |
| D49 | Preserve boundary | Reflection validation covers closed service metadata only. New wire fields require schema/validator fixtures; BYOT domain payloads do not inherit metadata field-name rules. |
| D50 | Defer optimization | RFC3339 regexp compilation is a small cost without a reproduced correctness error. Keep behavior until profiling establishes a useful typed/cache change. |
| D51 | Preserve | Restoring a backup requires current revocation reconciliation. The old backup cannot know later Forget operations; no automatic permissive restore fallback. |
| D52 | Change | Grade is nil iff version=none; configured-grader evidence records actual calls or not_applicable separately from the configured version. |
| D53 | Preserve | The quality harness remains internal and consumer-owned, without a public DSL or mandatory evaly dependency. |
| D54 | Change | Procedure/preference/evaluator code is split into setup, oracle, runner and case-family files; no plugin framework is added. |
| D55 | Change | RunProcedureCase shares corpus/port/case/repeat/seed checks and rejects unknown cases before store/provider setup. |
| D56 | Change | Known execution invariant failure is fail/exit1; incomplete execution is unknown/exit2 and takes precedence over known failures. |
| D57 | Change | Malformed supplied metrics add safe invalid_measurement diagnostics/unknown. Missing optional metrics remain unavailable, never fabricated zero. |
| D58 | Change | Quality schemas consolidate repeated versions/stages/measurements with $defs; actual-report validation and dereferenced comparison preserve constraints. |
| D59 | Change | Store, Search, Authority, Sources and Sink fixtures independently vary Tenant, Namespace and Subject. A suite pass is limited to declared cases. |
| D60 | Change | Search fixtures check exact revision upgrades, foreign/unstaged minima and delayed older acknowledgement without loss of the newer visible revision. |
| D61 | Change | Sink fixtures cover multiple refs/chunks/replay and multi-lineage invalidation. Core invalid ack/chunk tests remain separate; CallbackSuite only promises cooperative context/error checks. |
| D62 | Preserve supported scope | Manifest ReadFile remains for trusted local corpus files. No unsupported network/untrusted-corpus processing claim or speculative reader cap. |
| D63 | Preserve limits | Repeats are scripted determinism trials, not statistical quality. Real provider cost and canonical footprint stay unavailable unless measured. |
| D64 | Preserve with maturity | reference mixes offline fixtures and reusable pure policies, each with explicit scope/cost. Capability subpackages can follow real growth; no new repository or wrapper layer. |
| D65 | Preserve | Top-level generic functions introduce consumer query/output types, which Go methods cannot newly parameterize. No OO facade is needed. |
| D66 | Change | scanAll becomes collectAll with explicit O(total entries) time/memory documentation. It does not become a bounded whole-operation API. |
| D67 | Preserve deliberate lint scope | Keep explicit security-state initialization and the current lint gate. Do not add boilerplate or redesign runtime abstractions to match an aesthetic template. |
| D68 | Change, completed in release stage | Release no longer uses BSD sed; it supports only the root module and rejects unsupported major import-path changes. Local fixtures cover portability-sensitive preparation and v2-path mismatch. |
| D69 | Preserve | Metadata-only JSON context can exhaust the cap. BudgetUsage stays out of body; host transport accounting is separate. Greedy order preservation is not optimal knapsack. |
| D70 | Preserve names | Retain/Registry/Policy keep existing APIs with explicit roles. Naming alone does not justify compatibility aliases or manager wrappers. |

## Measured reference costs

[Captured terminal evidence](reviews/remediation-costs.md).

Local macOS arm64 / Apple M1 Max, Go1.27.1; synthetic strings of 100 bytes,
one untimed-setup trial per corpus size, `-benchtime=1x -benchmem`. These are
allocation totals, not peak resident memory or production latency percentiles.

| JSONPacking projections | Time | Allocated bytes | Allocations | Completion |
| --- | --- | --- | --- | --- |
| 100 | 7.877ms | 6,977,040 | 1,357 | Complete |
| 1,000 | 792.838ms | 829,380,392 | 14,581 | Complete |
| 10,000 | 5.000s | 9,210,358,384 | 67,085 | Censored by 5-second context deadline |

The 10,000 case does not report a completed selection or estimate its full runtime.
The measured cost supports an offline/reference designation and small host caps;
it does not justify removing full final JSON accounting. The benchmark retains an
explicit censored/op metric so the deadline cannot be mistaken for success.

Rejected memory Put with a preallocated 1MiB caller payload: 100 iterations,
14.58ns/op, 0B/op, 0allocs/op. Regression fixtures also check zero allocations for
invalid key, cancelled context, read-only, failed CAS and overflow rejection.
These figures are local resource-hygiene evidence, not cross-platform guarantees.


## Composite usability matrix

| Child result | AllowDegraded without minimum | Strict mode or minimum |
| --- | --- | --- |
| Valid ready/eventual/pending metadata, nil error | Candidates fused | Candidates fused only when required minimum is satisfied |
| Valid metadata plus ordinary error | Candidates discarded; other healthy children may survive | Error returned |
| Degraded/unavailable coverage, nil error | Candidates discarded; other healthy children may survive | ErrUnavailable |
| Missing minimum satisfaction | ErrVisibilityPending | ErrVisibilityPending |
| Cancellation, deadline or invalid envelope | Error; no degraded fallback | Error; no degraded fallback |
| No usable child remains | ErrUnavailable | Error |

Thus AllowDegraded preserves usable healthy siblings; it does not promote a
failed child's partial candidates into trustworthy output. Coverage remains
visible evidence and never means complete retrieval of all relevant knowledge.
