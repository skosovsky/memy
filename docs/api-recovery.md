# API recovery and adapter boundaries

The engine uses host-owned payload, source and actor types. Required Config ports
are Store, Authority, Sources, Clock, Retention, PayloadCodec and ReferenceCodec.
Sinks is optional: its exact list defines managed deletion. Reintroduction is
optional: nil denies reuse of retired identities. Extractor, search, ranker,
projector, budget and consolidation policies are supplied to the operation that
uses them. They are not permissive Config defaults.

## Operation and recovery matrix

| Operation | Successful boundary | Error or interruption | Host recovery |
|---|---|---|---|
| Remember / Extract / Revise | Stored proposal, no canonical truth | Invalid/schema/missing evidence/source unavailable; stale inputs | Correct inputs or repair source availability; review returned proposal before Accept |
| Accept / Commit | Acceptance binds proposal; Commit conditionally persists canonical state and receipt | Stale acceptance/input, conflict, denied policy, unknown commit outcome | Re-review stale proposals. Recover unknown outcome with the original operation identity and unchanged request; a different identity can duplicate effects |
| Replay of durable operation | Same bound request recovers its stored result | Same operation ID with different actor/purpose/input conflicts | Preserve original binding; recover receipt before choosing a new reviewed operation |
| Get / Snapshot / Recall | Authorized canonical read; Recall revalidates search metadata | Denied/not found/revoked, visibility pending, unavailable, stale cursor | Do not infer existence from denial. Wait for declared search visibility; restart stale scans; finish maintenance before reading fenced scope |
| Project / RecallProjected | Output corresponds to the complete effective input snapshot; packing bounds the full JSON body | Stale input, denied policy, unavailable, budget exceeded | No output is delivered on stale input; make a new explicit call after reviewing current input. Do not silently repeat model callbacks or use cache to bypass current authorization |
| Forget | Durable revocation first, then bounded canonical cleanup and exact sink acknowledgements | Unknown outcome; pending/failed sink; error may accompany durable receipt | Keep original Forget ID and request. Inspect receipt even on error; retry explicit bounded pages or allow Sweep to adopt this purge |
| Sweep | Per-call confirmed committed deltas, durable pass identity | Error with partial result, unknown current transaction, pending sink, exhausted per-call allowance | Consume result and error together; aggregate confirmed results and resume the same Sweep ID. No exactly-once metric guarantee after lost responses or unknown commit outcomes |

`errors.Is(err, ErrMaintenance)` identifies a transient active purge/sweep gate;
that gate also matches ErrRevoked. Continue the durable job to remove the gate.
Retired identities remain ErrRevoked without ErrMaintenance; waiting does not
permit reintroduction. Invalid input and unsupported schema/capability require
correction or explicit migration. ErrPolicyDenied/ErrUnauthorized require a new
host decision, not an automatic retry. ErrUnavailable/ErrSourceUnavailable are
fail-closed availability failures. ErrBudget is explicit inability to fit the
requested boundary, not a completed traversal.

## Purge and work accounting

RevocationCommitted means the durable revocation is installed; traversal may
remain. `CanonicalComplete` becomes true only when selected canonical content
and origins have been cleaned. It can be true with PurgePending/PurgeFailed
while a managed sink remains outstanding. Only PurgeComplete proves completion
of the canonical and registered sink boundary, including all exact chunks.
Already delivered answers, unmanaged caches/backups and forensic disk erasure
are outside this receipt. Restore the current revocation ledger before serving
restored canonical content.

Sweep Limit bounds charged allowance (`BudgetCharged`), not measured CPU/I/O.
MaxBytes bounds each storage page, not the full result or total pass. Empty pages
and callback allowance can be charged conservatively. Bound collected receipts
separately in the host. [Lifecycle example](../examples/lifecycle/main.go)
recovers a failed Forget sink through bounded Sweep calls; the adopted receipt
keeps the original Forget ID while the pass keeps its own Sweep ID.

## Ownership and concurrency

| Boundary | Contract |
|---|---|
| Payload/reference codecs | Detached encode/decode ownership; stock json/v2 preserves generic numeric lexemes as json.Number and rejects malformed Unicode. Custom codecs must satisfy CodecSuite; arbitrary dynamic numeric Go types are not preserved |
| Canonical callback inputs | Engine detaches BYOT content through configured codecs. Host callbacks must honor context and their port contract; ranker copy alone is shallow |
| Authority/Sources/Retention/Clock/codecs | Bounded local operations; no remote I/O or recursive entry into the same Store from callbacks inside transactions/fences |
| Projector/ranker/merge | Outside store transactions; fresh final canonical checks reject changed state. External callbacks may observe their input before a later failed delivery; no distributed transaction with model/IAM providers |
| Store View / Update / FencedView | Consistent read / scoped atomic conditional mutation / exact-scope exclusion until callback ends. Nested store entry can deadlock; host coordinates scheduling |
| Store Close | Closed admission returns ErrClosed; repeated Close succeeds. Host cancels and drains its callers. Portable minimum does not require forced cancellation or one universal callback-drain barrier |
| Snapshot / Scan | Explicit bounded pages and authenticated continuation; changed binding/generation is stale. Snapshot budget charges scanned revisions, not just selected results |

## Adapter and platform matrix

| Adapter | Profile and requirements | Verification scope |
|---|---|---|
| Memory | Standard-library process-local store, owned byte copies; no restart durability | Common conformance, concurrency and race fixtures |
| SQLite | CGO and pinned driver; local filesystem with supported Unix file locks; schema 3 and persistent `.memy-fences` directory while DB is in use | macOS arm64 local temporary files, reopen/fault/conformance/race fixtures |
| SQLite platform fencing | darwin, linux, freebsd, openbsd, netbsd, dragonfly build paths; other OS profiles return ErrUnsupported | Build support is not validation of every OS/filesystem deployment |
| Host adapters / sinks | Advertise honest capabilities and pass conformance; direct Store/search access is privileged | Remote backends, production durability/quality and forensic erasure are not certified by local fixtures |

Existing SQLite stores with missing required tables/schema row, malformed secret
or wrong required column/primary-key shape fail ErrSchema. Only empty databases
bootstrap; a missing secondary index may be repaired. Structural checks do not
authenticate a hostile database or prove its whole SQL constraint history.
