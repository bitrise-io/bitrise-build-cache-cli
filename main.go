package main

import (
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/auth"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/bazel"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/browse"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/daemon"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/doctor"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/file"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/gradle"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/reactnative"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/update"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/xcode"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/xcode_app"
)

func main() {
	common.Execute()
}
