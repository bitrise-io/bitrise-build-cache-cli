package paths

import "path/filepath"

const (
	// BazelrcFileName is the per-user bazelrc filename under $HOME.
	BazelrcFileName = ".bazelrc"

	// BazelCredHelperShimName is the shim script `install-credhelper-shim` writes.
	BazelCredHelperShimName = "bitrise-build-cache-credhelper.sh" //nolint:gosec // filename, not a credential

	// BazelCredHelperShimDefaultDir is the default workspace-relative dir the shim lives in.
	BazelCredHelperShimDefaultDir = "tools"

	// BazelWorkspaceCacheBinRelative is the workspace-local dir the shim installs the CLI into.
	BazelWorkspaceCacheBinRelative = ".bitrise-cache/bin"
)

// BazelrcFile returns the absolute path of the per-user ~/.bazelrc file.
func (p Paths) BazelrcFile() string {
	return filepath.Join(p.Home, BazelrcFileName)
}
