package common

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/auth/keychain"
)

type fakeAuthLoader struct {
	creds keychain.Credentials
	err   error
}

func (f fakeAuthLoader) Load() (keychain.Credentials, error) { return f.creds, f.err }

func TestResolveAuthConfig_envVarsWinOverKeychain(t *testing.T) {
	loader := fakeAuthLoader{creds: keychain.Credentials{AuthToken: "kc-tok", WorkspaceID: "kc-ws"}}
	envs := map[string]string{
		"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "env-tok",
		"BITRISE_BUILD_CACHE_WORKSPACE_ID": "env-ws",
	}

	got, err := resolveAuthConfig(envs, loader, nil)
	require.NoError(t, err)
	assert.Equal(t, "env-tok", got.AuthToken, "env vars take precedence over stored creds")
	assert.Equal(t, "env-ws", got.WorkspaceID)
}

func TestResolveAuthConfig_jwtEnvWinsOverKeychain(t *testing.T) {
	loader := fakeAuthLoader{creds: keychain.Credentials{AuthToken: "kc-tok", WorkspaceID: "kc-ws"}}
	envs := map[string]string{
		"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": makeUMAJWT("ci-ws"),
	}

	got, err := resolveAuthConfig(envs, loader, nil)
	require.NoError(t, err)
	assert.Equal(t, "ci-ws", got.WorkspaceID, "CI JWT env var overrides stored creds")
	assert.True(t, got.IsJWT)
}

func TestResolveAuthConfig_noEnv_keychainHit(t *testing.T) {
	loader := fakeAuthLoader{creds: keychain.Credentials{AuthToken: "kc-tok", WorkspaceID: "kc-ws"}}

	got, err := resolveAuthConfig(map[string]string{}, loader, nil)
	require.NoError(t, err)
	assert.Equal(t, "kc-tok", got.AuthToken)
	assert.Equal(t, "kc-ws", got.WorkspaceID)
}

func TestResolveAuthConfig_noEnv_keychainNotFound_returnsEnvNotSetError(t *testing.T) {
	loader := fakeAuthLoader{err: keychain.ErrNotFound}

	_, err := resolveAuthConfig(map[string]string{}, loader, nil)
	require.ErrorIs(t, err, ErrAuthTokenNotProvided)
}

func TestResolveAuthConfig_noEnv_keychainError_returnsEnvNotSetError(t *testing.T) {
	loader := fakeAuthLoader{err: errors.New("dbus connection failed")}

	_, err := resolveAuthConfig(map[string]string{}, loader, nil)
	require.ErrorIs(t, err, ErrAuthTokenNotProvided)
}

func TestResolveAuthConfig_noEnv_keychainPartial_returnsEnvNotSetError(t *testing.T) {
	loader := fakeAuthLoader{creds: keychain.Credentials{AuthToken: "kc-tok"}}

	_, err := resolveAuthConfig(map[string]string{}, loader, nil)
	require.ErrorIs(t, err, ErrAuthTokenNotProvided)
}

func TestResolveAuthConfig_partialEnv_fallsBackToKeychain(t *testing.T) {
	loader := fakeAuthLoader{creds: keychain.Credentials{AuthToken: "kc-tok", WorkspaceID: "kc-ws"}}
	envs := map[string]string{
		"BITRISE_BUILD_CACHE_AUTH_TOKEN": "env-tok",
	}

	got, err := resolveAuthConfig(envs, loader, nil)
	require.NoError(t, err)
	assert.Equal(t, "kc-tok", got.AuthToken, "incomplete env vars should not silently mix with keychain")
	assert.Equal(t, "kc-ws", got.WorkspaceID)
}

func TestResolveAuthConfig_noEnv_noKeychain_fallsBackToMultiplatformConfig(t *testing.T) {
	loader := fakeAuthLoader{err: keychain.ErrNotFound}
	readMp := func() (CacheAuthConfig, error) {
		return CacheAuthConfig{AuthToken: "mp-tok", WorkspaceID: "mp-ws"}, nil
	}

	got, err := resolveAuthConfig(map[string]string{}, loader, readMp)
	require.NoError(t, err)
	assert.Equal(t, "mp-tok", got.AuthToken)
	assert.Equal(t, "mp-ws", got.WorkspaceID)
}

