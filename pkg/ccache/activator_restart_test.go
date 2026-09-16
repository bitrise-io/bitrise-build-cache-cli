//go:build unit

package ccache

import (
	"context"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"

	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
)

func fakeRestartSeams(t *testing.T, listening bool) *int {
	t.Helper()

	var stopCalls int
	prevIsListening, prevStop := isListeningFn, stopHelperFn
	isListeningFn = func(string) bool { return listening }
	stopHelperFn = func(context.Context, log.Logger, string) error {
		stopCalls++

		return nil
	}
	t.Cleanup(func() { isListeningFn, stopHelperFn = prevIsListening, prevStop })

	return &stopCalls
}

func TestRestartHelperOnConfigDelta_StopsHelperOnModeFlip(t *testing.T) {
	stopCalls := fakeRestartSeams(t, true)

	a := NewActivator(ActivatorParams{Logger: log.NewLogger(), Envs: map[string]string{}})
	a.restartHelperOnConfigDelta(
		context.Background(),
		ccacheconfig.Config{ProjectMode: machineconfig.ModeAlways, IPCEndpoint: "/tmp/test.sock"},
		ccacheconfig.Config{ProjectMode: machineconfig.ModeOptIn, IPCEndpoint: "/tmp/test.sock"},
	)

	assert.Equal(t, 1, *stopCalls, "a mode flip with a live helper must stop it once")
}

func TestRestartHelperOnConfigDelta_NoOpWithoutListener(t *testing.T) {
	stopCalls := fakeRestartSeams(t, false)

	a := NewActivator(ActivatorParams{Logger: log.NewLogger(), Envs: map[string]string{}})
	a.restartHelperOnConfigDelta(
		context.Background(),
		ccacheconfig.Config{ProjectMode: machineconfig.ModeAlways, IPCEndpoint: "/tmp/test.sock"},
		ccacheconfig.Config{ProjectMode: machineconfig.ModeOptIn, IPCEndpoint: "/tmp/test.sock"},
	)

	assert.Zero(t, *stopCalls, "no listener means no stop attempt")
}
