package quality

import (
	"context"
	"errors"

	"github.com/skosovsky/memy"
)

type Status string

const (
	Pass          Status = "pass"
	Fail          Status = "fail"
	Unknown       Status = "unknown"
	NotApplicable Status = "not_applicable"
)

type Verdict string

const (
	VerdictPass    Verdict = "pass"
	VerdictFail    Verdict = "fail"
	VerdictUnknown Verdict = "unknown"
)

// Evidence contains synthetic public observations, never input text or errors.
type Evidence struct {
	Code    string   `json:"code"`
	Count   int      `json:"count"`
	Aliases []string `json:"aliases,omitempty"`
}
type Check struct {
	ID        string     `json:"id"`
	Mandatory bool       `json:"mandatory"`
	Status    Status     `json:"status"`
	Expected  string     `json:"expected"`
	Observed  string     `json:"observed"`
	Evidence  []Evidence `json:"evidence"`
}
type StageResult struct {
	Status  Status  `json:"status"`
	Checks  []Check `json:"checks"`
	Version string  `json:"version,omitempty"`
}
type Measurement struct {
	Status string  `json:"status"`
	Unit   string  `json:"unit"`
	Value  *uint64 `json:"value,omitempty"`
	Reason string  `json:"reason,omitempty"`
}
type Metrics struct {
	PayloadBytes     Measurement `json:"payload_bytes"`
	ContextJSONBytes Measurement `json:"context_json_bytes"`
	CanonicalBytes   Measurement `json:"canonical_serialized_bytes"`
	ProviderCost     Measurement `json:"provider_abstract_cost"`
	RealProviderCost Measurement `json:"real_provider_cost"`
}
type ScenarioReport struct {
	Versions   PortVersions     `json:"versions"`
	Mode       string           `json:"mode"`
	ID         string           `json:"id"`
	Domain     string           `json:"domain"`
	Version    string           `json:"version"`
	Repeat     int              `json:"repeat"`
	Seed       uint64           `json:"seed"`
	Variation  string           `json:"variation"`
	Candidate  StageResult      `json:"candidate"`
	HostReview StageResult      `json:"host_review"`
	Effective  StageResult      `json:"effective"`
	Canonical  StageResult      `json:"canonical"`
	Rendered   StageResult      `json:"rendered"`
	Execution  StageResult      `json:"execution"`
	Metrics    Metrics          `json:"metrics"`
	Final      Verdict          `json:"final"`
	Diagnostic string           `json:"diagnostic,omitempty"`
	Probes     []ScenarioReport `json:"probes,omitempty"`
}
type Report struct {
	Schema     string           `json:"schema"`
	Manifest   *Corpus          `json:"manifest,omitempty"`
	Versions   PortVersions     `json:"versions"`
	Scenarios  []ScenarioReport `json:"scenarios"`
	Final      Verdict          `json:"final"`
	Diagnostic string           `json:"diagnostic,omitempty"`
}

func KnownMeasurement(unit string, value uint64) Measurement {
	return Measurement{Status: measurementKnown, Unit: unit, Value: &value, Reason: ""}
}
func UnavailableMeasurement(unit, reason string) Measurement {
	return Measurement{Status: measurementUnavailable, Unit: unit, Reason: reason, Value: nil}
}
func stages(s *ScenarioReport) []*StageResult {
	return []*StageResult{&s.Candidate, &s.HostReview, &s.Effective, &s.Canonical, &s.Rendered, &s.Execution}
}

