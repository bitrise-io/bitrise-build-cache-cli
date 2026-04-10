package dependencies

import (
	"fmt"
	"runtime"

	"github.com/bitrise-io/go-utils/v2/log"
)

const (
	ccacheBinaryName = "ccache"
	ccacheVersion    = "4.13.2"
)

// CcacheTool returns a Tool that installs ccache.
func CcacheTool() Tool {
	return Tool{
		Name:    ccacheBinaryName,
		Version: ccacheVersion,
		Install: func(logger log.Logger) error {
			url, err := ccacheDownloadURL(ccacheVersion, runtime.GOOS, runtime.GOARCH)
			if err != nil {
				return err
			}

			return installFromGitHubRelease(logger, url, ccacheBinaryName)
		},
	}
}

func ccacheDownloadURL(version, goos, goarch string) (string, error) {
	var platformSuffix string

	switch {
	case goos == "darwin":
		platformSuffix = "darwin"
	case goos == "linux" && goarch == "amd64":
		platformSuffix = "linux-x86_64-glibc"
	case goos == "linux" && goarch == "arm64":
		platformSuffix = "linux-aarch64-glibc"
	default:
		return "", fmt.Errorf("unsupported platform: %s/%s", goos, goarch)
	}

	return fmt.Sprintf(
		"https://github.com/ccache/ccache/releases/download/v%s/ccache-%s-%s.tar.gz",
		version, version, platformSuffix,
	), nil
}
