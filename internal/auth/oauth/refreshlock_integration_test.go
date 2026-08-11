//go:build unit

package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

// Goroutines share a pid and a lock table, so they cannot exercise what the refresh
// lock is for: separate CLI processes, spawned per Bazel RPC, refreshing one
// credential — including one that dies mid-refresh. These re-exec the test binary
// and put real processes through EnsureFresh.
//
// The invariant under test is not "they did not overlap", it is the consequence:
// the identity provider rotates the refresh token on every grant and rejects a
// reused one, so a double spend leaves the login permanently broken. Every
// assertion here is about the credential surviving.

const (
	envRole   = "REFRESHLOCK_CHILD_ROLE"
	envHome   = "REFRESHLOCK_CHILD_HOME"
	envIssuer = "REFRESHLOCK_CHILD_ISSUER"
	envLog    = "REFRESHLOCK_CHILD_LOG"
	envHoldMS = "REFRESHLOCK_CHILD_HOLD_MS"
	envEpoch  = "REFRESHLOCK_CHILD_EPOCH"

	roleRefresh = "refresh"
	roleCrash   = "crash"
	// Refreshes and then keeps the lock, so a waiter has something to give up on.
	roleHoldAfterRefresh = "hold-after-refresh"
)

func TestMain(m *testing.M) {
	if role := os.Getenv(envRole); role != "" {
		os.Exit(runLockChild(role))
	}

	os.Exit(m.Run())
}

// runLockChild refreshes through the production entry point.
func runLockChild(role string) int {
	// paths.Default and the credential store both resolve from HOME.
	_ = os.Setenv("HOME", os.Getenv(envHome))
	issuer := os.Getenv(envIssuer)
	holdMS, _ := strconv.Atoi(os.Getenv(envHoldMS))

	cfg := NewConfigFromEnv(map[string]string{
		"BITRISE_OAUTH_ISSUER":        issuer,
		"BITRISE_OIDC_TOKEN_ENDPOINT": issuer + "/oidc/token",
	})

	if role == roleHoldAfterRefresh {
		return holdAfterRefresh(cfg, holdMS)
	}

	if role == roleCrash {
		// Die holding the lock, between the grant and the save: the worst moment,
		// because the token this process spent is already invalid at the IdP.
		release, err := acquireRefreshLock(context.Background())
		if err != nil {
			logEvent(fmt.Sprintf("BLOCKED   pid=%-6d %v", os.Getpid(), err))

			return 1
		}
		defer func() { _ = release() }()

		if _, err := cfg.refreshJWT(context.Background(), storedRefreshToken()); err != nil {
			logEvent(fmt.Sprintf("GRANT_ERR pid=%-6d %v", os.Getpid(), err))

			return 1
		}
		logEvent(fmt.Sprintf("SPENT     pid=%-6d rotated the token, dying before the save", os.Getpid()))
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
	}

	start := time.Now()
	creds, err := ensureFresh(cfg, context.Background())
	if err != nil {
		logEvent(fmt.Sprintf("FAILED    pid=%-6d after=%s %v", os.Getpid(), time.Since(start).Round(time.Millisecond), err))

		return 1
	}
	logEvent(fmt.Sprintf("FRESH     pid=%-6d after=%-8s pat=%s", os.Getpid(), time.Since(start).Round(time.Millisecond), creds.AuthToken))

	time.Sleep(time.Duration(holdMS) * time.Millisecond)

	return 0
}

// holdAfterRefresh stands in for the other helper: it rotates the token, saves,
// and keeps the lock. Not EnsureFresh, because that releases on return and a
// waiter would then never have to give up — which is how the first version of this
// test managed to pass against a broken reload.
func holdAfterRefresh(cfg Config, holdMS int) int {
	release, err := acquireRefreshLock(context.Background())
	if err != nil {
		logEvent(fmt.Sprintf("BLOCKED   pid=%-6d %v", os.Getpid(), err))

		return 1
	}
	defer func() { _ = release() }()

	logEvent(fmt.Sprintf("HOLDING   pid=%-6d", os.Getpid()))
	// Long enough for the waiter to load the stale credential and start waiting.
	time.Sleep(300 * time.Millisecond)

	creds, err := loadForTest()
	if err != nil {
		return 1
	}
	refreshed, err := cfg.refreshJWT(context.Background(), creds.RefreshToken)
	if err != nil {
		logEvent(fmt.Sprintf("GRANT_ERR pid=%-6d %v", os.Getpid(), err))

		return 1
	}
	creds.JWT, creds.JWTExpiry = refreshed.AccessToken, time.Now().Add(time.Hour)
	creds.RefreshToken = refreshed.RefreshToken
	pat, expiry, err := cfg.exchangeJWTForPAT(context.Background(), creds.JWT)
	if err != nil {
		return 1
	}
	creds.AuthToken, creds.PATExpiry = pat, expiry
	if err := saveTo(store.NewFile(), creds); err != nil {
		return 1
	}
	logEvent(fmt.Sprintf("ROTATED   pid=%-6d pat=%s still holding the lock", os.Getpid(), pat))

	time.Sleep(time.Duration(holdMS) * time.Millisecond)

	return 0
}

