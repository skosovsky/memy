package memy

// ScanOptions bounds live prefix traversal in one consistent transaction.
// Cursor is opaque, authenticated, and bound to scope/prefix/store generation.
type ScanOptions struct {
	Prefix   string
	After    string
	Plan     string
	Cursor   string
	Limit    int
	MaxBytes int
}

// ScanPage never treats a partial or stale traversal as a complete snapshot.
// Bytes counts returned key and value bytes, excluding envelope overhead.
type ScanPage struct {
	Entries  []Entry
	Cursor   string
	Complete bool
	Bytes    int
}

// scanAll is an internal full-traversal operation implemented in bounded pages.
// Callers that need host continuation use the lifecycle page APIs instead.
func scanAll(b Bucket, prefix string) ([]Entry, error) {
	options := ScanOptions{Prefix: prefix, Limit: 256, MaxBytes: int(^uint(0) >> 1)}
	var result []Entry
	for {
		page, err := b.Scan(options)
		if err != nil {
			return nil, err
		}
		result = append(result, page.Entries...)
		if page.Complete {
			return result, nil
		}
		if len(page.Entries) == 0 || page.Cursor == "" {
			return nil, ErrSchema
		}
		options.Cursor = page.Cursor
	}
}
