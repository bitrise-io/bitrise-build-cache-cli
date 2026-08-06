package reactnative

import (
	"context"

	"github.com/bitrise-io/go-utils/v2/log"

	doctorpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/doctor"
)

//go:generate moq -stub -out doctor_gate_mock_test.go -pkg reactnative . buildHealthReporter

type buildHealthReporter interface {
	CheckAtStart(ctx context.Context)
	ReportAtEnd(ctx context.Context, outcome buildOutcome)
	OnInvocationSaveFailure(ctx context.Context)
}

// buildOutcome is what only the finished run knows.
type buildOutcome struct {
	// ChildInvocations is how many nested build-tool invocations reached the ledger.
	ChildInvocations     int
	InvocationSaveFailed bool
}

// rnDoctor runs the RN-relevant checks around a wrapped command.
type rnDoctor struct {
	Logger    log.Logger
	Debug     bool
	RunChecks func(ctx context.Context, opts doctorpkg.Options) doctorpkg.Report

	gate *doctorpkg.Gate
}

func (d *rnDoctor) CheckAtStart(ctx context.Context) {
	d.doctorGate().CheckAtStart(ctx, doctorpkg.ReactNativeCheckNames)
}

func (d *rnDoctor) ReportAtEnd(_ context.Context, outcome buildOutcome) {
	// Zero children means the nested build tools never went through the Bitrise
	// wrappers — but a wrapped command that isn't a build has none either, so it
	// can't be a warning without knowing which case this is.
	d.Logger.Debugf("Child invocations recorded during this build: %d", outcome.ChildInvocations)

	d.doctorGate().Recap()
}

func (d *rnDoctor) OnInvocationSaveFailure(ctx context.Context) {
	d.doctorGate().ProbeAuth(ctx)
}

func (d *rnDoctor) doctorGate() *doctorpkg.Gate {
	if d.gate == nil {
		d.gate = &doctorpkg.Gate{Logger: d.Logger, Debug: d.Debug, RunChecks: d.RunChecks}
	}

	return d.gate
}
