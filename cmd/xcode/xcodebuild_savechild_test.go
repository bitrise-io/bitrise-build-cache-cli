//go:build unit

package xcode

import (
	"encoding/json"
	"os"
	"testing"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/common/childstats"
)

func newSaveChildTestLogger() *utilsMocks.Logger {
	l := &utilsMocks.Logger{}
	l.On("Debugf", mock.Anything, mock.Anything).Return()
	l.On("Infof", mock.Anything, mock.Anything).Return()
	l.On("Errorf", mock.Anything, mock.Anything).Return()
	l.On("Warnf", mock.Anything, mock.Anything).Return()
	l.On("TInfof", mock.Anything, mock.Anything, mock.Anything).Return()
	l.On("TInfof", mock.Anything, mock.Anything).Return()
	l.On("TInfof", mock.Anything).Return()

	return l
}

type stubInvocationSaver struct {
	err error
}

func (s *stubInvocationSaver) PutInvocation(_ analytics.Invocation) error {
	return s.err
}

// saveInvocation records this xcode invocation in the parent's child-stats
// ledger when run under a parent wrapper. The parent (e.g. react-native) reads
// that ledger to report the parent→child lineage inline — there is no longer a
// separate relation API call.
func Test_saveInvocation_WritesChildStatsLedger(t *testing.T) {
	t.Run("writes ledger entry when parent ID is set", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("BITRISE_INVOCATION_ID", "parent-inv-id")

		runner := &XcodebuildRunner{
			Config:        xcelerate.Config{},
			InvocationID:  "child-inv-id",
			Logger:        newSaveChildTestLogger(),
			invocationAPI: &stubInvocationSaver{},
		}

		runner.saveInvocation(analytics.Invocation{InvocationID: "child-inv-id"}, 3, 4)

		data, err := os.ReadFile(childstats.LedgerPath("parent-inv-id", "child-inv-id"))
		require.NoError(t, err)

		var entry struct {
			ChildInvocationID  string `json:"child_invocation_id"`
			ParentInvocationID string `json:"parent_invocation_id"`
			BuildTool          string `json:"build_tool"`
		}
		require.NoError(t, json.Unmarshal(data, &entry))
		assert.Equal(t, "child-inv-id", entry.ChildInvocationID)
		assert.Equal(t, "parent-inv-id", entry.ParentInvocationID)
		assert.Equal(t, "xcode", entry.BuildTool)
	})

	t.Run("does not write ledger when no parent ID", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("BITRISE_INVOCATION_ID", "")

		runner := &XcodebuildRunner{
			Config:        xcelerate.Config{},
			InvocationID:  "child-inv-id",
			Logger:        newSaveChildTestLogger(),
			invocationAPI: &stubInvocationSaver{},
		}

		runner.saveInvocation(analytics.Invocation{InvocationID: "child-inv-id"}, 0, 0)

		// LedgerDir("") resolves to the invocations root; nothing should exist.
		_, err := os.Stat(childstats.LedgerDir(""))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("does not write ledger when PutInvocation fails", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("BITRISE_INVOCATION_ID", "parent-inv-id")

		runner := &XcodebuildRunner{
			Config:        xcelerate.Config{},
			InvocationID:  "child-inv-id",
			Logger:        newSaveChildTestLogger(),
			invocationAPI: &stubInvocationSaver{err: assert.AnError},
		}

		runner.saveInvocation(analytics.Invocation{InvocationID: "child-inv-id"}, 0, 0)

		_, err := os.Stat(childstats.LedgerPath("parent-inv-id", "child-inv-id"))
		assert.True(t, os.IsNotExist(err))
	})
}
