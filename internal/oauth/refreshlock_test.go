//go:build unit

package oauth

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

// useFileStore points every credential read/write at the config file in a temp
// HOME. go-keyring's in-memory mock is not safe for concurrent use, and the file
// store is the backend a keychain-less host (Linux, containers, CI) uses anyway.
func useFileStore(t *testing.T) {
	t.Helper()
	keyring.MockInitWithError(keyring.ErrNotFound)
	t.Setenv("HOME", t.TempDir())
}

// Bazel spawns the credential helper N-way parallel. WorkOS rotates the refresh
// token on every grant, so without the lock several callers would each spend the
// same one and the last writer would persist a token the others already burned.
func TestEnsureFresh_ConcurrentCallers_RefreshOnce(t *testing.T) {
	useFileStore(t)
	require.NoError(t, SaveTo(store.NewFile(), Credentials{
		PAT: "old-pat", PATExpiry: time.Now().Add(-time.Minute),
		JWT: "old-jwt", JWTExpiry: time.Now().Add(-time.Minute),
		RefreshToken: "refresh-0", WorkspaceID: "ws",
	}))

	m := newOAuthMock()
	defer m.close()
	cfg := m.config()

	const callers = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []Credentials
	)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			creds, err := cfg.EnsureFresh(t.Context())
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				results = append(results, creds)
			}
		}()
	}
	wg.Wait()

	require.Len(t, results, callers, "every caller should get a credential")
	for _, got := range results {
		assert.Equal(t, m.pat, got.PAT)
		assert.Equal(t, "ws", got.WorkspaceID)
	}

	tokenCalls, exchangeCalls := m.counts()
	assert.Equal(t, 1, tokenCalls, "the refresh token must be spent exactly once")
	assert.Equal(t, 1, exchangeCalls, "one JWT→PAT exchange for the whole fleet")
}

// The lock is only for the refresh; a valid PAT must stay a pure read.
func TestEnsureFresh_ValidPAT_TakesNoLock(t *testing.T) {
	useFileStore(t)
	require.NoError(t, SaveTo(store.NewFile(), Credentials{
		PAT: "still-good", PATExpiry: time.Now().Add(time.Hour),
		RefreshToken: "r", WorkspaceID: "ws",
	}))

	m := newOAuthMock()
	defer m.close()

	release, err := acquireRefreshLock(t.Context())
	require.NoError(t, err)
	defer release()

	// Would block for refreshLockWait if the fast path touched the lock.
	start := time.Now()
	got, err := m.config().EnsureFresh(t.Context())
	require.NoError(t, err)

	assert.Equal(t, "still-good", got.PAT)
	assert.Less(t, time.Since(start), refreshLockWait, "the valid-PAT fast path must not wait on the lock")
}

// A file-stored OAuth login must refresh too — this is the keychain-less host
// the ExpiryAwareResolver used to skip entirely.
func TestEnsureFresh_FileStore_ExpiredPAT_Refreshes(t *testing.T) {
	useFileStore(t)
	require.NoError(t, SaveTo(store.NewFile(), Credentials{
		PAT: "old-pat", PATExpiry: time.Now().Add(-time.Minute),
		JWT: "old-jwt", JWTExpiry: time.Now().Add(-time.Minute),
		RefreshToken: "refresh-0", WorkspaceID: "ws",
	}))

	m := newOAuthMock()
	defer m.close()

	got, err := m.config().EnsureFresh(t.Context())
	require.NoError(t, err)
	assert.Equal(t, m.pat, got.PAT)

	// The refreshed credential must be persisted back into the file store.
	reloaded, err := Load()
	require.NoError(t, err)
	assert.Equal(t, m.pat, reloaded.PAT)
}
