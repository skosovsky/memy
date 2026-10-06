# Release entrypoint follow-up

After the accepted strict-lint snapshot, the user requested a short commit and
`make release`. The Makefile now exposes that entrypoint with `RELEASE_TYPE=patch`
by default, retaining the explicit patch/break targets and full validation.
README documents the same interface. A dry-run with `RELEASE_TYPE=break` invokes
`./scripts/release.sh "break" "."` after all validation prerequisites.

Only Makefile and README differ from the 127-file accepted hash manifest. Go
sources, strict lint configuration, dependencies, the supplied release script,
corpus and golden report retain their tested hashes. The original manifest and
acceptance evidence remain the historical snapshot of that completed run.

The next release is breaking because the unpublished commits after v0.1.0 change
public contracts. The observed remote has only v0.1.0; the script therefore
selects v0.2.0 for this release. Publication is verified separately after running
the actual release target.
