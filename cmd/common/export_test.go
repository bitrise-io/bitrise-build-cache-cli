//go:build unit

package common

import (
	"context"

	"github.com/bitrise-io/go-utils/v2/log"
)

type DeactivateFuncForTest = func(context.Context, log.Logger, bool) error

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
