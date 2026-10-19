//go:build unit

package xcode

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodeargs"
)

func Test_assembleArgs_compilationCachePath(t *testing.T) {
	const managed = "COMPILATION_CACHE_CAS_PATH=/home with spaces/.bitrise/cache/xcode-cas"
	for _, tt := range []struct {
		name   string
		config xcelerate.Config
		args   []string
		want   string
	}{
		{name: "target build", config: xcelerate.Config{BuildCacheEnabled: true}, want: managed},
		{name: "prefix mapping disabled", config: xcelerate.Config{BuildCacheEnabled: true, DisablePrefixMapping: true}, want: managed},
		{name: "Swift caching disabled", config: xcelerate.Config{BuildCacheEnabled: true, NoSwiftCache: true}, want: managed},
		{name: "user path", config: xcelerate.Config{BuildCacheEnabled: true}, args: []string{"COMPILATION_CACHE_CAS_PATH=/custom/cache"}, want: "COMPILATION_CACHE_CAS_PATH=/custom/cache"},
		{name: "cache disabled"},
		{name: "cache flags skipped", config: xcelerate.Config{BuildCacheEnabled: true, BuildCacheSkipFlags: true}},
		{name: "query", config: xcelerate.Config{BuildCacheEnabled: true}, args: []string{"-showBuildSettings"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			argv := append([]string{"-project", "Example.xcodeproj", "-target", "Example"}, tt.args...)
			r := XcodebuildRunner{
				Config:     tt.config,
				Logger:     bundleTestLogger,
				Paths:      paths.FromHome("/home with spaces"),
				NoXcresult: true,
				XcodeArgs:  xcodeargs.NewDefault(&cobra.Command{Use: "xcodebuild"}, argv, bundleTestLogger),
			}
			got := r.assembleArgs()
			var cachePaths []string
			for _, arg := range got {
				if strings.HasPrefix(arg, "COMPILATION_CACHE_CAS_PATH=") {
					cachePaths = append(cachePaths, arg)
				}
			}
			if tt.want == "" {
				assert.Empty(t, cachePaths)
			} else {
				assert.Equal(t, []string{tt.want}, cachePaths)
			}
			assert.NotContains(t, got, "-derivedDataPath")
		})
	}
}
