package main

import (
	_ "github.com/bitrise-io/bitrise-build-cache-cli/cmd/bazel"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/cmd/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/common"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/cmd/gradle"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/cmd/reactnative"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/cmd/xcode"
)

func main() {
	common.Execute()
}
