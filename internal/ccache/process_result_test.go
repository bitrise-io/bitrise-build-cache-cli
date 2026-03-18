//go:build unit

package ccache

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_processResult_OutcomeString(t *testing.T) {
	tests := []struct {
		name     string
		result   processResult
		expected string
	}{
		{
			name:     "OK outcome",
			result:   processResult{Outcome: PROCESS_REQUEST_OK},
			expected: "OK",
		},
		{
			name:     "MISS outcome",
			result:   processResult{Outcome: PROCESS_REQUEST_MISS},
			expected: "MISS",
		},
		{
			name:     "ERROR outcome",
			result:   processResult{Outcome: PROCESS_REQUEST_ERROR, Err: errors.New("something broke")},
			expected: "ERROR: something broke",
		},
		{
			name:     "PUSH_DISABLED outcome",
			result:   processResult{Outcome: PROCESS_REQUEST_PUSH_DISABLED},
			expected: "PUSH_DISABLED",
		},
		{
			name:     "unknown outcome",
			result:   processResult{Outcome: processResultOutcome(99)},
			expected: fmt.Sprintf("UNKNOWN(%d)", 99),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.result.OutcomeString())
		})
	}
}

func Test_processResult_Prefix(t *testing.T) {
	result := processResult{
		CallStats: callStats{
			method: CALL_METHOD_GET,
			key:    "abcdef",
		},
	}
	assert.Equal(t, "[Get - abcdef]", result.Prefix())
}

func Test_processResult_Log(t *testing.T) {
	result := processResult{
		Outcome: PROCESS_REQUEST_OK,
		CallStats: callStats{
			method: CALL_METHOD_PUT,
			key:    "somekey",
		},
	}
	assert.Equal(t, "[Set - somekey] OK", result.Log())
}
