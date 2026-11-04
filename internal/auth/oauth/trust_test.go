//go:build unit || integration

package oauth

import "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/trustedurl"

func init() { trustedurl.AllowLoopbackForTests() }
