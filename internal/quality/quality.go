// Package quality is a synthetic consumer-owned offline harness. It is not a
// library policy, an executable instruction dispatcher, or an LLM benchmark.
package quality

import (
	"context"
	"encoding/binary"
	"hash/fnv"
	"slices"

	"github.com/skosovsky/memy"
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
	// Encoding signed repeat preserves its eight-byte representation without
	// a potentially overflowing unsigned conversion. The runner uses nonnegative repeats.
	_, _ = binary.Encode(b[:], binary.LittleEndian, int64(repeat))
	_, _ = h.Write(b[:])
	return h.Sum64()
}
func Run(ctx context.Context, c Corpus) (Report, error) {
	if err := validateCorpus(c); err != nil {
		return FailureReport(err), err
	}
	r := Report{
		Schema:     reportSchema,
		Manifest:   &c,
		Versions:   c.Versions,
		Scenarios:  []ScenarioReport{},
		Final:      "",
		Diagnostic: "",
	}
	registry := map[string]CasePlan{}
	for _, p := range plans() {
		registry[p.ID] = p
	}
	var first error
	for _, spec := range c.Scenarios {
		p := registry[spec.ID]
		for repeat := range c.Repeats {
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
				s.Execution.Checks = append(
					s.Execution.Checks,
					Check{
						ID:        "runner_execution",
						Mandatory: true,
						Status:    Unknown,
						Expected:  checkpointCompleted,
						Observed:  string(Unknown),
						Evidence:  []Evidence{{Code: "execution_error", Count: 1, Aliases: nil}},
					},
				)
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
	names := []string{stageCandidate, stageHostReview, stageEffective, stageCanonical, stageRendered, stageExecution}
	missing := s.Versions != p.Versions || s.Mode == ""
	for i, stage := range stages(s) {
		if stage.Status == NotApplicable && !containsStage(p.OptionalStages, names[i]) {
			missing = true
		}
	}
	for _, required := range p.Required {
		if !hasRequiredCheck(s, names, required) {
			missing = true
		}
	}
	if len(p.Required) == 0 {
		missing = true
	}
	if missing {
		s.Execution.Checks = append(
			s.Execution.Checks,
			Check{
				ID:        "missing_checkpoint",
				Mandatory: true,
				Status:    Unknown,
				Expected:  checkpointCompleted,
				Observed:  string(Unknown),
				Evidence:  []Evidence{{Code: "missing_checkpoint", Count: 1, Aliases: nil}},
			},
		)
	}
}
func containsStage(stages []string, stage string) bool {
	return slices.Contains(stages, stage)
}

func hasRequiredCheck(s *ScenarioReport, names []string, required RequiredCheck) bool {
	for i, stage := range stages(s) {
		if names[i] != required.Stage {
			continue
		}
		for _, check := range stage.Checks {
			if check.ID == required.ID && check.Mandatory {
				return true
			}
		}
	}
	return false
}
