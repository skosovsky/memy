# Independent correctness review before release

Reviewed source snapshot: `3072e9ac7f7f007c3c8e84a6edaca58507e0e26046821f19623b70988643fec0` (202 files). All source hashes matched at review start. No production or test sources were changed by this reviewer. This is a bounded review, not a proof of absence of all errors.

## Confirmed findings

### P1: checkpoint delivery can return bytes after authority or source invalidation during final I/O

Location: `examples/managed-projections/checkpoint/delivery.go:36-47`.

`WithDerivedWrite` validates lineage, authority and deadlines before invoking its callback. The callback then performs a potentially slow second `Reader.Load`, captures bytes, and returns only `ctx.Err()`. There is no fresh eligibility/authority check after this external I/O. Canonical revoke is excluded by the fence during the callback, but host authority, source revisions and clock deadlines are not held stable by that fence.

Reproduction: a `Reader` wrapper delegates both loads to the real SQLite checkpoint store and synchronously changes the host policy on the second load before returning. Removing the source instead also reproduces. Seed, recall and persistence use the real example session and canonical SQLite. Both return a complete 1090-byte checkpoint with nil error despite invalidation preceding delivery. The standalone probe is `/tmp/memy-correctness-probe.go` and uses no sleeps or production modifications.

```
authority revoked during final Load: bytes=1090 err=<nil> loads=2
source removed during final Load: bytes=1090 err=<nil> loads=2
```

Required fix: validate authority and all exact lineage/deadlines after loading bytes and before serving. Preserve canonical revoke exclusion at the delivery gate; do not recursively enter the same canonical store from its callback. Add deterministic regressions for authority/source changes and expiry during the checkpoint load. Errors must return no bytes and invalidate/recompile according to the declared host contract.

### P2: HTTP failure status loses unknown-effect classification after an actual durable commit

Location: `examples/managed-projections/checkpoint/remote.go:117-118`.

Every non-200 status becomes only `ErrUnavailable`. A trusted gateway may return 502 after the service committed the request. This contradicts the adapter's explicit unknown-outcome contract and prevents callers using `errors.Is(err, ErrUnknownOutcome)` from selecting reconciliation.

Reproduction: an HTTP wrapper calls the real credential-protected checkpoint Handler into a recorder (committing a real SQLite Put), then emits 502 instead of the recorded response. Remote.Put returns unavailable without unknown-outcome; direct checkpoint Load proves the external effect persisted.

```
gateway status 502: err=memy: unavailable unknown=false unavailable=true
effect after gateway 502: data=secret err=<nil>
```

Required fix: classify responses whose execution outcome cannot be established as `ErrUnknownOutcome` (possibly joined with availability/status context), especially gateway/server failures. Only documented definite pre-dispatch rejection may claim a known no-effect outcome. Add a real-commit/gateway-error regression alongside lost-reply coverage.

## Scope and limits

Reviewed optional score validation/order and native-vs-ranking evidence, Composite RRF ordinal/native-rank separation, detached ranker and packing inputs, full JSON byte measurement, canonical refresh/revalidation, SQLite artifact/lineage atomicity, structural scoped keys, monotonic epoch fences and old purge retries, checkpoint restoration delivery, HTTP unknown effects and credential boundary, and canonical revoke plus remote restart integration. No additional confirmed actionable defect was found in those inspected paths.

Release, published-artifact consumer verification and issue closeout were intentionally pending and were not certified by this review. Existing targeted race tests cannot cover the new deterministic scenarios above until regressions are added.

## Executed verification

- `go run /tmp/memy-correctness-probe.go`: reproduced both findings above against this frozen snapshot.
- `go test -race ./examples/managed-projections/checkpoint ./examples/managed-projections/host ./reference -count=1`: passed (checkpoint 1.864s, host 2.631s, reference 1.426s). These passing existing tests do not negate the demonstrated missing scenarios.
- Rechecked all 202 manifest hashes at review end: no mismatches.

Verdict: release acceptance rejected for this snapshot; P1 and P2 remain open confirmed defects.
