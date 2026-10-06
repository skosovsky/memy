// Package quality is a synthetic consumer-owned offline harness. It is not a
// library policy, an executable instruction dispatcher, or an LLM benchmark.
package quality

import (
	"context"
	"encoding/binary"
	"github.com/skosovsky/memy"
	"hash/fnv"
)

type CaseRun struct {
	Repeat int
	Seed   uint64
}
type RequiredCheck struct{ Stage, ID string }
type CasePlan struct {
	ID, Domain, Version string
	Groups              []string
	Required            []RequiredCheck
	OptionalStages      []string
	Versions            PortVersions
	Run                 func(context.Context, Corpus, CaseRun) (ScenarioReport, error)
}

func plans() []CasePlan {
	return append(append(preferencePlans(), procedurePlans()...), evaluatorPlans()...)
}
func DerivedSeed(seed uint64, id string, repeat int) uint64 {
	h := fnv.New64a()
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], seed)
	_, _ = h.Write(b[:])
	_, _ = h.Write([]byte(id))
	binary.LittleEndian.PutUint64(b[:], uint64(repeat))
	_, _ = h.Write(b[:])
	return h.Sum64()
}
func Run(ctx context.Context, c Corpus) (Report, error) {
	if err := validateCorpus(c); err != nil {
		return FailureReport(err), err
	}
	r := Report{Schema: "memy-quality/v2", Manifest: &c, Versions: c.Versions, Scenarios: []ScenarioReport{}}
	registry := map[string]CasePlan{}
	for _, p := range plans() {
		registry[p.ID] = p
	}
	var first error
	for _, spec := range c.Scenarios {
		p := registry[spec.ID]
		for repeat := 0; repeat < c.Repeats; repeat++ {
			run := CaseRun{repeat, DerivedSeed(c.Seed, p.ID, repeat)}
			var s ScenarioReport
			var err error
			if ctx.Err() != nil {
				err = ctx.Err()
			} else {
				s, err = p.Run(ctx, c, run)
			}
			s.ID, s.Domain, s.Version, s.Repeat, s.Seed = p.ID, p.Domain, p.Version, repeat, run.Seed
			if err != nil {
				if first == nil {
					first = err
				}
				s.Diagnostic = ErrorClass(err)
				s.Execution.Checks = append(s.Execution.Checks, Check{ID: "runner_execution", Mandatory: true, Status: Unknown, Expected: "completed", Observed: "unknown", Evidence: []Evidence{{Code: "execution_error", Count: 1}}})
			}
			enforcePlan(&s, p)
			FinalizeScenario(&s)
			r.Scenarios = append(r.Scenarios, s)
		}
	}
	FinalizeReport(&r)
	if r.Final == VerdictUnknown && first == nil {
		first = memy.ErrInvalid
	}
	return r, first
}

func enforcePlan(s *ScenarioReport, p CasePlan) {
	names := []string{"candidate", "host_review", "effective", "canonical", "rendered", "execution"}
	missing := s.Versions != p.Versions || s.Mode == ""
	for i, stage := range stages(s) {
		if stage.Status == NotApplicable && !containsStage(p.OptionalStages, names[i]) {
			missing = true
		}
	}
	for _, required := range p.Required {
		found := false
		for i, stage := range stages(s) {
			if names[i] != required.Stage {
				continue
			}
			for _, check := range stage.Checks {
				if check.ID == required.ID && check.Mandatory {
					found = true
				}
			}
		}
		if !found {
			missing = true
		}
	}
	if len(p.Required) == 0 {
		missing = true
	}
	if missing {
		s.Execution.Checks = append(s.Execution.Checks, Check{ID: "missing_checkpoint", Mandatory: true, Status: Unknown, Expected: "completed", Observed: "unknown", Evidence: []Evidence{{Code: "missing_checkpoint", Count: 1}}})
	}
}
func containsStage(stages []string, stage string) bool {
	for _, v := range stages {
		if v == stage {
			return true
		}
	}
	return false
}
