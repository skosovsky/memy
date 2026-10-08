# Development checks

The development modules are `.`, `tools`, and `integration/consumer`; only the
root module is published. All commands use GOWORK=off. Go 1.27.1 and
 golangci-lint v2.14.0 are required; `make install-tools` installs the pinned linter.

`make test` runs fresh race tests in every development module. `make lint` checks
formatting, vet and lint without editing files. `make examples` builds examples.
`make check` checks prerequisites and tracked module inventory, then runs lint,
tests, example builds and mandatory integration profiles in order. Independent
stage failures are collected and produce a nonzero result. `V=1` prints commands.
`make test-integration` checks candidate artifacts and the pinned published
consumer baseline. Network/tool failures are errors, never skipped acceptance.

Consumer tests have fixed peers and use the current root source through a
development-only replace. Published checks use v0.3.1 by default, no replaces,
and a separate module cache. `make test-published MEMY_REF=vX.Y.Z` verifies an
explicit published version. No sibling checkout or go.work is required.

Tooling tests use temporary repositories and local bare remotes. They never
publish production refs. Historical performance reports and measurements remain
archived; their Python generators are retired and report regeneration is no
longer a supported development command.
