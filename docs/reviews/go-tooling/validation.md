# Go tooling acceptance — 2026-10-08

Implementation replaces all four tracked Python files with Go tooling and consumer
modules. Core Go sources, root go.mod/go.sum and persisted schemas are unchanged.
Only the root module is publishable; actual candidate ZIPs exclude both development
modules. The fixed consumer graph has no published replacements or latest queries.

## Verification

| Platform | Full check | Root fresh race suite | Final tooling verification |
|---|---|---|---|
| macOS arm64, Go 1.27.1 | make check, exit 0 | 608.790s | strict lint: 0 issues; fresh tooling race PASS |
| Linux arm64, Go 1.27.1 | make check, exit 0 | 710.090s | strict lint: 0 issues; fresh tooling race PASS |

Full checks include three development modules, five example builds, candidate
artifact installability/builds and the v0.3.1 public baseline. Eight consumer
semantic tests execute against current source and both integration graphs.
Published graphs use public proxy and checksum verification; candidate-only memy
checksums are admitted through an isolated local proxy, with peer verification
retained. Missing tools/network fail the required profile rather than skip it.

Full runs preceded the last tooling-only deadline refinements. Final strict lint
and fresh tooling race tests were rerun on both systems; core did not change.
Retained [macOS full output](check-macos.txt), [macOS final lint](lint-macos.txt),
[macOS final tooling](tools-macos.txt), [Linux final lint](lint-linux.txt) and
[Linux final tooling](tools-linux.txt) document those runs. The first successful
Linux full terminal log was overwritten during supplemental checks; its exit 0
and root timing above were directly observed. It is not presented as a retained
frozen manifest. Interrupted Linux attempts are not passing evidence.

Linux used the official Go 1.27.1 Bookworm image (manifest digest
sha256:8d48e12ec56735e9358640898b9d9b9fcca110612ed8a5567438c0a1baa24e66),
with writable caches outside the image. The complete Linux check ran without
python or python3 in PATH. No sibling checkout, go.work or live provider was
required. Temporary test containers were removed; shared Docker resources were
not pruned. CI defines the same make check contract on Ubuntu and macOS; remote
GitHub jobs have not been run for these local commits.

## Behavioral coverage

Ten original release scenarios are retained among twenty table cases, plus a
blocking-verification watchdog case. Assertions cover caller HEAD bytes/index/
tracked state/refs, exact source selection, dirty/staged and detached operation,
remote-derived versions and numeric-bound rejection, rejected atomic push, unavailable atomic capability,
lost response, unknown outcome, remote conflict, legacy records, locks, failed
source/candidate checks and finish/resume of the same candidate. Retry fixtures
accelerate backoff without changing production delays. A blocking verifier is
killed by the actual shell watchdog; six attempts cannot produce false success.

Inventory rejects missing/duplicate modules and publishing tooling. Make fixtures
prove ordered stages with -j8, prerequisite failure, multiple independent failed
stages, forced fresh race flags and failure during fuzz discovery. Checks use
readonly Go flags. Final git diff checks pass; no tracked Python files remain.

Publication/recovery stays in portable Bash, tested on macOS system Bash 3.2.
Public verification allows up to six attempts, per-attempt process bounds and
exponential backoff within a five-minute total deadline. Service state is text,
not executable configuration. Unknown/unverified outcomes retain the candidate.

No production tags, release, PR, issue closeout or push was performed. Issue #2's
actual publication requirement remains outstanding. This verifies infrastructure
and local fixtures, not a production release or live-provider behavior.
