//go:build unit

package daemon

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsTransientBinaryPath(t *testing.T) {
	cases := map[string]bool{
		"/tmp/bitrise-build-cache":                          true,
		"/private/tmp/bitrise-build-cache":                  true,
		"/var/folders/xy/abc/T/bitrise-build-cache":         true,
		"/private/var/folders/xy/abc/T/bitrise-build-cache": true,
		"/Users/me/.local/bin/bitrise-build-cache":          false,
		"/opt/homebrew/bin/bitrise-build-cache":             false,
		"/usr/local/bin/bitrise-build-cache":                false,
		"":                                                  false,
	}

	for path, want := range cases {
		assert.Equal(t, want, isTransientBinaryPath(path), "path=%q", path)
	}
}

func TestErrTransientBinaryPath_mentionsPath(t *testing.T) {
	err := errTransientBinaryPath("/tmp/bitrise-build-cache")
	assert.ErrorContains(t, err, "/tmp/bitrise-build-cache")
	assert.ErrorContains(t, err, "transient path")
}
