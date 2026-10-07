package memy

// Score separates an absent value from an observed or computed numeric zero.
// An absent score must have Value=0. Presence never implies relevance or trust.
type Score struct {
	Present bool    `json:"present"`
	Value   float64 `json:"value"`
}

// ScoreOf records a present value. Ports validate finiteness before using it.
func ScoreOf(value float64) Score { return Score{Present: true, Value: value} }

// Validate rejects nonfinite numbers and nonzero values marked absent.
func (s Score) Validate() error {
	if !finiteScore(s.Value) || (!s.Present && s.Value != 0) {
		return ErrInvalid
	}
	return nil
}

// Compare orders present scores descending, followed by absent scores.
func (s Score) Compare(other Score) int {
	if s.Present != other.Present {
		if s.Present {
			return -1
		}
		return 1
	}
	if s.Value > other.Value {
		return -1
	}
	if s.Value < other.Value {
		return 1
	}
	return 0
}

// RetrievalEvidence is host ranking evidence bound by its containing projection.
// Signals preserve native evidence; Score is the separate ranking result.
type RetrievalEvidence struct {
	Score       Score          `json:"score"`
	Explanation string         `json:"explanation"`
	Signals     []SearchSignal `json:"signals"`
}
