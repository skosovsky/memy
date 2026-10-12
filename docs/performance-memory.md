# Reference adapter memory and search costs

`reference.Index` stores visible and pending metadata in exact-scope maps.
Search visits only the requested scope, including pending matching checks. A pure
BYOT matcher may require visiting every candidate in that scope. For V visible
entries and limit K, selection uses O(V log K) work, O(K) temporary candidate
storage, and preserves `candidateOrder`, native scores, ranks and coverage.
The mutex still serializes index operations; this is a reference adapter, not a
production search engine. Returned candidates own their slice and signals;
capacity is at most K and omitted metadata is not retained in a hidden tail.
Empty scope maps are removed by Acknowledge/Purge; a delayed acknowledgement
cannot resurrect purged metadata. Global visibility notifications are preserved.

`reference.Composite` still needs all child results for exact RRF fusion; its
working storage depends on backend count and each child's K. Its returned
candidate slice is detached when fusion is truncated, so omitted candidates and
their signals are not retained by the returned slice.

`store/memory` counts both executing operations and gate waiters while holding
the registry mutex. A scope can be reclaimed only after its last registered user
finishes and only when it has never committed data or a generation change.
Cancellation and callback panic release registration too. Written state, CAS
tombstones and nonzero generations are kept until Close, including after all live
keys have been deleted. Close still prevents new operations and waits for started
callbacks. No expiry, public interfaces, schemas or BYOT behavior changed.
