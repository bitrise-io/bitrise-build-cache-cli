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

func TestAnalyticsAuthConfig_BrokeredJWTSurvivesTheFile(t *testing.T) {
	cred := auth.Credential{Token: "jwt-value", WorkspaceID: "org-slug"}
	origin := auth.Origin{Backend: auth.BackendJWT, Provenance: auth.ProvenanceBrokered}

	got := roundTrip(t, multiplatformconfig.NewAnalyticsAuthConfig(cred, origin))

	assert.True(t, got.IsJWT)
	assert.Equal(t, multiplatformconfig.ProvenanceBrokered, got.Provenance)
}

func TestAnalyticsAuthConfig_InjectedJWTSurvivesTheFile(t *testing.T) {
	cred := auth.Credential{Token: "jwt-value", WorkspaceID: "org-slug"}
	origin := auth.Origin{Backend: auth.BackendJWT, Provenance: auth.ProvenanceInjected}

	got := roundTrip(t, multiplatformconfig.NewAnalyticsAuthConfig(cred, origin))

	assert.True(t, got.IsJWT)
	assert.Equal(t, multiplatformconfig.ProvenanceInjected, got.Provenance)
}

func TestAnalyticsAuthConfig_PATIsUnaffected(t *testing.T) {
	cred := auth.Credential{Token: "pat-value", WorkspaceID: "org-slug"}
	origin := auth.Origin{Backend: auth.BackendKeychain, Provenance: auth.ProvenanceOAuthLogin}

	got := roundTrip(t, multiplatformconfig.NewAnalyticsAuthConfig(cred, origin))

	assert.False(t, got.IsJWT)
	assert.Empty(t, got.Provenance)
}
