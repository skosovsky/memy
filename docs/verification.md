# Repository verification

Make invokes standard Go commands and golangci-lint directly. Modules are discovered
from go.mod files, excluding hidden directories and vendor. All commands use
GOWORK=off. There is no aggregate check target or tool version validation target.

| Command | Scope |
|---|---|
| `make modules` | List all discovered development modules. |
| `make test` | Fresh ordinary race tests across all modules. |
| `make test-integration` | Files with integration build tag; execute TestIntegration… functions only. |
| `make test-e2e` | Files with e2e build tag; execute TestE2E… functions only. |
| `make test-live` | Files with live build tag; execute TestLive… functions only, including paid calls. |
| `make lint` | Formatting diff and lint without rewriting files. |
| `make fix` | Go fix, formatting and lint fixes; modifies files. |
| `make fuzz` | Every discovered fuzz function separately, 30 seconds per function. |
| `make bench` / `make cover` | Benchmarks / per-module coverage. |

Build tags alone do not exclude ordinary test files. Profile targets combine the tag
with a matching test-name prefix so a module without such tests executes none.
Use the same convention for new tests; each profile runs directly through Go too:

```sh
GOWORK=off go test -race -tags=integration -run '^TestIntegration' ./...
GOWORK=off go test -race -tags=e2e -run '^TestE2E' ./...
```

Recipes use tools from PATH and explicitly propagate command failures.
Tool versions are pinned in CI, not enforced by Make. CI and source release gates
run lint, fresh unit tests, integration and e2e sequentially.

## Project content

The modules are the root library (including examples), tools, and integration/consumer.
All discovered modules are tested and published; module paths follow github.com/skosovsky/memy and each directory. There is no separate publication inventory.

SQLite requires CGO_ENABLED=1 and a C compiler. Cross-process fencing uses integration; TCP checkpoint recovery and consumer lifecycle use e2e. Consumer compatibility uses integration with pinned ragy/contexty/toolsy dependencies and a local root replace.

Candidate integration checks construct artifacts for every module and remove internal replaces only in a temporary copy. Published e2e checks use memy v0.3.1 by default, checksum verification and an isolated module cache. MEMY_REF selects an explicit published version. Network/tool failures fail selected profiles. No Docker, PDF runtime, Python, sibling checkout or go.work is required.

Live-provider tests are currently absent. Historical benchmark reports remain archived; their retired Python generators are not supported tooling.
