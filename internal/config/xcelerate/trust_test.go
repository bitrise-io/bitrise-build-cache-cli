//go:build unit || integration

package xcelerate_test

import "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/trustedurl"

func init() { trustedurl.AllowLoopbackForTests() }
