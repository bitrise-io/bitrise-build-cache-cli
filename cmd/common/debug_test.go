//go:build unit

package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A supervised process is started without the CLI flag, so its config has to be
// able to turn debug on by itself.
func TestDebugEnabled(t *testing.T) {
	for _, tc := range []struct{ source, global, want bool }{
		{false, false, false},
		{true, false, true},
		{false, true, true},
		{true, true, true},
	} {
		IsDebugLogMode = tc.global
		t.Cleanup(func() { IsDebugLogMode = false })

		assert.Equal(t, tc.want, DebugEnabled(tc.source))
	}
}
