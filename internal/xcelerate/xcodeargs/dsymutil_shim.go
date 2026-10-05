package xcodeargs

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/dsymshim"
)

// DsymutilShimToolchainsArg returns the TOOLCHAINS build setting entry when the
// dsymutil CAS shim is both staged on disk and not killswitched out. Returns a
// zero map when either precondition is false, so the caller can unconditionally
// merge it into `additional` without a nil check.
//
// The decision runs at wrapper time (per xcodebuild invocation) rather than
// activate time so an operator can flip the killswitch without reactivating.
func DsymutilShimToolchainsArg(p paths.Paths, osProxy utils.OsProxy, env map[string]string) map[string]string {
	if env[dsymshim.EnvKillSwitch] != "" {
		return nil
	}

	if osProxy == nil {
		osProxy = utils.DefaultOsProxy{}
	}

	shimPath := p.DsymutilCasShimPath()
	if _, err := osProxy.Stat(shimPath); err != nil {
		return nil
	}

	return map[string]string{ToolchainsKey: DsymutilCasShimToolchainsValue}
}