func TestResolveAuthConfig_keychainWinsOverMultiplatformConfig(t *testing.T) {
	loader := fakeAuthLoader{creds: keychain.Credentials{AuthToken: "kc-tok", WorkspaceID: "kc-ws"}}
	readMp := func() (CacheAuthConfig, error) {
		return CacheAuthConfig{AuthToken: "mp-tok", WorkspaceID: "mp-ws"}, nil
	}

	got, err := resolveAuthConfig(map[string]string{}, loader, readMp)
	require.NoError(t, err)
	assert.Equal(t, "kc-tok", got.AuthToken, "keychain takes precedence over multiplatform config")
	assert.Equal(t, "kc-ws", got.WorkspaceID)
}

func TestResolveAuthConfig_envWinsOverMultiplatformConfig(t *testing.T) {
	loader := fakeAuthLoader{err: keychain.ErrNotFound}
	readMp := func() (CacheAuthConfig, error) {
		return CacheAuthConfig{AuthToken: "mp-tok", WorkspaceID: "mp-ws"}, nil
	}
	envs := map[string]string{
		"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "env-tok",
		"BITRISE_BUILD_CACHE_WORKSPACE_ID": "env-ws",
	}

	got, err := resolveAuthConfig(envs, loader, readMp)
	require.NoError(t, err)
	assert.Equal(t, "env-tok", got.AuthToken, "env vars take precedence over multiplatform config")
	assert.Equal(t, "env-ws", got.WorkspaceID)
}

func TestResolveAuthConfig_multiplatformReaderErrorIsSwallowed(t *testing.T) {
	loader := fakeAuthLoader{err: keychain.ErrNotFound}
	readMp := func() (CacheAuthConfig, error) {
		return CacheAuthConfig{}, errors.New("disk read failed")
	}

	_, err := resolveAuthConfig(map[string]string{}, loader, readMp)
	require.ErrorIs(t, err, ErrAuthTokenNotProvided, "multiplatform read error falls through to env-not-set canonical error")
}

func TestResolveAuthConfig_multiplatformPartial_fallsThroughToEnvError(t *testing.T) {
	loader := fakeAuthLoader{err: keychain.ErrNotFound}
	readMp := func() (CacheAuthConfig, error) {
		return CacheAuthConfig{AuthToken: "mp-tok"}, nil
	}

	_, err := resolveAuthConfig(map[string]string{}, loader, readMp)
	require.ErrorIs(t, err, ErrAuthTokenNotProvided)
}

func makeJWT(payload map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))

	payloadJSON, err := json.Marshal(payload) //nolint:errchkjson
	if err != nil {
		panic(err)
	}

	body := base64.RawURLEncoding.EncodeToString(payloadJSON)
	sig := base64.RawURLEncoding.EncodeToString([]byte("signature"))

	return header + "." + body + "." + sig
}

func makeUMAJWT(orgID string) string {
	return makeJWT(map[string]any{
		"authorization": map[string]any{
			"permissions": []map[string]any{
				{
					"rsname": "default",
					"claims": map[string]any{
						"org_id": []string{orgID},
						"app_id": []string{"test-app"},
					},
				},
			},
		},
	})
}

