package doctor

import (
	"context"
	"fmt"
	"os"
	"strings"

	xceleratconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func (d *Doctor) xcelerateXcconfigCheck() Check {
	dir := xceleratconfig.DirPath(utils.DefaultOsProxy{})

	return Check{
		Name: "xcelerate-xcconfig",
		Diagnose: func(_ context.Context) Result {
			info, err := os.Stat(dir)
			if err != nil || !info.IsDir() {
				return Result{State: StateOK, Detail: "xcelerate not activated (skipping check)"}
			}

			entries, err := os.ReadDir(dir)
			if err != nil {
				return Result{State: StateError, Detail: "read " + dir + ": " + err.Error()}
			}

			var xcconfigs []string
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".xcconfig") {
					xcconfigs = append(xcconfigs, e.Name())
				}
			}

			if len(xcconfigs) == 0 {
				return Result{State: StateOK, Detail: "xcelerate dir exists but no xcconfig files (no Xcode local activation yet)"}
			}

			return Result{State: StateOK, Detail: fmt.Sprintf("%d xcconfig file(s) present in %s", len(xcconfigs), dir)}
		},
	}
}
