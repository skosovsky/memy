//go:build integration || e2e

package compatibility_test

import (
	"context"

	"github.com/skosovsky/ragy/retrieval"
	ragtool "github.com/skosovsky/toolsy/toolkits/rag"

	"github.com/skosovsky/memy"
)

type meta struct {
	Scope    memy.Scope
	Revision memy.Version
}
type adapter struct{ docs []retrieval.Document[meta] }

func (a adapter) Capabilities() memy.SearchCapabilities {
	return memy.SearchCapabilities{Scoped: true, BoundedCandidates: true}
}

func (a adapter) Search(
	ctx context.Context,
	scope memy.Scope,
	_ string,
	opts memy.SearchOptions,
) (memy.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return memy.SearchResult{}, err
	}
	if opts.MaxCandidates < 1 || opts.MaxCandidates > memy.MaxSearchCandidates {
		return memy.SearchResult{}, memy.ErrInvalid
	}
	if opts.Minimum != nil {
		return memy.SearchResult{}, memy.ErrUnsupported
	}
	out := memy.SearchResult{Coverage: []memy.Coverage{{Backend: "rag", Status: "ready"}}}
	for i, d := range a.docs {
		if d.Meta.Scope != scope {
			continue
		}
		rank := d.Rank
		if rank == 0 {
			rank = i + 1
		}
		if err := retrieval.ValidateDocument(d); err != nil {
			return memy.SearchResult{}, err
		}
		native := memy.Score{}
		if d.ScoreState == retrieval.ScorePresent {
			native = memy.ScoreOf(d.Score)
		}
		// A normalized score is host ranking evidence, never an observed native score.
		ranking := memy.ScoreOf(1 / float64(rank))
		out.Candidates = append(
			out.Candidates,
			memy.Candidate{
				RecordID: d.ID,
				Revision: d.Meta.Revision,
				Score:    ranking,
				Signals:  []memy.SearchSignal{{Backend: "rag", Rank: rank, Score: native}},
			},
		)
	}
	if len(out.Candidates) > opts.MaxCandidates {
		out.Candidates = out.Candidates[:opts.MaxCandidates]
		out.CandidatesTruncated = true
	}
	return out, nil
}

var _ memy.Search[string] = adapter{}

type toolAdapter struct{}

func (toolAdapter) Retrieve(ctx context.Context, _ string) ([]ragtool.Document, error) {
	return nil, ctx.Err()
}

var _ ragtool.DocumentRetriever = toolAdapter{}
