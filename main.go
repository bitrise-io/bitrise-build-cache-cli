package main

import (
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/auth"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/bazel"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/file"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/gradle"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/health"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/reactnative"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/xcode"
)

func main() {
	common.Execute()
}
