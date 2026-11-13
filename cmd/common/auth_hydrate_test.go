//go:build unit

package common

import (
	"context"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

type hydrateStore struct {
	ts      auth.TokenSet
	present bool
}

func (s *hydrateStore) Backend() auth.Backend { return auth.BackendKeychain }

func (s *hydrateStore) Load() (auth.TokenSet, error) {
	if !s.present {
		return auth.TokenSet{}, store.ErrNotFound
	}

	return s.ts, nil
}

func (s *hydrateStore) Save(ts auth.TokenSet) error { s.ts, s.present = ts, true; return nil }
func (s *hydrateStore) Clear() error                { s.present = false; return nil }

func hydrateResolver(stored *hydrateStore, refreshes, brokers *int) *live.Resolver {
	return &live.Resolver{
		Backends:       []store.Store{stored},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
		Refresh: func(_ context.Context, ts auth.TokenSet, _ store.Store) (auth.TokenSet, error) {
			*refreshes++

			return ts, nil
		},
		Broker: func(context.Context, map[string]string) (auth.Credential, error) {
			*brokers++

			return auth.Credential{}, nil
		},
	}
}

func vmPairEnvs() map[string]string {
	return map[string]string{auth.EnvVMToken: "vm-token", auth.EnvVMTokenURL: "https://instance-manager.services.bitrise.io/vm/tokens"}
}

func TestHydrate_StoredLoginBeatsTheVMToken(t *testing.T) {
	stored := &hydrateStore{present: true, ts: auth.TokenSet{AuthToken: "tok", WorkspaceID: "ws", RefreshToken: "rt"}}
	var refreshes, brokers int

	hydrateWith(t.Context(), vmPairEnvs(), hydrateResolver(stored, &refreshes, &brokers), log.NewLogger())

	assert.Equal(t, 1, refreshes, "a stored login must still be refreshed on a VM")
	assert.Zero(t, brokers)
}

func TestHydrate_SkipsWhenOnlyTheVMTokenIsAvailable(t *testing.T) {
	var refreshes, brokers int

	hydrateWith(t.Context(), vmPairEnvs(), hydrateResolver(&hydrateStore{}, &refreshes, &brokers), log.NewLogger())

	assert.Zero(t, refreshes)
	assert.Zero(t, brokers, "hydrate must not broker a token on every command")
}
