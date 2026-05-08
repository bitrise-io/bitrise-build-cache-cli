package dependencies

import (
	"context"
	"fmt"
	"runtime"

	"github.com/bitrise-io/go-utils/v2/log"
)

const (
	ccacheBinaryName = "ccache"
	// ccacheVersion is the pinned upstream ccache version. The bitrise.yml
	// release flow mirrors the matching tarballs to GAR, so this constant
	// drives both the download URL and the GAR mirror upload.
	ccacheVersion = "4.13.2"
)

// CcacheTool returns a Tool that installs ccache.
func CcacheTool() Tool {
	return Tool{
		Name:    ccacheBinaryName,
		Version: ccacheVersion,
		Install: func(ctx context.Context, logger log.Logger) error {
			suffix, err := ccachePlatformSuffix(runtime.GOOS, runtime.GOARCH)
			if err != nil {
				return err
			}

			return installFromMirrors(
				ctx, logger,
				[]string{
					ccacheGARDownloadURL(ccacheVersion, suffix),
					ccacheGitHubDownloadURL(ccacheVersion, suffix),
				},
				ccacheBinaryName,
			)
		},
	}
}

func ccachePlatformSuffix(goos, goarch string) (string, error) {
	switch {
	case goos == "darwin":
		return "darwin", nil
	case goos == "linux" && goarch == "amd64":
		return "linux-x86_64-glibc", nil
	case goos == "linux" && goarch == "arm64":
		return "linux-aarch64-glibc", nil
	default:
		return "", fmt.Errorf("unsupported platform: %s/%s", goos, goarch)
	}
}

func ccacheGitHubDownloadURL(version, platformSuffix string) string {
	return fmt.Sprintf(
		"https://github.com/ccache/ccache/releases/download/v%s/ccache-%s-%s.tar.gz",
		version, version, platformSuffix,
	)
}

func ccacheGARDownloadURL(version, platformSuffix string) string {
	pkg := fmt.Sprintf("ccache-%s.tar.gz", platformSuffix)
	filename := fmt.Sprintf("ccache-%s-%s.tar.gz", version, platformSuffix)

	return garDownloadURL(pkg, version, filename)
}
