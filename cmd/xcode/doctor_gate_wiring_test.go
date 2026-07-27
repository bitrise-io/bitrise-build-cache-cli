//go:build unit

package xcode

import (
	"context"
	"io"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs"
	xcodeargsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs/mocks"
)

type stubXcodeRunner struct{}

func (stubXcodeRunner) Run(context.Context, []string) xcodeargs.RunStats {
	return xcodeargs.RunStats{Success: true}
}

func TestSaveInvocationAndRelation_ProbesDoctorOnFailure(t *testing.T) {
	t.Setenv("BITRISE_INVOCATION_ID", "")

	for _, tc := range []struct {
		name      string
		saveErr   error
		wantProbe int
	}{
		{name: "save fails", saveErr: assert.AnError, wantProbe: 1},
		{name: "save succeeds", saveErr: nil, wantProbe: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doctorMock := &buildHealthReporterMock{}
			runner := &XcodebuildRunner{
				Config:        xcelerate.Config{},
				InvocationID:  "inv-id",
				Logger:        relationTestLogger,
				invocationAPI: &stubInvocationSaver{err: tc.saveErr},
				Doctor:        doctorMock,
			}

			runner.saveInvocationAndRelation(context.Background(), testInvocation(), 0, 0)

			assert.Len(t, doctorMock.OnInvocationSaveFailureCalls(), tc.wantProbe)
		})
	}
}

func TestRun_QueryInvocationSkipsDoctor(t *testing.T) {
	doctorMock := &buildHealthReporterMock{}

	runner := &XcodebuildRunner{
		Config:       xcelerate.Config{},
		InvocationID: "inv-id",
		Logger:       log.NewLogger(log.WithOutput(io.Discard)),
		XcodeRunner:  stubXcodeRunner{},
		XcodeArgs: &xcodeargsMocks.XcodeArgsMock{
			HasBuildActionFunc: func() bool { return false },
		},
		Doctor: doctorMock,
	}

	runner.Run(context.Background())

	require.Empty(t, doctorMock.CheckAtStartCalls(), "a query invocation runs no build to diagnose")
	require.Empty(t, doctorMock.ReportAtEndCalls())
}
