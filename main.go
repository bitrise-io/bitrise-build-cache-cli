package main

import (
	_ "github.com/bitrise-io/bitrise-build-cache-cli/cmd/bazel"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/cmd/ccache"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/cmd/gradle"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/cmd/reactnative"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/cmd/xcode"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/common"
)

func main() {
	common.Execute()
}