// FinalizeScenario runs after every observation. Descriptive candidate failures
// have Mandatory=false and cannot erase a later mandatory failure or unknown.
func FinalizeScenario(s *ScenarioReport) {
	final := VerdictPass
	if s.Diagnostic != "" {
		final = VerdictUnknown
	}
	for _, stage := range stages(s) {
		final = finalizeStage(stage, final)
	}
	if s.Execution.Status != Pass && s.Execution.Status != Fail {
		final = VerdictUnknown
	}
	s.Final = final
	if defaultMetrics(&s.Metrics) {
		s.Diagnostic = "invalid_measurement"
		s.Final = VerdictUnknown
	}
}
func finalizeStage(stage *StageResult, final Verdict) Verdict {
	if stage.Checks == nil {
		stage.Checks = []Check{}
	}
	if stage.Status == NotApplicable {
		if len(stage.Checks) > 0 {
			return VerdictUnknown
		}
		return final
	}
	if len(stage.Checks) == 0 {
		stage.Status = Unknown
		return VerdictUnknown
	}
	stage.Status = Pass
	seenIDs := map[string]bool{}
	for i := range stage.Checks {
		c := &stage.Checks[i]
		finalizeCheck(c, seenIDs)
		stage.Status = combineStatus(stage.Status, c.Status)
		if c.Mandatory {
			final = combineVerdict(final, c.Status)
		}
	}
	return final
}
func finalizeCheck(c *Check, seenIDs map[string]bool) {
	if c.Evidence == nil {
		c.Evidence = []Evidence{}
	}
	invalid := seenIDs[c.ID]
	seenIDs[c.ID] = true
	for _, e := range c.Evidence {
		if e.Code == "" || e.Count < 0 {
			invalid = true
		}
	}
	if invalid || c.ID == "" || c.Expected == "" || c.Observed == "" || len(c.Evidence) == 0 ||
		(c.Status != Pass && c.Status != Fail && c.Status != Unknown) {
		c.Status = Unknown
	}
}
func combineStatus(current, observed Status) Status {
	if observed == Unknown {
		return Unknown
	}
	if observed == Fail && current != Unknown {
		return Fail
	}
	return current
}
func combineVerdict(current Verdict, observed Status) Verdict {
	if observed == Unknown {
		return VerdictUnknown
	}
	if observed == Fail && current != VerdictUnknown {
		return VerdictFail
	}
	return current
}
func defaultMetrics(m *Metrics) bool {
	invalid := false
	for _, p := range []*Measurement{&m.PayloadBytes, &m.ContextJSONBytes, &m.CanonicalBytes, &m.ProviderCost, &m.RealProviderCost} {
		if p.Status == "" || p.Unit == "" || (p.Status == measurementKnown && (p.Value == nil || p.Reason != "")) ||
			(p.Status == measurementUnavailable && (p.Value != nil || p.Reason == "")) ||
			(p.Status != measurementKnown && p.Status != measurementUnavailable) {
			absent := *p == (Measurement{Status: "", Unit: "", Value: nil, Reason: ""})
			reason := "not_measured"
			if !absent {
				invalid = true
				reason = "invalid_measurement"
			}
			*p = UnavailableMeasurement("unmeasured", reason)
		}
	}
	return invalid
}
func FinalizeReport(r *Report) {
	r.Final = VerdictPass
	if r.Diagnostic != "" || len(r.Scenarios) == 0 {
		r.Final = VerdictUnknown
	}
	for i := range r.Scenarios {
		FinalizeScenario(&r.Scenarios[i])
		v := r.Scenarios[i].Final
		if v == VerdictUnknown {
			r.Final = VerdictUnknown
		} else if v == VerdictFail && r.Final != VerdictUnknown {
			r.Final = VerdictFail
		}
	}
}
func (r Report) ExitCode() int {
	if r.Diagnostic != "" {
		return 2
	}
	knownFailure := false
	for _, s := range r.Scenarios {
		if s.Diagnostic != "" || s.Final == VerdictUnknown {
			return 2
		}
		if s.Final == VerdictFail {
			knownFailure = true
		}
	}
	if knownFailure && r.Final != VerdictUnknown {
		return 1
	}
	switch r.Final {
	case VerdictPass:
		return 0
	case VerdictFail:
		return 1
	case VerdictUnknown:
		return 2
	default:
		return 2
	}
}
func FailureReport(err error) Report {
	return Report{
		Schema:     reportSchema,
		Scenarios:  []ScenarioReport{},
		Final:      VerdictUnknown,
		Diagnostic: ErrorClass(err),
		Manifest:   nil,
		Versions: PortVersions{
			Provider:      "",
			Model:         "",
			HostReview:    "",
			Retention:     "",
			Resolver:      "",
			Consolidation: "",
			Search:        "",
			Projector:     "",
			Packing:       "",
			Grader:        "",
		},
	}
}

// ErrorClass intentionally never incorporates err.Error().
func ErrorClass(err error) string {
	switch {
	case err == nil:
		return portNone
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, memy.ErrInvalid):
		return "invalid"
	case errors.Is(err, memy.ErrUnauthorized):
		return "unauthorized"
	case errors.Is(err, memy.ErrUnavailable):
		return measurementUnavailable
	case errors.Is(err, memy.ErrBudget):
		return "budget"
	case errors.Is(err, memy.ErrSourceUnavailable):
		return "source_unavailable"
	case errors.Is(err, memy.ErrNotFound):
		return "not_found"
	case errors.Is(err, memy.ErrRevoked):
		return "revoked"
	case errors.Is(err, memy.ErrPolicyDenied):
		return "policy_denied"
	case errors.Is(err, memy.ErrUnresolvedConflict):
		return "unresolved_conflict"
	case errors.Is(err, memy.ErrScopeViolation):
		return "scope_violation"
	case errors.Is(err, memy.ErrUnsupported):
		return "unsupported"
	case errors.Is(err, memy.ErrVisibilityPending):
		return "visibility_pending"
	case errors.Is(err, memy.ErrStaleInput):
		return "stale"
	default:
		return "execution_error"
	}
}
