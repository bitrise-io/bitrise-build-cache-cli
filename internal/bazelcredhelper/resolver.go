package bazelcredhelper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/oauth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// Pulls the cache hint inside oauth's refresh window. MUST stay below
// RefreshSkew: a larger lead re-spawns the helper on every RPC, because
// EnsureFresh's fast path keeps returning the same already-past expires.
const expiresLead = oauth.RefreshSkew / 2

// Short so a repaired login is picked up without waiting out Bazel's 30m default.
const staleCacheHint = time.Minute

// Rate-limits the warning across the many helper processes one build spawns.
const warnCooldown = 10 * time.Minute

// warn must not be stdout.
func NewResolver(envs map[string]string, warn io.Writer) Resolver {
	// Logger stays nil: go-utils' logger writes to stdout, which is the protocol channel.
	return newResolver(live.Default(nil), envs, warn)
}

// newResolver holds Bazel's failure policy and nothing else; resolution and
// refresh belong to live.
func newResolver(resolver *live.Resolver, envs map[string]string, warn io.Writer) Resolver {
	// FailFast so a dead login is reported here rather than silently served: Bazel
	// treats a short expiry as a soft cache miss, which is the graceful degradation.
	resolver.OnRefreshFailure = live.FailFast

	return func(ctx context.Context) (Credential, error) {
		cred, origin, err := resolver.Resolve(ctx, envs)
		switch {
		case err != nil && !origin.Resolved():
			return Credential{}, fmt.Errorf("resolve stored credentials: %w", err)
		case err != nil:
			// A stale token is a soft cache miss for Bazel; a non-zero exit fails the RPC outright.
			warnStale(warn, err)

			return Credential{Token: cred.Token, Expiry: time.Now().Add(staleCacheHint)}, nil
		// Env vars, the CI JWT and the analytics block carry no refresh token, so
		// there is no expiry to hint at.
		case !origin.StoreManaged():
			return Credential{Token: cred.Token}, nil
		// A manual `auth set` PAT is store-managed but has no expiry to subtract
		// from; a zero time here would serialise as year 1 and make Bazel re-spawn
		// the helper on every RPC.
		case cred.Expiry.IsZero():
			return Credential{Token: cred.Token}, nil
		}

		return Credential{Token: cred.Token, Expiry: cred.Expiry.Add(-expiresLead)}, nil
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
	if pathErr == nil && !claimCooldown(p.BazelCredHelperWarnFile(), warnCooldown) {
		return
	}

	_, _ = fmt.Fprintln(w, msg)
}
