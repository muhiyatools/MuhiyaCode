package orchestrator

import "github.com/muhiya/muhiyacode/internal/contract"

type pressureSnapshot struct {
	Tokens    int
	Estimated bool
	Ratio     float64
}

type mainUsageObservation struct {
	model         string
	usage         contract.Usage
	changeReasons []string
	messageCount  int
	durationMS    *int64
	requestBuild  RequestBuild
	prefixHash    string
	// A model is warm only for the history revision it actually saw.
	rewriteVersion int
}

type usageRecordInput struct {
	model        string
	stream       contract.UsageStream
	pin          string
	purpose      contract.RequestPurpose
	cacheEpoch   uint64
	usage        contract.Usage
	reasons      []string
	attribution  contract.CacheAttribution
	durationMS   *int64
	requestBuild *RequestBuild
	prefixHash   string
}

type cacheMissContext struct {
	previous             *contract.UsageRecord
	usage                contract.Usage
	newTail              int
	previousMessageCount int
	currentMessageCount  int
}
