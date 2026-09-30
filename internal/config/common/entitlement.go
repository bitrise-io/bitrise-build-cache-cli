package common

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/retryhttp"
	"github.com/hashicorp/go-retryablehttp"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

// A workspace with no Build Cache trial or subscription should never be
// activated: the build cannot use the cache, and activating anyway spends an
// analytics invocation saying so. Telling the user where to start a trial is
// more useful than a build's worth of rejected RPCs.

// EnvSkipEntitlementCheck disables the gate entirely.
//
// TEMPORARY, ACI-5515. The endpoint below does not exist yet, so without this
// every activation would refuse to run. Set in this repo's own bitrise.yml for
// the PoC.
//
// Delete this constant, the branch that reads it, and the bitrise.yml entries
// the moment entitlementEndpointShipped flips to true.
// TestEntitlementBypass_MustBeRemovedOnceTheEndpointShips fails until you do.
const EnvSkipEntitlementCheck = "BITRISE_BUILD_CACHE_TMP_SKIP_ENTITLEMENT_CHECK" //nolint:gosec // env-var key, not a credential

// entitlementEndpointShipped records whether the website endpoint this gate
// calls actually exists yet. Flip it when it does — a test then fails until the
// temporary bypass above is removed, so the two cannot drift apart.
const entitlementEndpointShipped = false

// entitlementPath is provisional and will almost certainly change when the
// endpoint is designed for real; it is written to match the shape of the
// benchmark-phase call next to it.
const entitlementPath = "%s/build-cache/%s/entitlement"

const entitlementTimeout = 5 * time.Second

// MsgNoEntitlement is what a user without Build Cache sees instead of an
// activation. It names the action, not the failure.
const MsgNoEntitlement = "Bitrise Build Cache is not enabled for this workspace. " +
	"Start a free trial at https://app.bitrise.io/build-cache — skipping activation."

type entitlementResponse struct {
	Active bool `json:"active"`
}

// EntitlementState is deliberately three-valued: "we could not tell" must not
// be confused with "no", or one API blip disables caching for everyone.
type EntitlementState int

const (
	EntitlementUnknown EntitlementState = iota
	EntitlementActive
	EntitlementNone
)

// CheckEntitlement asks whether the workspace has Build Cache. Any failure is
// Unknown, never None: this gate exists to stop pointless activations, not to
// become a new way for the website being down to break everyone's builds.
func CheckEntitlement(ctx context.Context, baseURL string, cred auth.Credential, logger log.Logger) EntitlementState {
	if !entitlementEndpointShipped {
		return EntitlementUnknown
	}

	if cred.WorkspaceID == "" {
		return EntitlementUnknown
	}

	client := retryhttp.NewClient(logger)
	client.RetryMax = 1
	client.HTTPClient.Timeout = entitlementTimeout

	ctx, cancel := context.WithTimeout(ctx, entitlementTimeout)
	defer cancel()

	url := fmt.Sprintf(entitlementPath, baseURL, cred.WorkspaceID)
	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		logger.Debugf("Could not build the entitlement request: %s", err)

		return EntitlementUnknown
	}
	req.Header.Set("Authorization", "Bearer "+cred.Token)

	resp, err := client.Do(req)
	if err != nil {
		logger.Debugf("Could not reach the entitlement endpoint: %s", err)

		return EntitlementUnknown
	}
	defer resp.Body.Close()

	// A 402/403 is the backend saying "no", which is an answer.
	if resp.StatusCode == http.StatusPaymentRequired || resp.StatusCode == http.StatusForbidden {
		return EntitlementNone
	}
	if resp.StatusCode != http.StatusOK {
		logger.Debugf("Entitlement endpoint returned %d", resp.StatusCode)

		return EntitlementUnknown
	}

	var body entitlementResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		logger.Debugf("Could not decode the entitlement response: %s", err)

		return EntitlementUnknown
	}

	if body.Active {
		return EntitlementActive
	}

	return EntitlementNone
}

// SkipActivationForEntitlement reports whether activation should stop before
// doing anything, and prints the reason when it should.
//
// Lite passes an empty credential: warmup has no workspace to ask about, so the
// question moves to build time along with everything else.
func SkipActivationForEntitlement(ctx context.Context, baseURL string, cred auth.Credential, logger log.Logger) bool {
	if os.Getenv(EnvSkipEntitlementCheck) != "" {
		logger.Warnf("TEMPORARY: the Build Cache entitlement check is bypassed via %s (ACI-5515). "+
			"Remove it once the entitlement endpoint ships.", EnvSkipEntitlementCheck)

		return false
	}

	if CheckEntitlement(ctx, baseURL, cred, logger) != EntitlementNone {
		return false
	}

	logger.Warnf("%s", MsgNoEntitlement)

	return true
}
