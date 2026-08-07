//go:build unit

package oauth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

// resetKeychain swaps in a fresh in-memory keychain so each test starts with no
// stored credential and never touches the real OS keychain. HOME is redirected
// too, so the file store and the refresh lock stay inside the test's temp dir.
func resetKeychain(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
}

func TestCredentialsStore_RoundTrip(t *testing.T) {
	resetKeychain(t)

	want := auth.TokenSet{
		AuthToken:    "bitpat_x",
		PATExpiry:    time.Now().Add(time.Hour).UTC().Truncate(time.Second),
		JWT:          "header.payload.sig",
		JWTExpiry:    time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second),
		RefreshToken: "refresh-1",
		WorkspaceID:  "acme",
	}
	if err := saveForTest(t, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := loadForTest()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AuthToken != want.AuthToken || got.JWT != want.JWT || got.RefreshToken != want.RefreshToken || got.WorkspaceID != want.WorkspaceID {
		t.Fatalf("string fields mismatch: got %+v", got)
	}
	if !got.PATExpiry.Equal(want.PATExpiry) || !got.JWTExpiry.Equal(want.JWTExpiry) {
		t.Fatalf("expiry round-trip mismatch: got %+v want %+v", got, want)
	}
	if !got.IsOAuthManaged() {
		t.Fatal("a credential with a refresh token should be OAuth-managed")
	}
}

func TestCredentialsStore_MissingFileIsZero(t *testing.T) {
	resetKeychain(t)

	got, err := loadForTest()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AuthToken != "" || got.IsOAuthManaged() {
		t.Fatalf("expected zero credentials, got %+v", got)
	}
}

func TestCredentialsStore_SaveRejectsEmptyPAT(t *testing.T) {
	resetKeychain(t)

	if err := saveForTest(t, auth.TokenSet{WorkspaceID: "x"}); err == nil {
		t.Fatal("Save with empty PAT should fail")
	}
}

func TestCredentialsStore_Clear(t *testing.T) {
	resetKeychain(t)

	if err := saveForTest(t, auth.TokenSet{AuthToken: "p", RefreshToken: "r", WorkspaceID: "w"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := ClearFrom(store.NewKeychain(), store.NewFile()); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if got, _ := loadForTest(); got.AuthToken != "" {
		t.Fatalf("expected cleared, got %+v", got)
	}
	if err := ClearFrom(store.NewKeychain(), store.NewFile()); err != nil {
		t.Fatalf("Clear should be idempotent: %v", err)
	}
}

// One TokenSet for storage and for OAuth means the save is faithful: whatever the
// caller hands over is what lands, with no narrowing conversion to drop fields.
// That is what makes the refresh flow safe — it loads a record and saves it back.
// It does NOT by itself protect `auth login`, which builds a fresh record; see
// TestStoredUsername and the merge in loginAndStore.
func TestSaveToWithFallback_IsFaithfulToTheRecord(t *testing.T) {
	resetKeychain(t)

	target := store.NewKeychain()
	require.NoError(t, target.Save(auth.TokenSet{AuthToken: "old", WorkspaceID: "ws", Username: "bob"}))

	existing, err := target.Load()
	require.NoError(t, err)
	existing.AuthToken = "bitpat_new"
	existing.RefreshToken = "refresh-1"
	_, err = SaveToWithFallback(target, existing, false)
	require.NoError(t, err)

	got, err := target.Load()
	require.NoError(t, err)
	assert.Equal(t, "bob", got.Username)
	assert.Equal(t, "refresh-1", got.RefreshToken)
}

// saveForTest is the keychain write the exported convenience wrappers used to
// provide; production code always names its backend.
func saveForTest(t *testing.T, c auth.TokenSet) error {
	t.Helper()
	_, err := SaveToWithFallback(store.NewKeychain(), c, false)

	return err
}

func loadForTest() (auth.TokenSet, error) {
	c, _, err := LoadWithSource()

	return c, err
}

// ensureFresh is the load-then-refresh entry point production no longer needs:
// live.Resolve has already read the store by the time it refreshes. The refresh
// tests still drive the whole path from disk.
func ensureFresh(c Config, ctx context.Context) (auth.TokenSet, error) { //nolint:revive // ctx after the receiver-ish arg keeps the call sites readable
	creds, src, err := LoadWithSource()
	if err != nil {
		return auth.TokenSet{}, err
	}

	return c.EnsureFreshFrom(ctx, creds, src)
}
