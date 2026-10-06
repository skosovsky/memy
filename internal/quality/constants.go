package quality

const (
	reportSchema           = "memy-quality/v2"
	stageCandidate         = "candidate"
	stageHostReview        = "host_review"
	stageEffective         = "effective"
	stageCanonical         = "canonical"
	stageRendered          = "rendered"
	stageExecution         = "execution"
	measurementKnown       = "known"
	measurementUnavailable = "unavailable"
	fixtureModeScripted    = "scripted"
	portNone               = "none"
	checkpointCompleted    = "completed"
	evaluatorDomain        = "evaluator"
	probeAdapterError      = "adapter_error"
	qualityPurpose         = "quality"
	checkpointMatched      = "matched"
	groupAbstention        = "abstention"
	jsonPackingVersion     = "json-packing/v1"
	groupRetrieval         = "retrieval"
	groupPoisoning         = "poisoning"
	groupForget            = "forget"
	probeFalseEmptyOutage  = "false_empty_outage"
	defaultCorpusSeed      = 20260601
	defaultCandidateLimit  = 100
	defaultContextBytes    = 20000
	defaultProviderBytes   = 10000
	defaultProviderCost    = 5
)
