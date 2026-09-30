package common

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// Lite activation cannot ask whether the workspace has Build Cache: at preboot
// there is no workspace. The build asks instead, and every build-time entry
// point the CLI owns consults the answer before doing anything billable.
//
// This matters more for analytics than for the cache. An unentitled cache call
// is rejected and every tool stands down on its own; an analytics invocation is
// accepted and recorded, so nothing downstream declines. Without this check a
// lite-activated VM reports invocations for workspaces that never had Build
// Cache.

// EnvEntitlementOverride pins the answer for a single build. e2e workflows use
// it to exercise both sides of the gate without a real unentitled workspace.
// Values: "active" or "none"; anything else is ignored.
const EnvEntitlementOverride = "BITRISE_BUILD_CACHE_ENTITLEMENT_OVERRIDE"

// EntitlementRecord is the build's cached answer. Scoped to a build because a
// persistent runner reuses the machine, and one build's "none" must not decide
// for every build that follows it.
type EntitlementRecord struct {
	State   EntitlementState `json:"state"`
	BuildID string           `json:"buildId,omitempty"`
}

// ResolveEntitlement returns this build's entitlement, asking the API at most
// once per build however many CLI invocations the build makes.
//
// Unknown is the answer to every failure, and callers must treat it as "carry
// on". A gate that closes when the website is unreachable would be a worse
// outage than the one it prevents.
func ResolveEntitlement(
	ctx context.Context,
	buildTool string,
	baseURL string,
	cred auth.Credential,
	metadata CacheConfigMetadata,
	logger log.Logger,
) EntitlementState {
	if os.Getenv(EnvSkipEntitlementCheck) != "" {
		return EntitlementUnknown
	}

	if state, ok := entitlementOverride(); ok {
		logger.Debugf("Entitlement from env var: %v", state)

		return state
	}

	buildID := BenchmarkBuildID(metadata)

	if record, found := readEntitlementRecord(buildTool, logger); found &&
		buildID != "" && record.BuildID == buildID {
		return record.State
	}

	state := CheckEntitlement(ctx, buildTool, baseURL, cred, logger)

	// Unknown is recorded too. A build makes many CLI calls, and an unreachable
	// website would otherwise cost a retrying request on every one of them.
	writeEntitlementRecord(buildTool, EntitlementRecord{State: state, BuildID: buildID}, logger)

	return state
}

// SkipForEntitlementAtBuildTime reports whether a build-time caller should stand
// down, and says so once. Only an explicit negative skips.
func SkipForEntitlementAtBuildTime(
	ctx context.Context,
	buildTool string,
	baseURL string,
	cred auth.Credential,
	metadata CacheConfigMetadata,
	logger log.Logger,
) bool {
	if ResolveEntitlement(ctx, buildTool, baseURL, cred, metadata, logger) != EntitlementNone {
		return false
	}

	logger.Warnf("%s", MsgNoEntitlementAtBuildTime)

	return true
}

func entitlementOverride() (EntitlementState, bool) {
	switch os.Getenv(EnvEntitlementOverride) {
	case "active":
		return EntitlementActive, true
	case "none":
		return EntitlementNone, true
	default:
		return EntitlementUnknown, false
	}
}

func entitlementRecordPath(buildTool string) (string, error) {
	p, err := paths.Default()
	if err != nil {
		return "", err //nolint:wrapcheck // paths.Default already names itself
	}

	return p.EntitlementRecordFile(buildTool), nil
}

func readEntitlementRecord(buildTool string, logger log.Logger) (EntitlementRecord, bool) {
	path, err := entitlementRecordPath(buildTool)
	if err != nil {
		logger.Debugf("Failed to resolve the entitlement record path: %v", err)

		return EntitlementRecord{}, false
	}

	body, err := os.ReadFile(path) //nolint:gosec // path derived from home + constant
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logger.Debugf("Failed to read the entitlement record: %v", err)
		}

		return EntitlementRecord{}, false
	}

	var record EntitlementRecord
	if err := json.Unmarshal(body, &record); err != nil {
		logger.Debugf("Failed to decode the entitlement record: %v", err)

		return EntitlementRecord{}, false
	}

	return record, true
}

// writeEntitlementRecord renames a temp file over the target so a concurrent
// reader never sees a half-written record.
func writeEntitlementRecord(buildTool string, record EntitlementRecord, logger log.Logger) {
	path, err := entitlementRecordPath(buildTool)
	if err != nil {
		logger.Debugf("Failed to resolve the entitlement record path: %v", err)

		return
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec,mnd // state dir, not a secret
		logger.Debugf("Failed to create the entitlement record dir: %v", err)

		return
	}

	body, err := json.Marshal(record)
	if err != nil {
		logger.Debugf("Failed to encode the entitlement record: %v", err)

		return
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		logger.Debugf("Failed to write the entitlement record: %v", err)

		return
	}

	if err := os.Rename(tmp, path); err != nil {
		logger.Debugf("Failed to install the entitlement record: %v", err)
		_ = os.Remove(tmp)
	}
}
