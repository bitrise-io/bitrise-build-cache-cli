// Package bazelcredhelper implements the EngFlow / Bazel Credential Helper
// JSON protocol as consumed by `bazel --credential_helper=<path>`.
//
// Bazel spawns the helper per request, writes a single-line JSON object
// {"uri": "..."} on stdin, and expects a JSON object on stdout containing at
// least {"headers": {...}}. Exit 0 on success; non-zero exit fails the RPC.
package bazelcredhelper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

// GetCredentialsRequest is the wire shape Bazel writes to the helper's stdin.
// URI is populated with the target endpoint (e.g. https://<host>) — we ignore
// it because our headers are endpoint-agnostic, matching the bare-header
// behavior of the pre-helper `--remote_header`/`--bes_header` lines.
type GetCredentialsRequest struct {
	URI string `json:"uri,omitempty"`
}

// GetCredentialsResponse is the wire shape Bazel reads from the helper's
// stdout. `Headers` values are string arrays per the spec even when a header
// has a single value.
type GetCredentialsResponse struct {
	Headers map[string][]string `json:"headers"`
}

// Run reads one credential-helper request from `in`, resolves the auth token
// via the same precedence chain as the rest of the CLI (envs → keychain →
// multiplatform config; envs are hydrated by the root PersistentPreRun), and
// writes a JSON response with an `authorization: Bearer <token>` header to
// `out`. Returns an error on unparseable input or missing credentials.
func Run(in io.Reader, out io.Writer, envs map[string]string) error {
	// The request body is optional in practice but we accept and discard it
	// so a malformed payload surfaces as an error rather than silent success.
	var req GetCredentialsRequest
	dec := json.NewDecoder(in)
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode credential-helper request: %w", err)
	}

	cfg, _, err := configcommon.ResolveAuthConfig(envs)
	if err != nil {
		return fmt.Errorf("resolve auth config: %w", err)
	}

	resp := GetCredentialsResponse{
		Headers: map[string][]string{
			"authorization": {"Bearer " + cfg.AuthToken},
		},
	}

	if err := json.NewEncoder(out).Encode(resp); err != nil {
		return fmt.Errorf("encode credential-helper response: %w", err)
	}

	return nil
}
