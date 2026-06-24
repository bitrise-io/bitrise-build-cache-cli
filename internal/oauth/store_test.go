//go:build unit

package oauth

import (
	"testing"
	"time"

	keyring "github.com/zalando/go-keyring"
)

// resetKeychain swaps in a fresh in-memory keychain so each test starts with no
// stored credential and never touches the real OS keychain.
func resetKeychain(t *testing.T) {
	t.Helper()
	keyring.MockInit()
}

func TestCredentialsStore_RoundTrip(t *testing.T) {
	resetKeychain(t)

	want := Credentials{
		PAT:          "bitpat_x",
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
	if got.PAT != want.PAT || got.JWT != want.JWT || got.RefreshToken != want.RefreshToken || got.WorkspaceID != want.WorkspaceID {
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
	if got.PAT != "" || got.IsOAuthManaged() {
		t.Fatalf("expected zero credentials, got %+v", got)
	}
}

func TestCredentialsStore_SaveRejectsEmptyPAT(t *testing.T) {
	resetKeychain(t)

	if err := Save(Credentials{WorkspaceID: "x"}); err == nil {
		t.Fatal("Save with empty PAT should fail")
	}
}

func TestCredentialsStore_Clear(t *testing.T) {
	resetKeychain(t)

	if err := Save(Credentials{PAT: "p", RefreshToken: "r", WorkspaceID: "w"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if got, _ := Load(); got.PAT != "" {
		t.Fatalf("expected cleared, got %+v", got)
	}
	if err := Clear(); err != nil {
		t.Fatalf("Clear should be idempotent: %v", err)
	}
}
