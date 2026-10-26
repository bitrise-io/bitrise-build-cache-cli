// Package bazelcredhelper implements the EngFlow / Bazel Credential Helper
// JSON protocol as consumed by `bazel --credential_helper=<path>`.
//
// Bazel spawns the helper per request, writes a single-line JSON object
// {"uri": "..."} on stdin, and expects a JSON object on stdout containing at
// least {"headers": {...}}. Exit 0 on success; non-zero exit fails the RPC.
// Nothing on this path may write to stdout — it is the protocol channel.
package bazelcredhelper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Budget leaves headroom under Bazel's --credential_helper_timeout (10s default).
const Budget = 8 * time.Second

// URI is ignored: our headers are endpoint-agnostic, matching the bare-header
// behavior of the pre-helper `--remote_header`/`--bes_header` lines.
type GetCredentialsRequest struct {
	URI string `json:"uri,omitempty"`
}

// Headers values are string arrays per the spec even for a single value.
type GetCredentialsResponse struct {
	Headers map[string][]string `json:"headers"`
	// RFC 3339 cache hint — Bazel reuses the credential until then. Omitted when
	// the lifetime is unknown, falling back to --credential_helper_cache_duration.
	Expires string `json:"expires,omitempty"`
}

type Credential struct {
	Token  string
	Expiry time.Time
}

type Resolver func(ctx context.Context) (Credential, error)

// resolveRepoURL may be nil, in which case no repository header is emitted.
//
// Opt-in gating fails SILENT (empty headers) via machineconfig.ProjectOptedOut:
// an opt-in gate exists to keep traffic off a project the user did not opt in,
// so a transient Getwd error or a malformed .bitrise-build-cache.json must
// not silently sign a request anyway. Empty headers let Bazel skip auth or
// surface a clean 401; fail-OPEN here would defeat the gate.
func Run(ctx context.Context, in io.Reader, out io.Writer, resolve Resolver, resolveRepoURL RepoURLResolver) error {
	// Decoded and discarded, so a malformed payload is an error not a silent pass.
	var req GetCredentialsRequest
	dec := json.NewDecoder(in)
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode credential-helper request: %w", err)
	}

	if machineconfig.ProjectOptedOut(utils.DefaultOsProxy{}, nil) {
		if err := json.NewEncoder(out).Encode(GetCredentialsResponse{Headers: map[string][]string{}}); err != nil {
			return fmt.Errorf("encode empty credential-helper response: %w", err)
		}

		return nil
	}

	cred, err := resolve(ctx)
	if err != nil {
		// Bazel shows only the helper's stderr, once per failing RPC, and the
		// wrapped error names env vars alone even though the keychain and the
		// config file were checked too — so say where to go from here.
		return fmt.Errorf("no Bitrise Build Cache credentials in the environment, the OS keychain or the config file (%w); "+
			"run `bitrise-build-cache doctor --fix --interactive` to sign in or store a token", err)
	}

	resp := GetCredentialsResponse{
		Headers: map[string][]string{
			"authorization": {"Bearer " + cred.Token},
		},
	}
	if !cred.Expiry.IsZero() {
		resp.Expires = cred.Expiry.UTC().Format(time.RFC3339)
	}

	if resolveRepoURL != nil {
		if repoURL := resolveRepoURL(ctx); repoURL != "" {
			resp.Headers[repositoryURLHeader] = []string{repoURL}
		}
	}

	if err := json.NewEncoder(out).Encode(resp); err != nil {
		return fmt.Errorf("encode credential-helper response: %w", err)
	}

	return nil
}
