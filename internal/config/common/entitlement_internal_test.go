//go:build unit

package common

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

func credFor(workspaceID string) auth.Credential {
	return auth.Credential{Token: "tok", WorkspaceID: workspaceID}
}

func testLogger() log.Logger { return log.NewLogger() }

// The bypass and the endpoint have to be removed and shipped together. Flipping
// entitlementEndpointShipped without deleting the bypass would leave a live
// env var that silently disables a paying-customer gate, so this fails the
// moment the flip happens and tells the next person what to delete.
func TestEntitlementBypass_MustBeRemovedOnceTheEndpointShips(t *testing.T) {
	if entitlementEndpointShipped {
		t.Fatalf(
			"the entitlement endpoint has shipped — now delete %s, the branch in "+
				"SkipActivationForEntitlement that reads it, and this test (ACI-5515)",
			EnvSkipEntitlementCheck,
		)
	}
}

// Until the endpoint exists the gate must be inert: a check that cannot be made
// is not a "no", or every activation everywhere would refuse to run.
func TestCheckEntitlement_IsUnknownWhileTheEndpointDoesNotExist(t *testing.T) {
	require.False(t, endpointLive, "this test describes the pre-ship state")

	srv := serve(t, http.StatusPaymentRequired, "")

	assert.Equal(t, EntitlementUnknown, CheckEntitlement(t.Context(), srv.URL, credFor("ws-1"), testLogger()))
}

func serve(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/build-cache/ws-1/entitlement", r.URL.Path)
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return srv
}

func shipped(t *testing.T) {
	t.Helper()

	endpointLive = true
	t.Cleanup(func() { endpointLive = entitlementEndpointShipped })
}

func TestCheckEntitlement_Answers(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   EntitlementState
	}{
		{"402 is a no", http.StatusPaymentRequired, "", EntitlementNone},
		{"403 is a no", http.StatusForbidden, "", EntitlementNone},
		{"inactive is a no", http.StatusOK, `{"active":false}`, EntitlementNone},
		{"active", http.StatusOK, `{"active":true}`, EntitlementActive},
		{"a server error is not a no", http.StatusInternalServerError, "", EntitlementUnknown},
		{"a not found is not a no", http.StatusNotFound, "", EntitlementUnknown},
		{"an undecodable answer is not a no", http.StatusOK, "<html>", EntitlementUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shipped(t)
			srv := serve(t, tt.status, tt.body)

			assert.Equal(t, tt.want, CheckEntitlement(t.Context(), srv.URL, credFor("ws-1"), testLogger()))
		})
	}
}

func TestCheckEntitlement_UnreachableIsNotANo(t *testing.T) {
	shipped(t)
	srv := serve(t, http.StatusOK, "")
	srv.Close()

	assert.Equal(t, EntitlementUnknown, CheckEntitlement(t.Context(), srv.URL, credFor("ws-1"), testLogger()))
}

func TestCheckEntitlement_NoWorkspaceIsUnknown(t *testing.T) {
	shipped(t)
	srv := serve(t, http.StatusPaymentRequired, "")

	assert.Equal(t, EntitlementUnknown, CheckEntitlement(t.Context(), srv.URL, credFor(""), testLogger()))
}

func TestSkipActivationForEntitlement_SkipsOnlyOnANo(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		skip   bool
	}{
		{"no", http.StatusPaymentRequired, "", true},
		{"active", http.StatusOK, `{"active":true}`, false},
		{"unknown", http.StatusInternalServerError, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shipped(t)
			t.Setenv(EnvSkipEntitlementCheck, "")
			srv := serve(t, tt.status, tt.body)

			assert.Equal(t, tt.skip, SkipActivationForEntitlement(t.Context(), srv.URL, credFor("ws-1"), testLogger()))
		})
	}
}

func TestSkipActivationForEntitlement_TheBypassSuppressesAGenuineNo(t *testing.T) {
	shipped(t)
	srv := serve(t, http.StatusPaymentRequired, "")

	t.Setenv(EnvSkipEntitlementCheck, "")
	require.True(t, SkipActivationForEntitlement(t.Context(), srv.URL, credFor("ws-1"), testLogger()), "precondition: no bypass means skip")

	t.Setenv(EnvSkipEntitlementCheck, "true")
	assert.False(t, SkipActivationForEntitlement(t.Context(), srv.URL, credFor("ws-1"), testLogger()))
}
