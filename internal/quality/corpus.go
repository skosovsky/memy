package quality

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"

	"github.com/skosovsky/memy"
)

const CorpusVersion = "memy-quality-corpus/v2"

type PortVersions struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	HostReview    string `json:"host_review"`
	Retention     string `json:"retention"`
	Resolver      string `json:"resolver"`
	Consolidation string `json:"consolidation"`
	Search        string `json:"search"`
	Projector     string `json:"projector"`
	Packing       string `json:"packing"`
	Grader        string `json:"grader"`
}
type Budgets struct {
	MaxCandidates int    `json:"max_candidates"`
	RecallLimit   int    `json:"recall_limit"`
	ContextBytes  uint64 `json:"context_bytes"`
	InputBytes    int    `json:"input_bytes"`
	OutputBytes   int    `json:"output_bytes"`
	CostUnits     int64  `json:"cost_units"`
}
type ScenarioSpec struct {
	Versions PortVersions `json:"versions"`
	ID       string       `json:"id"`
	Version  string       `json:"version"`
}
type Corpus struct {
	Version   string         `json:"version"`
	Seed      uint64         `json:"seed"`
	Repeats   int            `json:"repeats"`
	Versions  PortVersions   `json:"versions"`
	Budgets   Budgets        `json:"budgets"`
	Scenarios []ScenarioSpec `json:"scenarios"`
}

func requiredGroups() []string {
	return []string{
		"sessions",
		"temporal",
		groupRetrieval,
		groupAbstention,
		groupPoisoning,
		groupForget,
		"consolidation",
		evaluatorDomain,
	}
}

func DefaultVersions() PortVersions {
	return PortVersions{
		Provider:      "scripted-provider/v2",
		Model:         fixtureModeScripted,
		HostReview:    "fixture-host-review/v2",
		Retention:     "fixture-retention/v2",
		Resolver:      "fixture-resolver/v2",
		Consolidation: "fixture-consolidation/v2",
		Search:        "reference-rrf/v2",
		Projector:     "fixture-projector/v2",
		Packing:       jsonPackingVersion,
		Grader:        portNone,
	}
}
func DefaultCorpus() Corpus {
	c := Corpus{
		Version:  CorpusVersion,
		Seed:     defaultCorpusSeed,
		Repeats:  2,
		Versions: DefaultVersions(),
		Budgets: Budgets{
			MaxCandidates: defaultCandidateLimit,
			RecallLimit:   defaultCandidateLimit,
			ContextBytes:  defaultContextBytes,
			InputBytes:    defaultProviderBytes,
			OutputBytes:   defaultProviderBytes,
			CostUnits:     defaultProviderCost,
		},
		Scenarios: nil}
	for _, p := range plans() {
		c.Scenarios = append(c.Scenarios, ScenarioSpec{ID: p.ID, Version: p.Version, Versions: p.Versions})
	}
	return c
}
func Load(path string) (Corpus, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Corpus{}, err
	}
	var c Corpus
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return Corpus{}, memy.ErrInvalid
	}
	var trailing any
	if err = d.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Corpus{}, memy.ErrInvalid
	}
	if err = validateCorpus(c); err != nil {
		return Corpus{}, err
	}
	return c, nil
}
func validateCorpus(c Corpus) error {
	if c.Version != CorpusVersion || c.Seed == 0 || c.Repeats < 1 || c.Repeats > 20 ||
		c.Versions != DefaultVersions() || len(c.Scenarios) == 0 || len(c.Scenarios) > 100 {
		return memy.ErrInvalid
	}
	if err := validateBudgets(c.Budgets); err != nil {
		return err
	}
	registered := map[string]CasePlan{}
	for _, p := range plans() {
		if p.ID == "" || p.Version == "" || p.Run == nil || len(p.Required) == 0 || !validVersions(p.Versions) ||
			registered[p.ID].ID != "" {
			return memy.ErrInvalid
		}
		registered[p.ID] = p
	}
	seen := map[string]bool{}
	groups := map[string]bool{}
	for _, s := range c.Scenarios {
		p, ok := registered[s.ID]
		if !ok || seen[s.ID] || s.Version != p.Version || s.Versions != p.Versions {
			return memy.ErrInvalid
		}
		seen[s.ID] = true
		for _, g := range p.Groups {
			if !slices.Contains(requiredGroups(), g) {
				return memy.ErrInvalid
			}
			groups[g] = true
		}
	}
	for _, g := range requiredGroups() {
		if !groups[g] {
			return memy.ErrInvalid
		}
	}
	return nil
}

func validVersions(v PortVersions) bool {
	return !slices.Contains(
		[]string{
			v.Provider,
			v.Model,
			v.HostReview,
			v.Retention,
			v.Resolver,
			v.Consolidation,
			v.Search,
			v.Projector,
			v.Packing,
			v.Grader,
		},
		"",
	)
}

func validateBudgets(b Budgets) error {
	if b.MaxCandidates < 1 ||
		b.MaxCandidates > memy.MaxSearchCandidates ||
		b.RecallLimit < 1 ||
		b.RecallLimit > memy.MaxSearchCandidates ||
		b.ContextBytes < 1 ||
		b.ContextBytes > 64<<20 ||
		b.InputBytes < 1 ||
		b.InputBytes > 64<<20 ||
		b.OutputBytes < 1 ||
		b.OutputBytes > 64<<20 ||
		b.CostUnits < 1 ||
		b.CostUnits > 1000000 {
		return memy.ErrInvalid
	}
	return nil
}
