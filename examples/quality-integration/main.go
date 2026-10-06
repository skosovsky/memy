// Command quality-integration replaces fixture ports with consumer-owned typed
// offline ports and executes the same exact-revision and full-context checkpoints.
package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/internal/quality"
)

type observedProcedureExtractor struct {
	offline memy.Extractor[quality.ProcedureInput, quality.ProcedureObservation, quality.DocumentRef]
}

func (p observedProcedureExtractor) Extract(ctx context.Context, in quality.ProcedureInput) ([]memy.Suggestion[quality.ProcedureObservation, quality.DocumentRef], error) {
	// A consumer can replace this script with its own provider SDK. Typed input,
	// source verification, acceptance and checkpoint expectations stay unchanged.
	if in.Source.Reference.Document != 17 {
		return nil, memy.ErrInvalid
	}
	return p.offline.Extract(ctx, in)
}

type boundedProcedureSearch struct {
	offline memy.Search[quality.ProcedureQuery]
}

func (p boundedProcedureSearch) Capabilities() memy.SearchCapabilities {
	return p.offline.Capabilities()
}
func (p boundedProcedureSearch) Search(ctx context.Context, scope memy.Scope, q quality.ProcedureQuery, o memy.SearchOptions) (memy.SearchResult, error) {
	return p.offline.Search(ctx, scope, q, o)
}
func main() {
	ctx := context.Background()
	defaults := quality.DefaultProcedurePorts()
	ports := defaults
	ports.Mode = "scripted"
	ports.ExtractorVersion = "integration-extractor/v1"
	ports.SearchVersion = "integration-bounded-rrf/v1"
	ports.GraderVersion = "integration-exact-grader/v1"
	ports.Extractor = observedProcedureExtractor{offline: defaults.Extractor}
	ports.Search = func(scope memy.Scope, candidates []memy.Candidate) memy.Search[quality.ProcedureQuery] {
		return boundedProcedureSearch{offline: defaults.Search(scope, candidates)}
	}
	ports.Grade = func(ctx context.Context, p []memy.Projection[quality.ProcedureObservation, quality.DocumentRef]) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		return len(p) == 1 && p[0].Output.Service == "checkout" && p[0].Trust == "data", nil
	}
	corpus := quality.DefaultCorpus()
	scenario, err := quality.RunProcedureCase(ctx, "procedure-retrieval", corpus, quality.CaseRun{Seed: quality.DerivedSeed(corpus.Seed, "procedure-retrieval", 0)}, ports)
	report := quality.Report{Schema: "memy-quality/v2", Versions: scenario.Versions, Scenarios: []quality.ScenarioReport{scenario}}
	if err != nil {
		report.Diagnostic = quality.ErrorClass(err)
	}
	quality.FinalizeReport(&report)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if encoder.Encode(report) != nil {
		os.Stderr.WriteString("quality_report_write_failed\n")
		os.Exit(2)
	}
	os.Exit(report.ExitCode())
}

// Compile-time assertions keep this example on the consumer-owned typed seam.
var _ memy.Extractor[quality.ProcedureInput, quality.ProcedureObservation, quality.DocumentRef] = observedProcedureExtractor{}
var _ memy.Search[quality.ProcedureQuery] = boundedProcedureSearch{}
