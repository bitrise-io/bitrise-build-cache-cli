//go:build unit

package oauth

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// go-keyring's in-memory mock is not safe for concurrent use, so this
// concurrency test goes through the file store instead.
func useFileStore(t *testing.T) {
	t.Helper()
	keyring.MockInitWithError(keyring.ErrNotFound)
	t.Setenv("HOME", t.TempDir())
}

// Without the lock every caller spends the same rotated refresh token.
func TestEnsureFresh_ConcurrentCallers_RefreshOnce(t *testing.T) {
	useFileStore(t)
	require.NoError(t, saveTo(store.NewFile(), auth.TokenSet{
		AuthToken: "old-pat", PATExpiry: time.Now().Add(-time.Minute),
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
		results []auth.TokenSet
	)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			creds, err := ensureFresh(cfg, t.Context())
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
		assert.Equal(t, m.pat, got.AuthToken)
		assert.Equal(t, "ws", got.WorkspaceID)
	}

	tokenCalls, exchangeCalls := m.counts()
	assert.Equal(t, 1, tokenCalls, "the refresh token must be spent exactly once")
	assert.Equal(t, 1, exchangeCalls, "one JWT→PAT exchange for the whole fleet")
}

// The lock is only for the refresh; a valid PAT must stay a pure read.
func TestEnsureFresh_ValidPAT_TakesNoLock(t *testing.T) {
	useFileStore(t)
	require.NoError(t, saveTo(store.NewFile(), auth.TokenSet{
		AuthToken: "still-good", PATExpiry: time.Now().Add(time.Hour),
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

	assert.Equal(t, "still-good", got.AuthToken)
	assert.Less(t, time.Since(start), refreshLockWait, "the valid-PAT fast path must not wait on the lock")
}

func TestEnsureFresh_FileStore_ExpiredPAT_Refreshes(t *testing.T) {
	useFileStore(t)
	require.NoError(t, saveTo(store.NewFile(), auth.TokenSet{
		AuthToken: "old-pat", PATExpiry: time.Now().Add(-time.Minute),
		JWT: "old-jwt", JWTExpiry: time.Now().Add(-time.Minute),
		RefreshToken: "refresh-0", WorkspaceID: "ws",
	}))

	m := newOAuthMock()
	defer m.close()

	got, err := m.config().EnsureFresh(t.Context())
	require.NoError(t, err)
	assert.Equal(t, m.pat, got.AuthToken)

	// The refreshed credential must be persisted back into the file store.
	reloaded, err := loadForTest()
	require.NoError(t, err)
	assert.Equal(t, m.pat, reloaded.AuthToken)
}

// Giving up on the lock is exactly the case where another process is refreshing,
// so the credential read before the wait is the one whose refresh token they have
// already rotated away. Spending it again would break the login for good.
func TestEnsureFresh_LockWaitFailed_ReloadsBeforeSpendingTheRefreshToken(t *testing.T) {
	useFileStore(t)
	require.NoError(t, saveTo(store.NewFile(), auth.TokenSet{
		AuthToken: "old-pat", PATExpiry: time.Now().Add(-time.Minute),
		JWT: "old-jwt", JWTExpiry: time.Now().Add(-time.Minute),
		RefreshToken: "refresh-0", WorkspaceID: "ws",
	}))

	// Hold the real lock for the whole test, so the wait can only time out. Separate
	// open file descriptions conflict even inside one process, which is what lets the
	// test stand in for the other helper.
	p, err := paths.Default()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(p.AuthRefreshLockFile()), 0o700))
	holder := flock.New(p.AuthRefreshLockFile())
	held, err := holder.TryLock()
	require.NoError(t, err)
	require.True(t, held, "the test needs to be the one holding it")
	defer func() { _ = holder.Unlock() }()

	original := refreshLockWait
	refreshLockWait = 300 * time.Millisecond
	defer func() { refreshLockWait = original }()

	// Stand in for the process that holds the lock finishing its refresh. Joined
	// before the test returns, so it cannot write into the next test's HOME.
	saved := make(chan struct{})
	go func() {
		defer close(saved)
		time.Sleep(50 * time.Millisecond)
		_ = saveTo(store.NewFile(), auth.TokenSet{
			AuthToken: "refreshed-by-the-other-process", PATExpiry: time.Now().Add(time.Hour),
			JWT: "new-jwt", JWTExpiry: time.Now().Add(time.Hour),
			RefreshToken: "refresh-1", WorkspaceID: "ws",
		})
	}()
	defer func() { <-saved }()

	m := newOAuthMock()
	defer m.close()

	creds, err := m.config().EnsureFresh(t.Context())

	require.NoError(t, err)
	assert.Equal(t, "refreshed-by-the-other-process", creds.AuthToken, "the reload must win over the pre-wait credential")

	m.mu.Lock()
	defer m.mu.Unlock()
	assert.Zero(t, m.tokenCalls, "the rotated refresh token must not be spent a second time")
	assert.Zero(t, m.exchangeCalls)
}