func storedRefreshToken() string {
	creds, err := loadForTest()
	if err != nil {
		return ""
	}

	return creds.RefreshToken
}

func logEvent(line string) {
	f, err := os.OpenFile(os.Getenv(envLog), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()

	epoch, _ := strconv.ParseInt(os.Getenv(envEpoch), 10, 64)
	_, _ = f.WriteString(fmt.Sprintf("t=%-8s %s\n", time.Since(time.Unix(0, epoch)).Round(time.Millisecond), line))
}

// rotatingIDP behaves like WorkOS: one use per refresh token, and a reused token is
// rejected outright. Reuse is what breaks a login, so it is counted, not tolerated.
type rotatingIDP struct {
	mu       sync.Mutex
	valid    map[string]bool
	grants   int
	reuse    int
	issued   int
	server   *httptest.Server
	patCalls int
}

func newRotatingIDP(t *testing.T, seedToken string) *rotatingIDP {
	t.Helper()

	idp := &rotatingIDP{valid: map[string]bool{seedToken: true}}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", idp.handleRefresh)
	mux.HandleFunc("/oidc/token", idp.handleExchange)
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)

	return idp
}

func (i *rotatingIDP) handleRefresh(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	presented := r.FormValue("refresh_token")

	i.mu.Lock()
	defer i.mu.Unlock()

	if !i.valid[presented] {
		i.reuse++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)

		return
	}

	// One use only, exactly like the real thing.
	delete(i.valid, presented)
	i.issued++
	i.grants++
	rotated := fmt.Sprintf("refresh-%d", i.issued)
	i.valid[rotated] = true

	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  makeJWT(time.Now().Add(time.Hour).Unix()),
		"refresh_token": rotated,
		"expires_in":    3600,
		"token_type":    "Bearer",
	})
}

func (i *rotatingIDP) handleExchange(w http.ResponseWriter, _ *http.Request) {
	i.mu.Lock()
	i.patCalls++
	pat := fmt.Sprintf("pat-%d", i.patCalls)
	i.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": pat,
		"expires_in":   3600,
		"token_type":   "Bearer",
	})
}

func (i *rotatingIDP) stats() (grants, reuse int) {
	i.mu.Lock()
	defer i.mu.Unlock()

	return i.grants, i.reuse
}

func (i *rotatingIDP) accepts(token string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()

	return i.valid[token]
}

type lockEnv struct {
	home  string
	log   string
	idp   *rotatingIDP
	epoch time.Time
}

// newLockEnv seeds an expired login, so every child has to refresh.
func newLockEnv(t *testing.T) lockEnv {
	t.Helper()
	useFileStore(t)
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	require.NoError(t, saveTo(store.NewFile(), auth.TokenSet{
		AuthToken: "pat-seed", PATExpiry: time.Now().Add(-time.Minute),
		JWT: "jwt-seed", JWTExpiry: time.Now().Add(-time.Minute),
		RefreshToken: "refresh-seed", WorkspaceID: "ws",
	}))

	return lockEnv{
		home:  dir,
		log:   filepath.Join(dir, "events.log"),
		idp:   newRotatingIDP(t, "refresh-seed"),
		epoch: time.Now(),
	}
}

func (e lockEnv) start(t *testing.T, role string, holdMS int) *exec.Cmd {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), os.Args[0]) //nolint:gosec // re-exec of this test binary
	cmd.Env = append(os.Environ(),
		envRole+"="+role,
		envHome+"="+e.home,
		envIssuer+"="+e.idp.server.URL,
		envLog+"="+e.log,
		envHoldMS+"="+strconv.Itoa(holdMS),
		envEpoch+"="+strconv.FormatInt(e.epoch.UnixNano(), 10),
	)
	cmd.Cancel = func() error { return cmd.Process.Kill() }
	require.NoError(t, cmd.Start())

	return cmd
}

func (e lockEnv) events(t *testing.T) []string {
	t.Helper()

	content, err := os.ReadFile(e.log)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)

	return strings.Split(strings.TrimSpace(string(content)), "\n")
}

func (e lockEnv) count(t *testing.T, kind string) int {
	t.Helper()

	n := 0
	for _, line := range e.events(t) {
		if fields := strings.Fields(line); len(fields) > 1 && fields[1] == kind {
			n++
		}
	}

	return n
}

func (e lockEnv) dump(t *testing.T, title string) {
	t.Helper()
	t.Logf("%s\n\n%s\n", title, strings.Join(e.events(t), "\n"))
}

