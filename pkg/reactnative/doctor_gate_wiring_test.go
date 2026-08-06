//go:build unit

package reactnative

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	doctorpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/doctor"
)

func noOpExecFn(_ []string, _ string, _ ...string) (int, error) { return 0, nil }

func runnerWithDoctor(t *testing.T, params RunnerParams) (*Runner, *buildHealthReporterMock) {
	t.Helper()

	mock := &buildHealthReporterMock{}
	r := newTestRunner(params)
	r.doctor = mock

	return r, mock
}

func TestRunner_DoctorRunsAroundTheWrappedCommand(t *testing.T) {
	activateRNHome(t)

	r, doctor := runnerWithDoctor(t, RunnerParams{ExecFn: noOpExecFn})
	r.postRun = &postRunRunnerMock{
		runFunc: func(context.Context, string, []string, time.Duration, error) buildOutcome {
			return buildOutcome{ChildInvocations: 3}
		},
	}

	_, err := r.Run(context.Background(), []string{"yarn", "ios"}, "inv-1", []string{})

	require.NoError(t, err)
	assert.Len(t, doctor.CheckAtStartCalls(), 1)
	require.Len(t, doctor.ReportAtEndCalls(), 1)
	assert.Equal(t, 3, doctor.ReportAtEndCalls()[0].Outcome.ChildInvocations)
	assert.Empty(t, doctor.OnInvocationSaveFailureCalls(), "the analytics PUT succeeded")
}

func TestRunner_DoctorProbesWhenTheInvocationSaveFails(t *testing.T) {
	activateRNHome(t)

	r, doctor := runnerWithDoctor(t, RunnerParams{ExecFn: noOpExecFn})
	r.postRun = &postRunRunnerMock{
		runFunc: func(context.Context, string, []string, time.Duration, error) buildOutcome {
			return buildOutcome{InvocationSaveFailed: true}
		},
	}

	_, err := r.Run(context.Background(), []string{"yarn", "ios"}, "inv-1", []string{})

	require.NoError(t, err)
	assert.Len(t, doctor.OnInvocationSaveFailureCalls(), 1)
	assert.Len(t, doctor.ReportAtEndCalls(), 1)
}

func TestRunner_NotActivatedSkipsTheDoctor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	r, doctor := runnerWithDoctor(t, RunnerParams{ExecFn: noOpExecFn})

	_, err := r.Run(context.Background(), []string{"yarn", "ios"}, "inv-1", []string{})

	require.NoError(t, err)
	assert.Empty(t, doctor.CheckAtStartCalls(), "nothing is activated, so there is nothing to report on")
}

func TestRunner_NoDoctorFlagIsConsumedNotForwarded(t *testing.T) {
	activateRNHome(t)

	var capturedName string
	var capturedArgs []string

	r, doctor := runnerWithDoctor(t, RunnerParams{
		ExecFn: func(_ []string, name string, args ...string) (int, error) {
			capturedName, capturedArgs = name, args

			return 0, nil
		},
	})

	_, err := r.Run(context.Background(), []string{doctorpkg.NoDoctorFlag, "--", "yarn", "ios"}, "inv-1", []string{})

	require.NoError(t, err)
	assert.Equal(t, "yarn", capturedName)
	assert.Equal(t, []string{"ios"}, capturedArgs)
	assert.Empty(t, doctor.CheckAtStartCalls())
	assert.Empty(t, doctor.ReportAtEndCalls())
}

// The flag belongs to the wrapper, so past the "--" separator it is the child's.
func TestRunner_NoDoctorAfterTheSeparatorGoesToTheChild(t *testing.T) {
	activateRNHome(t)

	var capturedArgs []string

	r, doctor := runnerWithDoctor(t, RunnerParams{
		ExecFn: func(_ []string, _ string, args ...string) (int, error) {
			capturedArgs = args

			return 0, nil
		},
	})

	_, err := r.Run(context.Background(), []string{"--", "yarn", doctorpkg.NoDoctorFlag}, "inv-1", []string{})

	require.NoError(t, err)
	assert.Equal(t, []string{doctorpkg.NoDoctorFlag}, capturedArgs)
	assert.Len(t, doctor.CheckAtStartCalls(), 1)
}

func TestNewRunner_DoctorOptOuts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	assert.NotNil(t, NewRunner(RunnerParams{ExecFn: noOpExecFn}).doctor)
	assert.Nil(t, NewRunner(RunnerParams{ExecFn: noOpExecFn, SkipDoctor: true}).doctor)

	t.Setenv(doctorpkg.EnvSkipDoctor, "1")
	assert.Nil(t, NewRunner(RunnerParams{ExecFn: noOpExecFn}).doctor)
}