func TestReadAuthConfigFromEnvironments(t *testing.T) {
	validJWT := makeUMAJWT("jwt-workspace-id")

	tests := []struct {
		name           string
		envVars        map[string]string
		expectedError  string
		expectedConfig CacheAuthConfig
	}{
		{
			name:          "No envs specified",
			envVars:       map[string]string{},
			expectedError: ErrAuthTokenNotProvided.Error(),
		},
		{
			name: "Only BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN set with valid JWT",
			envVars: map[string]string{
				"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": validJWT,
			},
			expectedConfig: CacheAuthConfig{
				AuthToken:   validJWT,
				WorkspaceID: "jwt-workspace-id",
				IsJWT:       true,
			},
		},
		{
			name: "Both BITRISE_BUILD_CACHE_AUTH_TOKEN and BITRISE_BUILD_CACHE_WORKSPACE_ID set",
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "auth-token",
				"BITRISE_BUILD_CACHE_WORKSPACE_ID": "workspace-id",
			},
			expectedConfig: CacheAuthConfig{
				AuthToken:   "auth-token",
				WorkspaceID: "workspace-id",
			},
		},
		{
			name: "Only BITRISE_BUILD_CACHE_AUTH_TOKEN set",
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_AUTH_TOKEN": "auth-token",
			},
			expectedError: ErrWorkspaceIDNotProvided.Error(),
		},
		{
			name: "Only BITRISE_BUILD_CACHE_WORKSPACE_ID set",
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_WORKSPACE_ID": "workspace-id",
			},
			expectedError: ErrAuthTokenNotProvided.Error(),
		},
		{
			name: "JWT with missing org_id claim",
			envVars: map[string]string{
				"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": makeJWT(map[string]any{
					"authorization": map[string]any{
						"permissions": []map[string]any{
							{
								"rsname": "default",
								"claims": map[string]any{
									"app_id": []string{"app-id"},
								},
							},
						},
					},
				}),
			},
			expectedError: "extract workspace ID from JWT: org_id claim is missing from JWT",
		},
		{
			name: "JWT with empty org_id claim",
			envVars: map[string]string{
				"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": makeJWT(map[string]any{
					"authorization": map[string]any{
						"permissions": []map[string]any{
							{
								"rsname": "default",
								"claims": map[string]any{
									"org_id": []string{""},
								},
							},
						},
					},
				}),
			},
			expectedError: "extract workspace ID from JWT: org_id claim is empty in JWT",
		},
		{
			name: "JWT with no default permission",
			envVars: map[string]string{
				"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": makeJWT(map[string]any{
					"authorization": map[string]any{
						"permissions": []map[string]any{
							{
								"rsname": "other",
								"claims": map[string]any{
									"org_id": []string{"some-org"},
								},
							},
						},
					},
				}),
			},
			expectedError: "extract workspace ID from JWT: 'default' permission not found in JWT",
		},
		{
			name: "Invalid JWT format",
			envVars: map[string]string{
				"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": "not-a-jwt",
			},
			expectedError: "extract workspace ID from JWT: invalid JWT format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := ReadAuthConfigFromEnvironments(tt.envVars)
			if tt.expectedError != "" {
				require.EqualError(t, err, tt.expectedError)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedConfig, config)
			}
		})
	}
}

func TestTokenInGradleFormat(t *testing.T) {
	tests := []struct {
		name   string
		config CacheAuthConfig
		want   string
	}{
		{
			name: "PAT with workspace ID",
			config: CacheAuthConfig{
				AuthToken:   "auth-token",
				WorkspaceID: "workspace-id",
			},
			want: "workspace-id:auth-token",
		},
		{
			name: "JWT returns token as-is",
			config: CacheAuthConfig{
				AuthToken:   "jwt-token",
				WorkspaceID: "workspace-id",
				IsJWT:       true,
			},
			want: "jwt-token",
		},
		{
			name: "No workspace ID returns token as-is",
			config: CacheAuthConfig{
				AuthToken: "auth-token",
			},
			want: "auth-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.config.TokenInGradleFormat())
		})
	}
}

func TestExtractWorkspaceIDFromJWT(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		want    string
		wantErr string
	}{
		{
			name:  "valid JWT with org_id",
			token: makeUMAJWT("my-org"),
			want:  "my-org",
		},
		{
			name:    "invalid format - not enough parts",
			token:   "only.two",
			wantErr: "invalid JWT format",
		},
		{
			name:    "invalid base64 payload",
			token:   "header.!!!invalid!!!.sig",
			wantErr: "decode JWT payload",
		},
		{
			name:    "invalid JSON payload",
			token:   "header." + base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".sig",
			wantErr: "parse JWT payload",
		},
		{
			name: "missing default permission",
			token: makeJWT(map[string]any{
				"authorization": map[string]any{
					"permissions": []map[string]any{},
				},
			}),
			wantErr: "'default' permission not found in JWT",
		},
		{
			name: "missing org_id in default permission",
			token: makeJWT(map[string]any{
				"authorization": map[string]any{
					"permissions": []map[string]any{
						{"rsname": "default", "claims": map[string]any{}},
					},
				},
			}),
			wantErr: "org_id claim is missing from JWT",
		},
		{
			name: "empty org_id",
			token: makeJWT(map[string]any{
				"authorization": map[string]any{
					"permissions": []map[string]any{
						{"rsname": "default", "claims": map[string]any{"org_id": []string{""}}},
					},
				},
			}),
			wantErr: "org_id claim is empty in JWT",
		},
		{
			name: "picks default permission among multiple",
			token: makeJWT(map[string]any{
				"authorization": map[string]any{
					"permissions": []map[string]any{
						{"rsname": "other", "claims": map[string]any{"org_id": []string{"wrong"}}},
						{"rsname": "default", "claims": map[string]any{"org_id": []string{"correct"}}},
					},
				},
			}),
			want: "correct",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractWorkspaceIDFromJWT(tt.token)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
