package dsymshim

import (
	"path/filepath"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Resolve returns the libToolchainCASPlugin.dylib path and the plugin store dir
// to pass as -cas-plugin-path and -cas, along with a short note describing the
// resolution chain. Empty pluginPath means "bypass, we have no plugin".
//
// Resolution order:
//
//	plugin:
//	  1. BITRISE_DSYMUTIL_CAS_PLUGIN_PATH env override (test / escape hatch)
//	  2. DEVELOPER_DIR/usr/lib/libToolchainCASPlugin.dylib (xcodebuild sets this at exec time)
//	  3. (fallback) /Applications/Xcode.app/Contents/Developer/usr/lib/libToolchainCASPlugin.dylib
//
//	cas:
//	  1. BITRISE_DSYMUTIL_CAS_PATH env override
//	  2. <PROJECT_TEMP_DIR>/CompilationCache.noindex/plugin
//	  3. <TARGET_TEMP_DIR>/CompilationCache.noindex/plugin
//	  4. walk from argv's primary input Mach-O upwards to find a DerivedData with
//	     CompilationCache.noindex/plugin.
//	  5. empty -> omit -cas; plugin can still open the store from its own search path
//	     when the DerivedData is colocated with the dSYM output.
func Resolve(argv []string, env map[string]string, osProxy utils.OsProxy) (string, string, string) {
	pluginPath, pluginNote := resolvePluginPath(env, osProxy)
	casPath, casNote := resolveCasPath(argv, env, osProxy)

	note := strings.Join(trimEmpty([]string{pluginNote, casNote}), ";")

	return pluginPath, casPath, note
}

// EnvPluginPathOverride is the escape hatch for the CAS plugin location; useful on
// Bitrise CI stacks that stage Xcode under a non-standard path, and for tests.
const EnvPluginPathOverride = "BITRISE_DSYMUTIL_CAS_PLUGIN_PATH"

// EnvCASPathOverride lets tests + customers pin -cas directly without us inferring it.
const EnvCASPathOverride = "BITRISE_DSYMUTIL_CAS_PATH"

func resolvePluginPath(env map[string]string, osProxy utils.OsProxy) (string, string) {
	if v := env[EnvPluginPathOverride]; v != "" {
		return v, "plugin=env"
	}

	if dev := env["DEVELOPER_DIR"]; dev != "" {
		candidate := filepath.Join(dev, "usr", "lib", "libToolchainCASPlugin.dylib")
		if _, err := osProxy.Stat(candidate); err == nil {
			return candidate, "plugin=developer-dir"
		}
	}

	fallback := "/Applications/Xcode.app/Contents/Developer/usr/lib/libToolchainCASPlugin.dylib"
	if _, err := osProxy.Stat(fallback); err == nil {
		return fallback, "plugin=xcode-fallback"
	}

	return "", "plugin=missing"
}

func resolveCasPath(argv []string, env map[string]string, osProxy utils.OsProxy) (string, string) {
	if v := env[EnvCASPathOverride]; v != "" {
		return v, "cas=env"
	}

	for _, key := range []string{"PROJECT_TEMP_DIR", "TARGET_TEMP_DIR"} {
		base := env[key]
		if base == "" {
			continue
		}
		// Apple places CompilationCache.noindex at the DerivedData root, which is the
		// grandparent of PROJECT_TEMP_DIR/.../Build/Intermediates.noindex.
		for _, probe := range []string{
			filepath.Join(base, paths.CompilationCachePluginDirRelative),
			filepath.Join(derivedDataFromTempDir(base), paths.CompilationCachePluginDirRelative),
		} {
			if probe == "" {
				continue
			}
			if _, err := osProxy.Stat(probe); err == nil {
				return probe, "cas=" + key
			}
		}
	}

	if primary := firstPositional(argv); primary != "" {
		if dd := findDerivedDataRoot(primary, osProxy); dd != "" {
			candidate := filepath.Join(dd, paths.CompilationCachePluginDirRelative)
			if _, err := osProxy.Stat(candidate); err == nil {
				return candidate, "cas=walked"
			}
		}
	}

	return "", "cas=missing"
}

func derivedDataFromTempDir(tempDir string) string {
	// PROJECT_TEMP_DIR usually looks like
	// <DD>/Build/Intermediates.noindex/<Project>.build/<Config>
	// so chop at /Build/.
	marker := string(filepath.Separator) + "Build" + string(filepath.Separator)
	if idx := strings.Index(tempDir, marker); idx >= 0 {
		return tempDir[:idx]
	}

	return ""
}

func findDerivedDataRoot(binary string, osProxy utils.OsProxy) string {
	dir := filepath.Dir(binary)
	for i := 0; i < 12 && dir != "/" && dir != "."; i++ {
		candidate := filepath.Join(dir, paths.CompilationCachePluginDirRelative)
		if _, err := osProxy.Stat(candidate); err == nil {
			return dir
		}

		dir = filepath.Dir(dir)
	}

	return ""
}

func trimEmpty(parts []string) []string {
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}

	return out
}
