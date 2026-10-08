# Pinned ragy infrastructure acceptance — 2026-10-08

Reference: skosovsky/ragy commit 7baab34bdde64f1f6b1b99e89ab66d0b072fc6c5.
Makefile and scripts/release.sh are byte-for-byte copies. No production release,
push or issue closure was performed. The five existing local commits are preserved.

## Necessary adaptations

- Lint: memy local import prefix; remove ragy type/adapter ignore patterns and its
  contracttest fixture exclusion. Keep shared settings and generic exclusions;
  add no memy-specific exception. Clarify the local replacement comment.
- CI: remove PDF/Python prerequisites, enable CGO for SQLite. Keep pinned Go 1.27.1,
  golangci-lint 2.14.0 and the reference lint/unit/integration/e2e commands.
- Preserve actual tools and integration/consumer module paths. All three discovered
  modules are publishable; nested ZIPs remain separate from the root ZIP.
- Apply integration/e2e tags and test prefixes. All eight consumer scenarios remain
  enforced, including JSON-event assertions against zero-test success.
- Historical published baseline is root-only memy v0.3.1, including explicit
  MEMY_REF=v0.3.1. Candidate checks install every discovered module from a temporary
  file proxy and verify exact identities, internal versions and absence of replaces.
- Replace obsolete inventory/check/release fixtures with tests for the reference
  discovery and protocol. Documentation uses the reference command contracts.

## Results

| Check | macOS arm64 | Linux amd64 |
|---|---|---|
| make modules | ., integration/consumer, tools | same |
| make lint | exit 0 | exit 0 |
| make test | exit 0; root 539.827s | exit 0; root 567.365s |
| make test-integration | exit 0; artifacts 87.795s | exit 0; artifacts 211.221s |
| make test-e2e | exit 0; published 102.127s | exit 0; published 190.913s |
| Profile lint, integration,e2e tags | 0 issues | 0 issues |

Final extended release fixtures additionally passed macOS race tests (50.832s)
and Linux race tests (156.628s). macOS ordinary tooling lint passed after those
extensions. Explicit historical baseline passed separately on macOS (85.342s).
Linux ordinary lint preceded the final fixture extensions; profile lint followed
all Go changes. An optional second ordinary tooling lint was interrupted when the
completed container exited; it is not counted as a successful check.

macOS fixtures invoke /bin/bash 3.2.57. Linux uses an isolated official
Go 1.27.1 bookworm container. Python executables were moved outside PATH only in
that disposable container before all Make checks. No source Go dependencies or
checksums changed. Linux's final git status was empty; macOS checks produced no
tracked changes beyond intentional documentation/configuration edits.

Release scenarios use disposable bare remotes: source/candidate identities,
caller HEAD/index/file bytes/modes/refs, dirty/detached rejection, source gates,
versions, nested tags, manifest-only preparation, atomic rejection, lost response,
unknown inspection, remote conflicts, locks and recovery. Real remote publication
and GitHub CI execution remain unperformed. Logs alongside this report record the
local commands; historical reports remain unchanged.
