// Package bazelcredhelper implements the EngFlow / Bazel Credential Helper
// JSON protocol as consumed by `bazel --credential_helper=<path>`.
//
// Bazel spawns the helper per request, writes a single-line JSON object
// {"uri": "..."} on stdin, and expects a JSON object on stdout containing at
// least {"headers": {...}}. Exit 0 on success; non-zero exit fails the RPC.
//
// Nothing on this path may write to stdout — it is the protocol channel.
package bazelcredhelper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Budget bounds a single helper invocation, leaving headroom under Bazel's
// --credential_helper_timeout (10s default) for the spawn and the store read.
const Budget = 8 * time.Second

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
	// Expires is the spec's optional RFC 3339 cache hint: Bazel caches the
	// credential until then rather than re-spawning the helper. Omitted when the
	// lifetime is unknown, which falls back to --credential_helper_cache_duration.
	Expires string `json:"expires,omitempty"`
}

// Credential is what a Resolver hands the protocol layer. A zero Expiry omits
// the cache hint.
type Credential struct {
	Token  string
	Expiry time.Time
}

// Resolver produces a live credential; ctx bounds any network refresh it does.
type Resolver func(ctx context.Context) (Credential, error)

// Run reads one credential-helper request from `in`, resolves a credential via
// `resolve`, and writes the JSON response to `out`. Returns an error on
// unparseable input or when no credential could be resolved at all.
func Run(ctx context.Context, in io.Reader, out io.Writer, resolve Resolver) error {
	// The request body is optional in practice but we accept and discard it
	// so a malformed payload surfaces as an error rather than silent success.
	var req GetCredentialsRequest
	dec := json.NewDecoder(in)
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode credential-helper request: %w", err)
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

	if err := json.NewEncoder(out).Encode(resp); err != nil {
		return fmt.Errorf("encode credential-helper response: %w", err)
	}

	return nil
}
