//go:build unit

package xcode

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
)

func TestDebugFlag_ORsGlobal_ActivateXcode(t *testing.T) {
	t.Cleanup(func() { common.IsDebugLogMode = false })

	common.IsDebugLogMode = true
	params := xcelerate.Params{DebugLogging: false}

	params.DebugLogging = common.DebugEnabled(params.DebugLogging)

	assert.True(t, params.DebugLogging, "global -d must OR into activateXcodeParams.DebugLogging")
}
