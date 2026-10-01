//go:build unit

package reactnative

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnabledBuildTools(t *testing.T) {
	prevGradle, prevXcode, prevCpp := gradleEnabled, xcodeEnabled, cppEnabled
	t.Cleanup(func() { gradleEnabled, xcodeEnabled, cppEnabled = prevGradle, prevXcode, prevCpp })

	tests := []struct {
		name   string
		gradle bool
		xcode  bool
		cpp    bool
		want   []string
	}{
		{"all", true, true, true, []string{"gradle", "xcode", "cpp"}},
		{"cpp only, as activate all runs it", false, false, true, []string{"cpp"}},
		{"xcode only", false, true, false, []string{"xcode"}},
		{"none asks nothing", false, false, false, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gradleEnabled, xcodeEnabled, cppEnabled = tt.gradle, tt.xcode, tt.cpp

			assert.Equal(t, tt.want, enabledBuildTools())
		})
	}
}
