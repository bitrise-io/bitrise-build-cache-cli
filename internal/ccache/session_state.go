package ccache

import "sync/atomic"

type methodAndOutcome struct {
	Method  callMethod
	Outcome processResultOutcome
}

func (s *sessionState) updateWithResult(result processResult) {
	resultMethodAndOutcome := methodAndOutcome{
		Method:  result.CallStats.method,
		Outcome: result.Outcome,
	}

	switch resultMethodAndOutcome {
	case methodAndOutcome{Method: CALL_METHOD_GET, Outcome: PROCESS_REQUEST_OK}:
		s.incrementHits()
		s.addDownloadBytes(result.CallStats.downloadBytes)

	case methodAndOutcome{Method: CALL_METHOD_GET, Outcome: PROCESS_REQUEST_MISS}:
		s.incrementMisses()

	case methodAndOutcome{Method: CALL_METHOD_PUT, Outcome: PROCESS_REQUEST_OK}:
		s.addUploadBytes(result.CallStats.uploadBytes)

	default:
		// We could track other outcomes as well, but for now we focus on the main ones for stats.
	}
}

type sessionState struct {
	downloadBytes atomic.Int64
	uploadBytes   atomic.Int64
	hits          atomic.Int64
	misses        atomic.Int64
}

func newSessionState() *sessionState {
	return &sessionState{}
}

func (s *sessionState) addDownloadBytes(n int64) {
	s.downloadBytes.Add(n)
}

func (s *sessionState) addUploadBytes(n int64) {
	s.uploadBytes.Add(n)
}

func (s *sessionState) incrementMisses() {
	s.misses.Add(1)
}

func (s *sessionState) incrementHits() {
	s.hits.Add(1)
}
