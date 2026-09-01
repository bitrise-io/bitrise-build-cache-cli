//go:build unit

package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDebugEnabled(t *testing.T) {
	t.Cleanup(func() { IsDebugLogMode = false })

	cases := []struct {
		name   string
		source bool
		global bool
		want   bool
	}{
		{"both off", false, false, false},
		{"source on, global off", true, false, true},
		{"source off, global on", false, true, true},
		{"both on", true, true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			IsDebugLogMode = tc.global
			assert.Equal(t, tc.want, DebugEnabled(tc.source))
		})
	}
}
