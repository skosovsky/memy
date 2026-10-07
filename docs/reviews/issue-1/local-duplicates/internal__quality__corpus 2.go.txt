package quality

import "github.com/skosovsky/memy"

// The scripted protocol requires a duplicate, a negation and an isolated B
// record. Validate these assumptions before any trial allocates a store.
func validateCorpus(corpus Corpus) error {
	if corpus.Version == "" || corpus.Provider == "" || corpus.Resolver == "" || corpus.Projection == "" ||
		corpus.AutoApply || len(corpus.Records) == 0 || len(corpus.Expected) == 0 || corpus.BadSemantic == "" {
		return memy.ErrInvalid
	}
	ids := make(map[string]bool)
	facts := make(map[string]bool)
	duplicates, negative, foreign := false, false, false
	interval := memy.Interval{Known: corpus.Records[0].Known, From: corpus.Records[0].From, To: corpus.Records[0].To}
	if !interval.Known || interval.Validate() != nil {
		return memy.ErrInvalid
	}
	for _, item := range corpus.Records {
		if item.ID == "" || ids[item.ID] || item.Key != "drink" || item.Value == "" ||
			(memy.Interval{Known: item.Known, From: item.From, To: item.To}) != interval {
			return memy.ErrInvalid
		}
		ids[item.ID] = true
		switch item.Tenant {
		case "A":
			duplicates = duplicates || facts[item.Value]
			facts[item.Value] = true
			negative = negative || item.Value == "never drinks tea in the evening"
		case "B":
			foreign = foreign || item.ID == "tea-other-scope"
		default:
			return memy.ErrInvalid
		}
	}
	if !duplicates || !negative || !foreign || facts[corpus.BadSemantic] {
		return memy.ErrInvalid
	}
	return validateExpected(corpus.Expected, facts)
}

func validateExpected(expected []string, facts map[string]bool) error {
	if len(expected) != len(facts) {
		return memy.ErrInvalid
	}
	seen := make(map[string]bool, len(expected))
	for _, value := range expected {
		if !facts[value] || seen[value] {
			return memy.ErrInvalid
		}
		seen[value] = true
	}
	return nil
}
