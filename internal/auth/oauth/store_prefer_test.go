//go:build unit

package oauth

import (
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

type fakeStore struct {
	creds   auth.TokenSet
	present bool
	backend auth.Backend
}

func (f *fakeStore) Backend() auth.Backend { return f.backend }

func (f *fakeStore) Load() (auth.TokenSet, error) {
	if !f.present {
		return auth.TokenSet{}, store.ErrNotFound
	}

	return f.creds, nil
}

func (f *fakeStore) Save(c auth.TokenSet) error { f.creds, f.present = c, true; return nil }
func (f *fakeStore) Clear() error               { f.present = false; return nil }

// A manual PAT in an earlier backend must not hide a login in a later one, or
// logout reports nothing to remove and refresh never finds the refresh token.
func TestLoadFrom_PrefersTheOAuthCredential(t *testing.T) {
	manual := &fakeStore{backend: auth.BackendKeychain, present: true, creds: auth.TokenSet{
		AuthToken: "manual-pat", WorkspaceID: "ws-1",
	}}
	login := &fakeStore{backend: auth.BackendFile, present: true, creds: auth.TokenSet{
		AuthToken: "oauth-pat", WorkspaceID: "ws-1", RefreshToken: "refresh-1",
	}}

	creds, src, err := loadFrom(manual, login)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if !creds.IsOAuthManaged() {
		t.Fatal("expected the OAuth credential to win over the manual one")
	}
	if src.Backend() != auth.BackendFile {
		t.Fatalf("source = %v, want the store the login is in", src.Backend())
	}
}

// With no login anywhere, the first credential found still wins.
func TestLoadFrom_FallsBackToTheFirstManualCredential(t *testing.T) {
	first := &fakeStore{backend: auth.BackendKeychain, present: true, creds: auth.TokenSet{
		AuthToken: "manual-1", WorkspaceID: "ws-1",
	}}
	second := &fakeStore{backend: auth.BackendFile, present: true, creds: auth.TokenSet{
		AuthToken: "manual-2", WorkspaceID: "ws-2",
	}}

	creds, src, err := loadFrom(first, second)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if creds.AuthToken != "manual-1" || src.Backend() != auth.BackendKeychain {
		t.Fatalf("got %q from %v, want manual-1 from the keychain", creds.AuthToken, src.Backend())
	}
}

func TestLoadFrom_NothingStored(t *testing.T) {
	creds, src, err := loadFrom(&fakeStore{backend: auth.BackendKeychain}, &fakeStore{backend: auth.BackendFile})
	if err != nil || src != nil || creds.AuthToken != "" {
		t.Fatalf("expected an empty result, got %q / %v / %v", creds.AuthToken, src, err)
	}
}
