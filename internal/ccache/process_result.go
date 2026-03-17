package ccache

import "fmt"

type processResultOutcome int32

const (
	PROCESS_REQUEST_OK            processResultOutcome = 0
	PROCESS_REQUEST_MISS          processResultOutcome = 1
	PROCESS_REQUEST_ERROR         processResultOutcome = 3
	PROCESS_REQUEST_PUSH_DISABLED processResultOutcome = 4
)

type processResult struct {
	Err       error
	Data      []byte
	CallStats callStats
	Outcome   processResultOutcome
}

func (result processResult) OutcomeString() string {
	switch result.Outcome {
	case PROCESS_REQUEST_OK:
		return "OK"
	case PROCESS_REQUEST_MISS:
		return "MISS"
	case PROCESS_REQUEST_ERROR:
		return fmt.Sprintf("ERROR: %v", result.Err)
	case PROCESS_REQUEST_PUSH_DISABLED:
		return "PUSH_DISABLED"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", result.Outcome)
	}
}

func (result processResult) Prefix() string {
	return fmt.Sprintf("[%s - %s]", result.CallStats.method, result.CallStats.key)
}

func (result processResult) Log() string {
	return fmt.Sprintf("%s %s", result.Prefix(), result.OutcomeString())
}
