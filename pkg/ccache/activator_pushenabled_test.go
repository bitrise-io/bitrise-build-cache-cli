//go:build unit

package ccache

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewActivator_PushEnabledPropagates(t *testing.T) {
	cases := []struct {
		name string
		push bool
	}{
		{name: "push enabled", push: true},
		{name: "push disabled", push: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := NewActivator(ActivatorParams{PushEnabled: tc.push})

			assert.Equal(t, tc.push, a.pushEnabled)
		})
	}
}
