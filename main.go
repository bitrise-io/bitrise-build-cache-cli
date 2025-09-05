package main

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/cmd"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/cmd/bazel"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/cmd/gradle"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/cmd/xcode"
)

func main() {
	cmd.Execute()
}
