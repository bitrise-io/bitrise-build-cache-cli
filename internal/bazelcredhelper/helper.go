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
)

// Budget leaves headroom under Bazel's --credential_helper_timeout (10s default).
const Budget = 8 * time.Second

// caps Bazel's credential cache TTL.
const maxCacheHint = 5 * time.Minute

// URI is ignored: our headers are endpoint-agnostic, matching the bare-header
// behavior of the pre-helper `--remote_header`/`--bes_header` lines.
type GetCredentialsRequest struct {
	URI string `json:"uri,omitempty"`
}

// Headers values are string arrays per the spec even for a single value.
type GetCredentialsResponse struct {
	Headers map[string][]string `json:"headers"`
	// RFC 3339 cache hint — Bazel reuses the credential until then. Capped at
	// maxCacheHint so a project-marker edit is picked up within a bounded window.
	Expires string `json:"expires,omitempty"`
}

type Credential struct {
	Token  string
	Expiry time.Time
}

type Resolver func(ctx context.Context) (Credential, error)

// resolveRepoURL may be nil, in which case no repository header is emitted.
func Run(ctx context.Context, in io.Reader, out io.Writer, resolve Resolver, resolveRepoURL RepoURLResolver) error {
	// Decoded and discarded, so a malformed payload is an error not a silent pass.
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
		Expires: capCacheHint(cred.Expiry, time.Now()).UTC().Format(time.RFC3339),
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

// capCacheHint returns whichever comes first: the credential's own expiry, or
// now + maxCacheHint. A zero credExpiry means "unknown lifetime" and takes the
// cap. A credExpiry already in the past also takes the cap — Bazel would treat
// a past expiry as a soft cache miss, spawning the helper per RPC.
func capCacheHint(credExpiry, now time.Time) time.Time {
	ceiling := now.Add(maxCacheHint)
	if credExpiry.IsZero() || !credExpiry.After(now) || credExpiry.After(ceiling) {
		return ceiling
	}

	return credExpiry
}
