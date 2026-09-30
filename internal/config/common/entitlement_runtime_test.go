//go:build unit

package common_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func entitlementLogger() log.Logger {
	l := &mocks.Logger{}
	for _, m := range []string{"Infof", "Debugf", "Errorf", "Warnf", "TInfof", "TDebugf"} {
		l.On(m, mock.Anything).Return()
		l.On(m, mock.Anything, mock.Anything).Return()
	}

	return l
}

func isolateHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(common.EnvSkipEntitlementCheck, "")
	t.Setenv(common.EnvEntitlementOverride, "")
}

// The gate must never close because something failed. Unknown is the answer to
// every error, and only an explicit negative stands a build down.
func TestSkipForEntitlementAtBuildTime_FailsOpen(t *testing.T) {
	isolateHome(t)

	skip := common.SkipForEntitlementAtBuildTime(
		t.Context(), common.BuildToolGradle, "http://127.0.0.1:1", auth.Credential{WorkspaceID: "ws"},
		common.CacheConfigMetadata{BitriseBuildID: "build-1"}, entitlementLogger())

	assert.False(t, skip, "an unreachable website must not stand the build down")
}

func TestSkipForEntitlementAtBuildTime_OverrideStandsTheBuildDown(t *testing.T) {
	isolateHome(t)
	t.Setenv(common.EnvEntitlementOverride, "none")

	skip := common.SkipForEntitlementAtBuildTime(
		t.Context(), common.BuildToolGradle, "http://127.0.0.1:1", auth.Credential{WorkspaceID: "ws"},
		common.CacheConfigMetadata{BitriseBuildID: "build-1"}, entitlementLogger())

	assert.True(t, skip)
}

func TestResolveEntitlement_OverrideActiveDoesNotSkip(t *testing.T) {
	isolateHome(t)
	t.Setenv(common.EnvEntitlementOverride, "active")

	state := common.ResolveEntitlement(
		t.Context(), common.BuildToolGradle, "http://127.0.0.1:1", auth.Credential{WorkspaceID: "ws"},
		common.CacheConfigMetadata{BitriseBuildID: "build-1"}, entitlementLogger())

	assert.Equal(t, common.EntitlementActive, state)
}

// The TMP bypass has to reach the build-time gate too. If it only covered
// activation, every lite e2e build would be gated by an endpoint that does not
// exist yet.
func TestResolveEntitlement_TmpBypassApplies(t *testing.T) {
	isolateHome(t)
	t.Setenv(common.EnvSkipEntitlementCheck, "true")
	t.Setenv(common.EnvEntitlementOverride, "none")

	state := common.ResolveEntitlement(
		t.Context(), common.BuildToolGradle, "http://127.0.0.1:1", auth.Credential{WorkspaceID: "ws"},
		common.CacheConfigMetadata{BitriseBuildID: "build-1"}, entitlementLogger())

	assert.Equal(t, common.EntitlementUnknown, state, "the bypass must win over every other source")
}

// One build makes many CLI calls. The answer is recorded so they cost one
// request, and the record is scoped so the next build does not inherit it.
//
// Seeded directly rather than through a resolve: the override short-circuits
// before the record is read, so using it here would assert nothing.
func TestResolveEntitlement_RecordIsReusedByTheSameBuildOnly(t *testing.T) {
	isolateHome(t)
	seedEntitlementRecord(t, common.EntitlementRecord{
		State: common.EntitlementNone, BuildID: "build-1",
	})

	logger := entitlementLogger()
	// Unreachable, so anything other than the record answers Unknown.
	const unreachable = "http://127.0.0.1:1"

	same := common.ResolveEntitlement(t.Context(), common.BuildToolGradle, unreachable,
		auth.Credential{WorkspaceID: "ws"},
		common.CacheConfigMetadata{BitriseBuildID: "build-1"}, logger)
	assert.Equal(t, common.EntitlementNone, same, "the build's own record must be reused")

	other := common.ResolveEntitlement(t.Context(), common.BuildToolGradle, unreachable,
		auth.Credential{WorkspaceID: "ws"},
		common.CacheConfigMetadata{BitriseBuildID: "build-2"}, logger)
	assert.Equal(t, common.EntitlementUnknown, other,
		"another build's record must not decide this build")
}

// An unscoped record — written off CI, or before scoping existed — is never
// reused, or one machine's answer would outlive every build on it.
func TestResolveEntitlement_UnscopedRecordIsNotReused(t *testing.T) {
	isolateHome(t)
	seedEntitlementRecord(t, common.EntitlementRecord{State: common.EntitlementNone})

	state := common.ResolveEntitlement(t.Context(), common.BuildToolGradle, "http://127.0.0.1:1",
		auth.Credential{WorkspaceID: "ws"},
		common.CacheConfigMetadata{BitriseBuildID: "build-1"}, entitlementLogger())

	assert.Equal(t, common.EntitlementUnknown, state)
}

func seedEntitlementRecord(t *testing.T, record common.EntitlementRecord) {
	t.Helper()

	p, err := paths.Default()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(p.EntitlementRecordFile(common.BuildToolGradle)), 0o755))

	body, err := json.Marshal(record)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(p.EntitlementRecordFile(common.BuildToolGradle), body, 0o600))
}
