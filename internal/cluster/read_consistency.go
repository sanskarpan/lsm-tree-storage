package cluster

import "strings"

// ReadConsistency controls whether a read must observe a linearizable leader
// view or may be served from a follower's locally applied state.
type ReadConsistency string

const (
	ReadConsistencyLinearizable ReadConsistency = "linearizable"
	ReadConsistencyEventual     ReadConsistency = "eventual"
)

func normalizeReadConsistency(mode ReadConsistency) ReadConsistency {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case "", string(ReadConsistencyLinearizable):
		return ReadConsistencyLinearizable
	case string(ReadConsistencyEventual):
		return ReadConsistencyEventual
	default:
		return ReadConsistencyLinearizable
	}
}
