//go:build unit

package common

import (
	"context"

	"github.com/bitrise-io/go-utils/v2/log"
)

// DeactivateFuncForTest is the fan-out hook signature used by
// `deactivate all`. Exported for tests that need to spy on the fan-out.
type DeactivateFuncForTest = func(context.Context, log.Logger, bool) error

// SwapDeactivateAllFansForTest swaps every `deactivate all` fan-out hook and
// returns a restore func. Tests use this to spy on the fan-out without invoking
// the real per-tool cleanup.
func SwapDeactivateAllFansForTest(
	rn, gradle, bazel, xcode, ccache DeactivateFuncForTest,
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
