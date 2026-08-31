//go:build unit

package common

import (
	"context"

	"github.com/bitrise-io/go-utils/v2/log"
)

// SwapDeactivateAllFansForTest swaps every `deactivate all` fan-out hook and
// returns a restore func. Tests use this to spy on the fan-out without invoking
// the real per-tool cleanup.
func SwapDeactivateAllFansForTest(
	rn func(log.Logger, bool) error,
	gradle func(log.Logger, bool) error,
	bazel func(log.Logger, bool) error,
	xcode func(log.Logger, bool) error,
	ccache func(context.Context, log.Logger, bool) error,
) func() {
	prevRN, prevGradle, prevBazel, prevXcode, prevCcache :=
		deactivateAllReactNativeFn, deactivateAllGradleFn,
		deactivateAllBazelFn, deactivateAllXcodeFn, deactivateAllCcacheFn

	deactivateAllReactNativeFn = rn
	deactivateAllGradleFn = gradle
	deactivateAllBazelFn = bazel
	deactivateAllXcodeFn = xcode
	deactivateAllCcacheFn = ccache

	return func() {
		deactivateAllReactNativeFn = prevRN
		deactivateAllGradleFn = prevGradle
		deactivateAllBazelFn = prevBazel
		deactivateAllXcodeFn = prevXcode
		deactivateAllCcacheFn = prevCcache
	}
}
