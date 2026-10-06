# Local release preparation and exact publication

The script supports this single-module repository only (MODULES = .). Release
files have an empty edit allowlist: no go.mod rewrite or release commit is needed;
the new lightweight tag points at the initial committed HEAD; tag signing is
explicitly disabled for this representation. Attached and detached HEAD are
supported. Tracked staged/unstaged changes are rejected; unrelated untracked
files are neither copied nor published. The original HEAD, index, worktree and
local refs remain unchanged, including on failure.

Prepare in a disposable clone without importing local tags. Determine the next
version from the push destination's published semantic version tags, never local
unpublished tags. patch increments patch; break on major zero increments minor.
Major transitions beyond v0 require an explicit semantic import-version change
and are rejected by this script. A patch on an already published major >=2
requires a matching /vN root module path. No generic multi-module release framework is
provided. Only one tag is published with an exact refspec; no multi-ref transaction
is claimed. Remote advancement between discovery and publication fails safely.

The interactive confirmation authorizes publication of the displayed version
and commit. States are local-prepared, published, rejected or unknown. A failed
push is reconciled by querying the exact remote tag: matching commit means
published, absent/different tag means rejected, failed observation means unknown.
For unknown outcome, retain the prepared clone and print its recovery path and
exact tag/commit. Observe that remote ref before retrying or deleting the clone;
do not blindly delete refs after transport failure. Definite failure removes the
disposable preparation only, leaving original refs intact.

Fixtures use temporary repositories and local bare remotes, including rejecting
hooks; they never publish a real release. Run python3 scripts/release_test.py.
The implementation uses portable Bash/Git operations and no BSD sed or sort -V.
