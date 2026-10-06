//go:build unit

package ccache

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	ccacheanalytics "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/ccache/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/common/childstats"
)

func TestStorageHelper_refreshAuth(t *testing.T) {
	stale := authpkg.Credential{Token: "stale", WorkspaceID: "ws"}
	failing := func(context.Context) (authpkg.Credential, authpkg.Origin, error) {
		return authpkg.Credential{}, authpkg.Origin{}, errors.New("offline")
	}

	t.Run("replaces the config credential", func(t *testing.T) {
		fresh := authpkg.Credential{Token: "fresh", WorkspaceID: "ws"}
		origin := authpkg.Origin{Backend: authpkg.BackendJWT, Provenance: authpkg.ProvenanceBrokered}
		h := &StorageHelper{
			config: ccacheconfig.Config{AuthConfig: stale},
			logger: log.NewLogger(),
			resolveCredential: func(context.Context) (authpkg.Credential, authpkg.Origin, error) {
				return fresh, origin, nil
			},
		}

		assert.True(t, h.refreshAuth(context.Background()))
		assert.Equal(t, fresh, h.config.AuthConfig)
		assert.Equal(t, origin, h.config.AuthOrigin)
	})

	t.Run("keeps the config credential on failure", func(t *testing.T) {
		h := &StorageHelper{config: ccacheconfig.Config{AuthConfig: stale}, logger: log.NewLogger(), resolveCredential: failing}

		assert.True(t, h.refreshAuth(context.Background()))
		assert.Equal(t, stale, h.config.AuthConfig)
	})

	t.Run("reports no credential to send", func(t *testing.T) {
		h := &StorageHelper{logger: log.NewLogger(), resolveCredential: failing}

		assert.False(t, h.refreshAuth(context.Background()))
	})
}

func TestStorageHelper_report_writesTheLedgerWithoutACredential(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := &StorageHelper{
		logger: log.NewLogger(),
		resolveCredential: func(context.Context) (authpkg.Credential, authpkg.Origin, error) {
			return authpkg.Credential{}, authpkg.Origin{}, errors.New("offline")
		},
	}

	h.report(context.Background(), "child", "parent", ccacheanalytics.CcacheStats{CacheHit: 1}, 0, 0, nil)

	_, err := os.Stat(childstats.LedgerPath("parent", "child"))
	require.NoError(t, err)
}
