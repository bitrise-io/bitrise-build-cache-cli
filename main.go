package main

import (
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/auth"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/bazel"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/browse"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/daemon"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/doctor"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/file"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/gradle"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/reactnative"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/update"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/xcode"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/xcode_app"
)

func main() {
	common.Execute()
}
