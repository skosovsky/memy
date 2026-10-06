package quality

import (
	"context"
	"github.com/skosovsky/memy"
	"slices"
)

func fullSnapshot[P, R, A any](e *memy.Engine[P, R, A], ctx context.Context, authority A, scope memy.Scope, read memy.ReadOptions) ([]memy.Record[P, R], error) {
	options := memy.SnapshotOptions{Read: read, Limit: 256, MaxBytes: int(^uint(0) >> 1)}
	var records []memy.Record[P, R]
	for {
		page, err := e.Snapshot(ctx, authority, scope, options)
		if err != nil {
			return nil, err
		}
		records = append(records, page.Records...)
		if page.Complete {
			return records, nil
		}
		if page.Cursor == "" || page.Scanned == 0 {
			return nil, memy.ErrSchema
		}
		options.Cursor = page.Cursor
	}
}

func fullForget[P, R, A any](e *memy.Engine[P, R, A], ctx context.Context, authority A, scope memy.Scope, purpose string, request memy.ForgetRequest) (memy.PurgeReceipt, error) {
	if request.Limit == 0 {
		request.Limit = 256
	}
	if request.MaxBytes == 0 {
		request.MaxBytes = 64 << 20
	}
	ids := make(map[string]bool)
	for {
		receipt, err := e.Forget(ctx, authority, scope, purpose, request)
		if err != nil {
			return receipt, err
		}
		for _, id := range receipt.Batch.Records {
			ids[id] = true
		}
		unattempted := false
		for _, sink := range receipt.Sinks {
			if !sink.Acknowledged && sink.ErrorCode == "" {
				unattempted = true
			}
		}
		if receipt.State != memy.RevocationCommitted && !(receipt.State == memy.PurgePending && unattempted) {
			receipt.Batch.Records = make([]string, 0, len(ids))
			for id := range ids {
				receipt.Batch.Records = append(receipt.Batch.Records, id)
			}
			slices.Sort(receipt.Batch.Records)
			return receipt, nil
		}
	}
}
