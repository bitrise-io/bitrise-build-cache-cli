//go:build unit

package common

import "testing"

func TestSanitizeCacheKeyComponent(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "no slash is unchanged", in: "main", want: "main"},
		{name: "single slash replaced", in: "renovate/all-non-major-updates", want: "renovate_all-non-major-updates"},
		{name: "multiple slashes replaced", in: "feature/team/thing", want: "feature_team_thing"},
		{name: "empty stays empty", in: "", want: ""},
		{name: "other characters preserved", in: "fix-123.4_x", want: "fix-123.4_x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeCacheKeyComponent(tt.in); got != tt.want {
				t.Errorf("SanitizeCacheKeyComponent(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
