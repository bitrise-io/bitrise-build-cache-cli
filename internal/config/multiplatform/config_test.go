//go:build unit

package multiplatform_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform"
)

// roundTrip writes the block the way Config.Save does and reads it back, which is
// the process boundary the provenance has to survive: activation writes the file,
// a later command reads it.
func roundTrip(t *testing.T, in multiplatformconfig.AnalyticsAuthConfig) multiplatformconfig.AnalyticsAuthConfig {
	t.Helper()

	raw, err := json.Marshal(multiplatformconfig.Config{AuthConfig: in})
	require.NoError(t, err)

	var out multiplatformconfig.Config
	require.NoError(t, json.Unmarshal(raw, &out))

	return out.AuthConfig
}

// The credential is a JWT either way, so without a recorded provenance the reader
// would report a brokered token as the CI JWT and send anyone debugging it after
// an env var that is not set on a Build Hub runner.
func TestAnalyticsAuthConfig_BrokeredJWTSurvivesTheFile(t *testing.T) {
	cred := auth.Credential{Token: "jwt-value", WorkspaceID: "org-slug"}
	origin := auth.Origin{Backend: auth.BackendJWT, Provenance: auth.ProvenanceBrokered}

	got := roundTrip(t, multiplatformconfig.NewAnalyticsAuthConfig(cred, origin))

	assert.Equal(t, origin, got.Origin())
	assert.Equal(t, "Build Hub token (brokered)", got.Origin().Label())
}

func TestAnalyticsAuthConfig_InjectedJWTSurvivesTheFile(t *testing.T) {
	cred := auth.Credential{Token: "jwt-value", WorkspaceID: "org-slug"}
	origin := auth.Origin{Backend: auth.BackendJWT, Provenance: auth.ProvenanceInjected}

	got := roundTrip(t, multiplatformconfig.NewAnalyticsAuthConfig(cred, origin))

	assert.Equal(t, origin, got.Origin())
}

// A file written before the CLI could broker holds no provenance, and a JWT in it
// can only have been injected.
func TestAnalyticsAuthConfig_MissingProvenanceReadsAsInjected(t *testing.T) {
	var cfg multiplatformconfig.Config
	require.NoError(t, json.Unmarshal([]byte(`{"authConfig":{"AuthToken":"jwt-value","WorkspaceID":"org-slug","IsJWT":true}}`), &cfg))

	assert.Equal(t,
		auth.Origin{Backend: auth.BackendJWT, Provenance: auth.ProvenanceInjected},
		cfg.AuthConfig.Origin())
}

// A PAT is not a JWT, so it carries no JWT provenance and stays the static block
// credential — GradleToken prefixes it with the workspace, a JWT it would not.
func TestAnalyticsAuthConfig_PATIsUnaffected(t *testing.T) {
	cred := auth.Credential{Token: "pat-value", WorkspaceID: "org-slug"}
	origin := auth.Origin{Backend: auth.BackendKeychain, Provenance: auth.ProvenanceOAuthLogin}

	got := roundTrip(t, multiplatformconfig.NewAnalyticsAuthConfig(cred, origin))

	assert.False(t, got.IsJWT)
	assert.Empty(t, got.Provenance)
	assert.Equal(t,
		auth.Origin{Backend: auth.BackendFile, Provenance: auth.ProvenanceStatic},
		got.Origin())
}
