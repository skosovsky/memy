# Isolated shell release

`make release-patch` selects the next remote patch version. `make release-break`
increments minor before v1; later major transitions require an explicit reviewed
import-path migration. RELEASE_SOURCE defaults to HEAD and resolves to a commit.
Only the root module is publishable. Development modules never enter its ZIP.

The Bash script rejects tracked/index changes and ambiguous destinations, creates
an isolated source checkout and runs its fresh `make check`. Candidate artifact
and consumer checks run before confirmation. This single-module release does not
rewrite manifests or create an empty commit: candidate SHA equals source SHA.
Only the exact lightweight root tag is pushed, atomically, without force or
fallback. The original checkout, index and refs are preserved.

Confirmation displays source/candidate SHA, version, destination and refs. After
push, exact remote identity and exact-version consumer resolution through
proxy.golang.org with checksum verification are required. Six bounded attempts
within five minutes handle proxy delay. A transport error is reconciled before
retry; unavailable observation or verification is not success.

Recovery records live outside the source checkout and contain validated text
fields, updated by rename. A mkdir lock guards each repository and record; stale
locks require manual inspection. The record path is always printed.

* `scripts/release.sh inspect <state-dir>` observes state and remote refs only.
* `scripts/release.sh resume <state-dir>` continues the same version/candidate,
  revalidates its source and artifacts and confirms before a possible push.
* `scripts/release.sh finish <state-dir>` verifies an already published candidate;
  it never pushes.

States: prepared, publishing, published-unverified, complete, conflict, unknown.
Incomplete records and prepared clones are retained. Different remote identities
block recovery; tags are never deleted automatically. Unsupported legacy records
require recovery using the previous tooling, not automatic migration.

Go fixtures exercise isolation, failures and recovery against local remotes.
Implementation acceptance does not authorize a production release.
