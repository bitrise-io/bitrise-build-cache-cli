package gradleconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGradlePluginsMirrorURL(t *testing.T) {
	cases := map[string]struct {
		envs map[string]string
		want string
	}{
		"disabled": {
			envs: map[string]string{MirrorDatacenterEnvKey: "AMS1"},
			want: "",
		},
		"enabled but no datacenter": {
			envs: map[string]string{MirrorEnabledEnvKey: "true"},
			want: "",
		},
		"AMS1": {
			envs: map[string]string{MirrorEnabledEnvKey: "true", MirrorDatacenterEnvKey: "AMS1"},
			want: "https://repository-manager-ams.services.bitrise.io:8090/maven/gradle-plugins",
		},
		"IAD1": {
			envs: map[string]string{MirrorEnabledEnvKey: "true", MirrorDatacenterEnvKey: "IAD1"},
			want: "https://repository-manager-iad.services.bitrise.io:8090/maven/gradle-plugins",
		},
		"unsupported ATL1": {
			envs: map[string]string{MirrorEnabledEnvKey: "true", MirrorDatacenterEnvKey: "ATL1"},
			want: "",
		},
		"unsupported customer-private GCP US_EAST4": {
			envs: map[string]string{MirrorEnabledEnvKey: "true", MirrorDatacenterEnvKey: "US_EAST4"},
			want: "",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, GradlePluginsMirrorURL(tc.envs))
		})
	}
}
