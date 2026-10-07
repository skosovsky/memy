# Final independent correctness review

Reviewed frozen source snapshot: `5d517d4d29900935f1ead6d841e8c46d347abeb80622843b247a176e4cedb729` (202 files). All recorded hashes matched at the beginning and end of review. Production and test sources were not changed by this reviewer.

Verdict: **no open confirmed actionable defects found in the inspected implementation**. Both findings from `correctness-pre-release.md` are resolved. This is the result of a bounded code review and executed checks, not a mathematical guarantee of absence of errors.

## Previously confirmed defects

- P1 resolved: `checkpoint.ReadValidated` completes checkpoint loading, clones data and lineage, validates structural identity, and then performs the canonical fence and lineage/authority gate. The final gated callback performs no external I/O. A host-state change during storage I/O is now observed before delivery.
- P2 resolved: HTTP non-200 responses include both `ErrUnavailable` and `ErrUnknownOutcome`; an error response after durable commit no longer implies a known no-effect outcome.

Independent reproduction was rerun against this snapshot with `/tmp/memy-correctness-final-probe.go`, adapting the original changing Reader to the single storage load. It uses the real example canonical/checkpoint SQLite stores and changes authority or source state during the storage load. Results:

```
authority revoked during final Load: bytes=0 err=memy: unauthorized loads=1
source removed during final Load: bytes=0 err=memy: source unavailable
memy: source unavailable loads=1
gateway status 502: err=memy: unavailable
memy: commit outcome unknown; retry operation identity unknown=true unavailable=true
effect after gateway 502: data=secret err=<nil>
```

The gateway probe commits a real checkpoint Put via the authenticated Handler before replacing the response with 502; a subsequent local Load independently confirms its durable effect.

## Executed checks and inspection scope

`GOCACHE=/tmp/memy-audit-go-cache go test -race ./examples/managed-projections/checkpoint ./examples/managed-projections/host ./reference -count=1` passed:

- checkpoint: 1.615s;
- host: 2.705s;
- reference: 1.380s.

The temporary cache was used because the default Go build cache was inaccessible in the review sandbox. The independent probes and targeted tests completed successfully using the temporary cache.

Reviewed optional scores and malformed-number validation; absence/zero/negative ordering; native evidence preservation and ordinal RRF rank separation; ranker and packing mutation isolation; full JSON byte budget; canonical rereads and final gates; checkpoint data/lineage transaction atomicity; scope structural keys; monotonic backend epoch fencing; old purge retries; checkpoint restoration and stale/missing/unavailable lineage fail-closed behavior; HTTP response loss and gateway error outcomes; service credential boundary; canonical revoke with remote durable deletion and restart recovery.

Added regressions were inspected, including after-storage-I/O validation, missing canonical revision, unavailable verification, and durable commit followed by gateway 502. The independent probes additionally force authority/source changes during the single Load in the fixed implementation.

## Limits

This verdict covers the inspected source snapshot and tests above. Full `make validate` is an independent release gate being executed by the coordinating agent; this reviewer did not certify its unfinished parallel run. Published-artifact consumer verification, release publication and issue closeout remain external gates and are not claimed complete here. The selected network backend test covers the HTTP/SQLite example; it does not certify unrelated production backends, replication, HA, backup restoration of stale canonical authority, or forensic erasure.
