package dependencies

import "fmt"

// Public mirror in Google Artifact Registry (generic format, public read).
const (
	garProject  = "ip-build-cache-prod"
	garLocation = "us-central1"
	garRepo     = "build-cache-cli-releases"
)

// garDownloadURL builds the unauthenticated GAR generic-package download URL.
// Use only with HTTP GET — GAR returns 404 on HEAD for these media-download URLs.
func garDownloadURL(pkg, version, filename string) string {
	return fmt.Sprintf(
		"https://artifactregistry.googleapis.com/v1/projects/%s/locations/%s/repositories/%s/files/%s:%s:%s:download?alt=media",
		garProject, garLocation, garRepo, pkg, version, filename,
	)
}
