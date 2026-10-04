package quality_test

import (
	"context"
	"errors"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/internal/quality"
)

func TestOfflineQualityProtocol(t *testing.T) {
	// Arrange.
	corpus, operationErr := quality.Load("../../testdata/consolidation-v1.json")
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	// Act.
	report, operationErr := quality.Run(context.Background(), corpus)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	// Assert: qualities are hard gates, measured cost/volume cannot offset harm.
	if report.AutoApply || len(report.Trials) != 4 {
		t.Fatalf("invalid report: %+v", report)
	}
	for i, trial := range report.Trials {
		assertTrial(t, trial, i < 3)
	}
	if report.Trials[1].CandidateBytes >= report.Trials[0].CandidateBytes || report.Trials[2].EffectiveRecords != 2 {
		t.Fatal("deterministic consolidation did not reduce context")
	}
	assertSemanticRejection(t, report.Trials[3])
}

func assertTrial(t *testing.T, trial quality.Trial, deterministic bool) {
	t.Helper()
	if trial.PrivacyLeaks != 0 || trial.OriginalsRetained != 3 || trial.RecallLatencyNS <= 0 {
		t.Fatalf("invalid trial: %+v", trial)
	}
	if deterministic &&
		(!trial.Accepted || trial.FalseMemory != 0 || trial.LostNegations != 0 || trial.LostIntervals != 0 || trial.ExpectedMatches != 2) {
		t.Fatalf("deterministic constraint loss: %+v", trial)
	}
}

func assertSemanticRejection(t *testing.T, negative quality.Trial) {
	t.Helper()
	if negative.Accepted || negative.FalseMemory != 1 || negative.LostNegations != 1 || negative.LostIntervals != 1 ||
		negative.EffectiveRecords != 3 ||
		negative.ProviderCostUnits != 1 {
		t.Fatalf("semantic negative result hidden: %+v", negative)
	}
}

func TestMalformedCorpusCannotProduceQualityReport(t *testing.T) {
	for _, mutation := range []string{"empty", "missing_fact", "duplicate_id", "invalid_interval", "changed_negative"} {
		t.Run(mutation, func(t *testing.T) {
			// Arrange: each malformed input breaks an actual experiment assumption.
			corpus, loadErr := quality.Load("../../testdata/consolidation-v1.json")
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			corruptCorpus(&corpus, mutation)
			// Act.
			report, runErr := quality.Run(context.Background(), corpus)
			// Assert: no panic, partial trials or misleading score is published.
			if !errors.Is(runErr, memy.ErrInvalid) || len(report.Trials) != 0 {
				t.Fatalf("report=%+v err=%v", report, runErr)
			}
		})
	}
}

func corruptCorpus(corpus *quality.Corpus, mutation string) {
	switch mutation {
	case "empty":
		corpus.Records = nil
	case "missing_fact":
		corpus.Expected = corpus.Expected[:1]
	case "duplicate_id":
		corpus.Records[1].ID = corpus.Records[0].ID
	case "invalid_interval":
		corpus.Records[0].To = corpus.Records[0].From
	case "changed_negative":
		corpus.Records[1].Value = "drinks tea in the evening"
	}
}
