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
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

func captureLogger() (log.Logger, *strings.Builder) {
	var out strings.Builder

	return log.NewLogger(log.WithOutput(&out)), &out
}

type fakeSaveFn struct {
	keychainErr error
	fileErr     error

	calls []fakeSaveCall
}

type fakeSaveCall struct {
	target        store.Store
	creds         authpkg.TokenSet
	allowFallback bool
}

func (f *fakeSaveFn) fn() saveWithFallbackFn {
	return func(target store.Store, creds authpkg.TokenSet, allowFallback bool) (store.SaveResult, error) {
		f.calls = append(f.calls, fakeSaveCall{target: target, creds: creds, allowFallback: allowFallback})

		if f.keychainErr == nil {
			return store.SaveResult{Origin: creds.Origin(target.Backend())}, nil
		}
		if !allowFallback || target.Backend() != authpkg.BackendKeychain {
			return store.SaveResult{Origin: creds.Origin(target.Backend())}, f.keychainErr
		}
		if f.fileErr != nil {
			return store.SaveResult{Origin: creds.Origin(authpkg.BackendFile)}, f.fileErr
		}

		return store.SaveResult{Origin: creds.Origin(authpkg.BackendFile), KeychainErr: f.keychainErr}, nil
	}
}

func TestPersistWizardCredentials_MigrationMovesToTheKeychain(t *testing.T) {
	for _, origin := range []authpkg.Origin{
		{Backend: authpkg.BackendFile, Provenance: authpkg.ProvenanceManual},
		{Backend: authpkg.BackendFile, Provenance: authpkg.ProvenanceStatic},
	} {
		kc := &stubKeychain{}
		fake := &fakeSaveFn{}
		logger, out := captureLogger()

		persistWizardCredentialsTo(logger, kc, fake.fn(),
			wizardAuth{Origin: origin},
			wizardCredentials{WorkspaceID: "ws-1", AuthToken: "tok-1"})

		require.Len(t, fake.calls, 1, "origin=%v", origin)
		call := fake.calls[0]
		assert.Same(t, kc, call.target, "origin=%v", origin)
		assert.Equal(t, "tok-1", call.creds.AuthToken, "origin=%v", origin)
		assert.Equal(t, "ws-1", call.creds.WorkspaceID, "origin=%v", origin)
		assert.True(t, call.allowFallback, "origin=%v", origin)
		assert.Contains(t, out.String(), "Moved credentials")
	}
}

// Guards against a bare struct literal replacing `merged := auth.Stored` and
// dropping the OAuth refresh token.
func TestPersistWizardCredentials_FileSourceFallbackPreservesRefreshToken(t *testing.T) {
	kc := &stubKeychain{}
	fake := &fakeSaveFn{keychainErr: errors.New("dbus-launch missing")}
	logger, _ := captureLogger()

	persistWizardCredentialsTo(logger, kc, fake.fn(),
		wizardAuth{
			Origin: authpkg.Origin{Backend: authpkg.BackendFile},
			Stored: authpkg.TokenSet{RefreshToken: "refresh-1"},
		},
		wizardCredentials{WorkspaceID: "ws-1", AuthToken: "tok-1"})

	require.Len(t, fake.calls, 1)
	assert.Equal(t, "refresh-1", fake.calls[0].creds.RefreshToken)
}

func TestPersistWizardCredentials_FileSourceFallsBackToFile(t *testing.T) {
	kc := &stubKeychain{}
	fake := &fakeSaveFn{keychainErr: errors.New("dbus-launch missing")}
	logger, out := captureLogger()

	persistWizardCredentialsTo(logger, kc, fake.fn(),
		wizardAuth{Origin: authpkg.Origin{Backend: authpkg.BackendFile}},
		wizardCredentials{WorkspaceID: "ws-1", AuthToken: "tok-1"})

	require.Len(t, fake.calls, 1)
	logs := out.String()
	assert.Contains(t, logs, "Keychain unavailable")
	assert.Contains(t, logs, "config file")
	assert.NotContains(t, logs, "for this run only")
}

func TestPersistWizardCredentials_FileSourceReportsTotalFailure(t *testing.T) {
	kc := &stubKeychain{}
	fake := &fakeSaveFn{
		keychainErr: errors.New("keychain locked"),
		fileErr:     errors.New("permission denied"),
	}
	logger, out := captureLogger()

	persistWizardCredentialsTo(logger, kc, fake.fn(),
		wizardAuth{Origin: authpkg.Origin{Backend: authpkg.BackendFile}},
		wizardCredentials{WorkspaceID: "ws-1", AuthToken: "tok-1"})

	assert.Contains(t, out.String(), "for this run only")
}

func TestPersistWizardCredentials_ManualPromptFallsBackToFile(t *testing.T) {
	kc := &stubKeychain{}
	fake := &fakeSaveFn{keychainErr: errors.New("no secret-service")}
	logger, out := captureLogger()

	persistWizardCredentialsTo(logger, kc, fake.fn(),
		wizardAuth{Origin: authpkg.Origin{Backend: authpkg.BackendNone}},
		wizardCredentials{WorkspaceID: "ws-1", AuthToken: "tok-1"})

	require.Len(t, fake.calls, 1)
	call := fake.calls[0]
	assert.Same(t, kc, call.target)
	assert.True(t, call.allowFallback)
	logs := out.String()
	assert.Contains(t, logs, "config file")
	assert.NotContains(t, logs, "for this run only")
}

func TestPersistWizardCredentials_EnvImportFallsBackToFile(t *testing.T) {
	kc := &stubKeychain{}
	fake := &fakeSaveFn{keychainErr: errors.New("no secret-service")}
	logger, out := captureLogger()

	persistWizardCredentialsTo(logger, kc, fake.fn(),
		wizardAuth{Origin: authpkg.Origin{Backend: authpkg.BackendEnv}},
		wizardCredentials{WorkspaceID: "ws-1", AuthToken: "tok-1"})

	require.Len(t, fake.calls, 1)
	logs := out.String()
	assert.Contains(t, logs, "config file")
	assert.Contains(t, logs, "remove them from your shell rc files")
	assert.NotContains(t, logs, "for this run only")
}

func TestPersistWizardCredentials_KeychainSourceDoesNotResave(t *testing.T) {
	kc := &stubKeychain{}
	fake := &fakeSaveFn{}
	logger, _ := captureLogger()

	persistWizardCredentialsTo(logger, kc, fake.fn(),
		wizardAuth{Origin: authpkg.Origin{Backend: authpkg.BackendKeychain}},
		wizardCredentials{WorkspaceID: "ws-1", AuthToken: "tok-1", Username: "alice", StoredUsername: "alice"})

	assert.Empty(t, fake.calls)
	assert.Empty(t, kc.saved.AuthToken)
}