// servedPATs collects what each process handed back, so they can be compared.
func (e lockEnv) servedPATs(t *testing.T) []string {
	t.Helper()

	var pats []string
	for _, line := range e.events(t) {
		if _, pat, found := strings.Cut(line, "pat="); found {
			pats = append(pats, pat)
		}
	}

	return pats
}

// Eight helpers, one expired credential, an IdP that rejects a reused token.
func TestIntegration_RefreshLock_ParallelHelpersDoNotBurnTheRefreshToken(t *testing.T) {
	const helpers = 8
	env := newLockEnv(t)

	cmds := make([]*exec.Cmd, 0, helpers)
	for range helpers {
		cmds = append(cmds, env.start(t, roleRefresh, 0))
	}
	for i, cmd := range cmds {
		require.NoError(t, cmd.Wait(), "helper %d should end up with a live credential:\n%s", i, strings.Join(env.events(t), "\n"))
	}

	env.dump(t, "eight credential helpers refreshing one expired login")

	grants, reuse := env.idp.stats()
	assert.Zero(t, reuse, "a rotated refresh token was spent twice — this is what permanently breaks a login")
	assert.Equal(t, 1, grants, "one refresh should serve every helper")
	assert.Equal(t, helpers, env.count(t, "FRESH"), "every helper should get a credential")

	pats := env.servedPATs(t)
	require.Len(t, pats, helpers)
	for _, pat := range pats {
		assert.Equal(t, pats[0], pat, "every helper should be handed the same refreshed token")
	}

	stored, err := loadForTest()
	require.NoError(t, err)
	assert.True(t, env.idp.accepts(stored.RefreshToken), "the stored refresh token must still be one the IdP will honour")
}

// A helper killed after the grant but before the save leaves the stored token
// already spent. The next process must not present it again.
func TestIntegration_RefreshLock_TokenSpentByADeadHolderIsNotPresentedAgain(t *testing.T) {
	env := newLockEnv(t)

	crashed := env.start(t, roleCrash, 0)
	require.Error(t, crashed.Wait(), "the child should die from SIGKILL")
	require.Equal(t, 1, env.count(t, "SPENT"))

	grants, reuse := env.idp.stats()
	require.Equal(t, 1, grants, "the dead holder did spend a token")
	require.Zero(t, reuse)

	survivor := env.start(t, roleRefresh, 0)
	err := survivor.Wait()

	env.dump(t, "a helper killed between the grant and the save")

	// The stored token is genuinely dead, so the honest outcome is a login prompt —
	// never a silent double spend, and never a hang on the dead holder's lock.
	_, reuseAfter := env.idp.stats()
	assert.Equal(t, 1, reuseAfter, "presenting the spent token once is unavoidable; it must not be more than that")
	if err != nil {
		assert.Equal(t, 1, env.count(t, "FAILED"), "and the failure should say the session needs a new login")
	}
}

// Losing the wait is the case where someone else is refreshing, so the waiter has
// to re-read rather than present the token it loaded before waiting — that token
// has already been rotated away.
func TestIntegration_RefreshLock_WaiterThatGivesUpDoesNotSpendTheStaleToken(t *testing.T) {
	env := newLockEnv(t)

	holder := env.start(t, roleHoldAfterRefresh, 3000)
	require.Eventually(t, func() bool {
		return env.count(t, "HOLDING") == 1
	}, 5*time.Second, 5*time.Millisecond, "waiting for the holder to take the lock")

	original := refreshLockWait
	refreshLockWait = 900 * time.Millisecond // outlasts the holder's 300ms pre-refresh pause
	defer func() { refreshLockWait = original }()

	cfg := NewConfigFromEnv(map[string]string{
		"BITRISE_OAUTH_ISSUER":        env.idp.server.URL,
		"BITRISE_OIDC_TOKEN_ENDPOINT": env.idp.server.URL + "/oidc/token",
	})

	creds, err := ensureFresh(cfg, t.Context())

	env.dump(t, "a waiter giving up on a lock still held by the process that refreshed")

	require.NoError(t, err, "the waiter should serve the credential the holder refreshed")
	require.Equal(t, 1, env.count(t, "ROTATED"), "the holder must have rotated the token while holding the lock")

	grants, reuse := env.idp.stats()
	assert.Zero(t, reuse, "the waiter presented a refresh token that had already been spent")
	assert.Equal(t, 1, grants, "and it must not have refreshed again")
	assert.NotEqual(t, "pat-seed", creds.AuthToken, "it should have picked up the refreshed credential")

	stored, err := loadForTest()
	require.NoError(t, err)
	assert.True(t, env.idp.accepts(stored.RefreshToken), "the login must still be usable afterwards")

	require.NoError(t, holder.Wait())
}
