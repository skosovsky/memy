# Final independent correctness review

Current review snapshot: `cc7e1b931c98d807b3c23e7c4b35c45ffaf3d2b3d572616ee0301cc5659b0421` (203 files). Implementation/CI fix, final patch publication and remote CI accepted with no open confirmed defects. Terminal gate evidence is recorded in the final addendum; issue closure itself remains the coordinating agent’s last action. Earlier publication acceptance below records the preceding snapshot and is superseded for final closeout by the CI correction addendum.

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

This verdict covers the inspected source snapshot and tests above. At the initial source review, full validation and publication gates were pending. The addendum below records their subsequent verification. Migration communication and issue closure still require the coordinating agent to execute and verify closeout. The selected network backend test covers the HTTP/SQLite example; it does not certify unrelated production backends, replication, HA, backup restoration of stale canonical authority, or forensic erasure.


## Final external gate acceptance

The reviewer inspected `validate.txt`, `release-break.txt`, `consumer-published.txt`, `consumer-published.json`, and `artifact-verification.json` after publication. Both validation logs contain successful test results for all 11 test packages, all five runnable examples, lint reporting zero issues, and release-script contract tests completing successfully. No failed test or make failure appears. The release log proceeds through the prerequisite validation and records successful publication by the standard `make release-break` path.

Published artifact: `v0.3.0`, commit `fff101ecf3758ecdec015561a1338fc61d202446`. This reviewer independently executed `git ls-remote --tags origin refs/tags/v0.3.0`; the remote tag points to that exact commit.

This reviewer independently recomputed the aggregate SHA256 of all 161 `.go` files in the current reviewed source and in the downloaded published module directory. Both match `d64bf6765c4b0e072f18199fc83670b2d5483beafee76f3eb4ee05748b0121a1`. The final 202-file source snapshot continues to match `5d517d4d29900935f1ead6d841e8c46d347abeb80622843b247a176e4cedb729` without mismatches.

The published consumer manifest resolves memy to the new published tag, sets `GOWORK=off`, and contains no Replace entries anywhere in the resolved graph. The source-reviewed script executes the temporary consumer suite with `-mod=readonly -race -count=1`; all eight semantic tests pass in the publication log, including canonical recall/materialization/Forget, stale-source filtering, durable compiled context purge, isolation/bounds/cancellation and consumer toolkit composition. The record is a semantic integration result, not only a successful dependency resolution.

Terminal implementation acceptance: **accepted; no open confirmed actionable defects and no remaining implementation, validation, publication or published-consumer gate blocker found**. Proceed with the required explicit migration comment to the issue author and then close the issue. This reviewer does not claim that those final communication/closure actions have already occurred. Previously stated backend/HA/backup/forensic-erasure limits continue to apply.


## CI correction review

After the preceding implementation review, the actual GitHub consumer-local job revealed a portability failure: cloning a consumer default branch did not select the compatible API exercised by the local sibling and published lanes. The failure is real and was not dismissed as a core defect or hidden by the earlier passing local-sibling result.

Final corrected snapshot: `cc7e1b931c98d807b3c23e7c4b35c45ffaf3d2b3d572616ee0301cc5659b0421` (203 files). All hashes match at review start and end. Only README guidance, the portable consumer script, and the new `testdata/consumer/revisions.json` change from the accepted implementation snapshot. No Go source changed: independently recomputed 161-file Go SHA remains `d64bf6765c4b0e072f18199fc83670b2d5483beafee76f3eb4ee05748b0121a1`.

The portable local lane now clones each selected compatible consumer tag and checks `git rev-parse HEAD` against its recorded exact commit before using that checkout. This prevents default-branch API drift or silently moved tags from passing as the reviewed consumer revision. Explicit `--siblings` continues to use the actual supplied checkout and records both its HEAD and source digest. Core receives no consumer imports or new mandatory dependencies.

Inspected `consumer-cloned.txt`: all three actual clone messages resolve to the exact manifest commits; eight race semantic tests pass in the portable clone path, and the emitted manifest records `work=off`, the matching clone commits and result `pass`. Inspected `validate-ci-fix.txt`: full validation and release-script tests complete without failures.

An independent isolated Python probe imported the script and supplied a mismatched clone commit through a mocked command runner. The script selected `--branch v0.8.0`, rejected the mismatch with `Consumer revision changed for ragy`, and never started semantic tests. Probe-created bytecode was removed; no source was changed.

Verdict for corrected source: **accepted; no open confirmed actionable implementation/CI-script defects**. The previously published substantial API release remains immutable. The CI-only correction is eligible for the planned patch release after this review. This review does not yet certify publication or remote GitHub CI for that patch; those final gates must pass before migration comment and issue closure.


## Terminal acceptance after CI patch publication

Final patch: `v0.3.1`, commit `27b788e8d56ee33ccb3dd5a8fc99d5dee75fedff`. Inspected the standard `release-patch.txt` publication log. Independently reran `git ls-remote --tags origin refs/tags/v0.3.1`: the remote tag resolves to that commit.

Independently compared the downloaded `v0.3.1` module directory against all entries of the final 203-file snapshot: 202 published files match their SHA256 exactly; the only absent file is the intentionally local ignored `.cursor/docs/task1.md`. No mismatch was found, including the consumer script, pinned revision manifest, docs and core sources. Local snapshot hashes still match `cc7e1b931c98d807b3c23e7c4b35c45ffaf3d2b3d572616ee0301cc5659b0421`.

Inspected `consumer-published-patch.json` and its semantic test output: published result pass, `GOWORK=off`, no Replace entries, all eight semantic race tests pass. Inspected the actual downloaded remote CI logs: eight tests pass in each consumer lane and 838 test/subtest PASS entries occur in contracts with no test failures or job error annotations. Both consumer jobs and the contracts job completed successfully. Independently queried GitHub run `37602453534` using `gh api`: status completed, conclusion success, head SHA the final patch commit. This proves the portable clone correction runs successfully in its real GitHub CI environment.

Independently read author migration comment `6035357157` from the issue API. It addresses the author explicitly and covers actual before/after score construction, evidence, full-envelope budget, authenticated scope, separate approval, sink registration, fence and full lineage, synchronous writes, durable handles and pending purge retries, unknown effects, restore invalidation and host revocation-ledger responsibility. The comment accurately stated CI was pending when posted; the coordinating agent can now record successful final CI and close the issue.

**Terminal correctness acceptance: accepted. No open confirmed actionable defects or remaining implementation, publication, consumer or remote CI blockers were found.** Migration communication is verified; proceed with final closeout/state verification. This reviewer has not closed the issue and does not claim it is already closed. The review’s stated finite test/code-inspection and selected-backend limits remain in force.
