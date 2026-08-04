package bazelcredhelper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/filelock"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/oauth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// expiresLead pulls the cache hint in from the real PAT expiry so the next spawn
// lands inside oauth's refresh window. It MUST stay below oauth.RefreshSkew: with
// a larger lead Bazel would re-invoke while EnsureFresh's fast path still calls
// the PAT valid, get the same already-past expires back, and re-spawn the helper
// on every RPC until the PAT genuinely expired.
const expiresLead = oauth.RefreshSkew / 2

// staleCacheHint bounds how long Bazel caches a credential we could not refresh,
// so a repaired login is picked up without waiting out the 30m default.
const staleCacheHint = time.Minute

// warnCooldown rate-limits the stale-credential warning across the many helper
// processes a single build spawns.
const warnCooldown = 10 * time.Minute

// NewResolver returns the production Resolver. warn receives at most one
// stale-credential line per cooldown; it must not be stdout.
func NewResolver(envs map[string]string, warn io.Writer) Resolver {
	// Logger stays nil: go-utils' logger writes to stdout, which is the protocol channel.
	oauthCfg := oauth.NewConfigFromEnv(envs)

	return newResolver(envs, warn, oauthCfg.EnsureFresh)
}

func newResolver(envs map[string]string, warn io.Writer, ensureFresh func(context.Context) (oauth.Credentials, error)) Resolver {
	return func(ctx context.Context) (Credential, error) {
		cfg, source, err := configcommon.ResolveAuthConfig(envs)
		if err != nil {
			return Credential{}, fmt.Errorf("resolve stored credentials: %w", err)
		}

		// Env vars, the CI JWT and the legacy authConfig carry no refresh token.
		if !source.StoreManaged() {
			return Credential{Token: cfg.AuthToken}, nil
		}

		creds, err := ensureFresh(ctx)
		if err != nil {
			// A stale token is a soft cache miss for Bazel; a non-zero exit fails the RPC outright.
			warnStale(warn, err)

			return Credential{Token: cfg.AuthToken, Expiry: time.Now().Add(staleCacheHint)}, nil
		}

		return Credential{Token: creds.PAT, Expiry: creds.PATExpiry.Add(-expiresLead)}, nil
	}
}

func warnStale(w io.Writer, err error) {
	if w == nil {
		return
	}

	var msg string
	switch {
	case errors.Is(err, oauth.ErrNotLoggedIn):
		msg = "bitrise-build-cache: the stored token cannot be renewed automatically — run `bitrise-build-cache auth login`"
	case errors.Is(err, oauth.ErrLoginRequired):
		msg = "bitrise-build-cache: the Bitrise sign-in expired — run `bitrise-build-cache auth login`"
	default:
		// Transient (network, timeout): the stored token is probably still good.
		return
	}

	p, pathErr := paths.Default()
	if pathErr == nil && !filelock.ClaimCooldown(p.BazelCredHelperWarnFile(), warnCooldown) {
		return
	}

	_, _ = fmt.Fprintln(w, msg)
}
