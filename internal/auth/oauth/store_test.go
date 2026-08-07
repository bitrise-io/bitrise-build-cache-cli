//go:build unit

package oauth

import (
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
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
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

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AuthToken != "" || got.IsOAuthManaged() {
		t.Fatalf("expected zero credentials, got %+v", got)
	}
}

func TestCredentialsStore_SaveRejectsEmptyPAT(t *testing.T) {
	resetKeychain(t)

	if err := Save(auth.TokenSet{WorkspaceID: "x"}); err == nil {
		t.Fatal("Save with empty PAT should fail")
	}
}

func TestCredentialsStore_Clear(t *testing.T) {
	resetKeychain(t)

	if err := Save(auth.TokenSet{AuthToken: "p", RefreshToken: "r", WorkspaceID: "w"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if got, _ := Load(); got.AuthToken != "" {
		t.Fatalf("expected cleared, got %+v", got)
	}
	if err := Clear(); err != nil {
		t.Fatalf("Clear should be idempotent: %v", err)
	}
}

// One TokenSet for storage and for OAuth means a sign-in cannot silently drop a
// display name written by `auth username`: there is no narrowing conversion left
// for it to fall through.
func TestSaveToWithFallback_KeepsTheDisplayName(t *testing.T) {
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
