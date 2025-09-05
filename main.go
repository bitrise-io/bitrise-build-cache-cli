package main

import (
	_ "github.com/bitrise-io/bitrise-build-cache-cli/cmd/bazel"
	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/common"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/cmd/gradle"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/cmd/xcode"
)

func main() {
	common.Execute()
}
