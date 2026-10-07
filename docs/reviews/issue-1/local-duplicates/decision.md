# Local duplicate disposition

All three original files were copied byte-for-byte to `.txt` files in this directory
before the originals were removed from Go/script source directories. The manifest
records their original paths and SHA256. Full unified diffs are retained beside
those copies. No build exclusions or CI workarounds were added.

The alternate corpus and quality files implement an older monolithic corpus:
`Records/Expected/BadSemantic`, a baseline/experiment trial, and private session
helpers. Current `internal/quality` uses typed preference/procedure/evaluator
scenarios with explicit checkpoint oracles, budgets, mutation probes and structured
reports. Their corpus types are incompatible; merging old declarations would
restore obsolete public/internal contracts. The duplicate-only `validateExpected`
checks equality with the old scenario's fixed facts; those expected outcomes are
now checked in preference/procedure oracles instead. `fullSnapshot/fullForget`
from the alternate file match the paginated helpers already in `helpers.go`, with
current argument order and stricter field initialization. No unique current-schema
behavior was identified for merging. The complete old implementation is preserved
so this decision is reversible.

The alternate release script predates the current exact-publication and unknown
push outcome recovery behavior. Current release fixtures exercise the implemented
contract; the alternate script is preserved in full, not executed or silently
substituted. Workstation cleanup does not diagnose a remote source regression.
