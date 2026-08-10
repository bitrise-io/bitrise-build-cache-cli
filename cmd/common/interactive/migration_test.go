//go:build unit

package interactive

import (
	"errors"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

func captureLogger() (log.Logger, *strings.Builder) {
	var out strings.Builder

	return log.NewLogger(log.WithOutput(&out)), &out
}

// Migrating has to move the credential, not copy it: leaving the file copy keeps
// a plaintext token on disk even though the keychain now holds it.
func TestPersistWizardCredentials_MigrationClearsTheFileCopy(t *testing.T) {
	for _, origin := range []authpkg.Origin{
		{Backend: authpkg.BackendFile, Provenance: authpkg.ProvenanceManual},
		{Backend: authpkg.BackendFile, Provenance: authpkg.ProvenanceStatic},
	} {
		kc := &stubKeychain{}
		cleared := false
		logger, out := captureLogger()

		persistWizardCredentialsTo(logger, kc, func() error { cleared = true; return nil },
			wizardAuth{Origin: origin},
			wizardCredentials{WorkspaceID: "ws-1", AuthToken: "tok-1"})

		assert.True(t, cleared, "origin=%v: the file copy must be removed", origin)
		assert.Equal(t, "tok-1", kc.saved.AuthToken, "origin=%v: the keychain must hold it", origin)
		assert.Contains(t, out.String(), "Moved credentials")
	}
}

// If the copy can't be removed, say so and where — silently leaving a plaintext
// token behind is the thing worth shouting about.
func TestPersistWizardCredentials_ReportsAFailedCleanup(t *testing.T) {
	kc := &stubKeychain{}
	logger, out := captureLogger()

	persistWizardCredentialsTo(logger, kc, func() error { return errors.New("permission denied") },
		wizardAuth{Origin: authpkg.Origin{Backend: authpkg.BackendFile}},
		wizardCredentials{WorkspaceID: "ws-1", AuthToken: "tok-1"})

	assert.Contains(t, out.String(), "could not remove the config-file copy")
	assert.Contains(t, out.String(), "still on disk")
	assert.Contains(t, out.String(), "auth clear --storage=file")
}

// A failed keychain write must not then delete the only copy there is.
func TestPersistWizardCredentials_KeepsTheFileWhenTheKeychainFails(t *testing.T) {
	kc := &failingKeychain{err: errors.New("keychain locked")}
	cleared := false
	logger, out := captureLogger()

	persistWizardCredentialsTo(logger, kc, func() error { cleared = true; return nil },
		wizardAuth{Origin: authpkg.Origin{Backend: authpkg.BackendFile}},
		wizardCredentials{WorkspaceID: "ws-1", AuthToken: "tok-1"})

	require.False(t, cleared, "the file is the only remaining copy — it must survive")
	assert.Contains(t, out.String(), "Could not save credentials to the OS keychain")
}

// The keychain source has no file copy to remove.
func TestPersistWizardCredentials_KeychainSourceDoesNotTouchTheFile(t *testing.T) {
	kc := &stubKeychain{}
	cleared := false
	logger, _ := captureLogger()

	persistWizardCredentialsTo(logger, kc, func() error { cleared = true; return nil },
		wizardAuth{Origin: authpkg.Origin{Backend: authpkg.BackendKeychain}},
		wizardCredentials{WorkspaceID: "ws-1", AuthToken: "tok-1", Username: "alice", StoredUsername: "alice"})

	assert.False(t, cleared)
}

type failingKeychain struct{ err error }

func (f *failingKeychain) Backend() authpkg.Backend        { return authpkg.BackendKeychain }
func (f *failingKeychain) Clear() error                    { return nil }
func (f *failingKeychain) Load() (authpkg.TokenSet, error) { return authpkg.TokenSet{}, f.err }
func (f *failingKeychain) Save(authpkg.TokenSet) error     { return f.err }
