package common

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/retryhttp"
	"github.com/hashicorp/go-retryablehttp"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// A workspace can have Build Cache and still want it only in selected workflows.
// For an automatic activation, the website decides per app and workflow; entitlement
// stays a separate, workspace-wide check.

// autoActivationEndpointShipped records whether the website endpoint exists yet.
// Until it does the check is skipped and the org allowlist is the only guard.
const autoActivationEndpointShipped = false

// autoActivationLive is the test seam over autoActivationEndpointShipped.
var autoActivationLive = autoActivationEndpointShipped //nolint:gochecknoglobals

// Provisional, like the endpoint: same shape as the entitlement path.
const autoActivationPath = "%s/build-cache/%s/auto_activation"

const autoActivationTimeout = 5 * time.Second

type autoActivationResponse struct {
	Enabled bool `json:"enabled"`
}

// AppIdentity names the app and workflow asking, with the parameter names of the
// benchmark-status call.
type AppIdentity struct {
	BitriseAppSlug       string
	BitriseWorkflowName  string
	ExternalAppID        string
	ExternalWorkflowName string
}

// NewAppIdentity reads the app and workflow from the build's environment: the
// Bitrise app slug and triggered workflow on Bitrise CI, the repository or project
// and job on another CI provider.
func NewAppIdentity(envs map[string]string, osProxy utils.OsProxy) AppIdentity {
	provider := DetectCIProvider(envs, osProxy)
	if provider == CIProviderBitrise {
		return AppIdentity{
			BitriseAppSlug:      envs["BITRISE_APP_SLUG"],
			BitriseWorkflowName: envs["BITRISE_TRIGGERED_WORKFLOW_ID"],
		}
	}

	externalAppID, _, externalWorkflowName := detectExternalIDs(provider, envs)

	return AppIdentity{ExternalAppID: externalAppID, ExternalWorkflowName: externalWorkflowName}
}

func (a AppIdentity) query() string {
	params := url.Values{}
	set := func(key, value string) {
		if value != "" {
			params.Set(key, value)
		}
	}
	set("app_slug", a.BitriseAppSlug)
	set("workflow_name", a.BitriseWorkflowName)
	set("external_app_id", a.ExternalAppID)
	set("external_workflow_name", a.ExternalWorkflowName)

	return params.Encode()
}

// AutoActivationEnabled asks whether this app and workflow may be activated
// automatically. It fails closed once the endpoint exists: an automatic activation
// is a convenience, and an answer we could not get must not switch caching on for a
// workflow whose owner limited it. Before then it allows everything.
func AutoActivationEnabled(ctx context.Context, baseURL string, cred auth.Credential, app AppIdentity, logger log.Logger) bool {
	if !autoActivationLive {
		return true
	}

	client := retryhttp.NewClient(logger)
	client.RetryMax = 1
	client.HTTPClient.Timeout = autoActivationTimeout

	ctx, cancel := context.WithTimeout(ctx, autoActivationTimeout)
	defer cancel()

	requestURL := fmt.Sprintf(autoActivationPath, baseURL, cred.WorkspaceID)
	if query := app.query(); query != "" {
		requestURL += "?" + query
	}
	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		logger.Debugf("Could not build the auto-activation request: %s", err)

		return false
	}
	req.Header.Set("Authorization", "Bearer "+cred.Token)

	resp, err := client.Do(req)
	if err != nil {
		logger.Debugf("Could not reach the auto-activation endpoint: %s", err)

		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Debugf("Auto-activation endpoint returned %d", resp.StatusCode)

		return false
	}

	var body autoActivationResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		logger.Debugf("Could not decode the auto-activation response: %s", err)

		return false
	}

	return body.Enabled
}

// AutoActivationChecker is the seam that lets command tests force an answer.
var AutoActivationChecker = AutoActivationEnabled //nolint:gochecknoglobals
