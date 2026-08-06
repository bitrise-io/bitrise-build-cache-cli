package common

import (
	"context"

	"github.com/bitrise-io/go-utils/v2/log"
)

// RefreshFunc is a func type rather than an interface because importing
// internal/oauth here would cycle via internal/auth/store.
type RefreshFunc func(ctx context.Context) (pat string, workspaceID string, err error)

type resolveFunc func(envs map[string]string) (CacheAuthConfig, AuthSource, error)

// ExpiryAwareResolver refreshes store-managed credentials before serving them.
type ExpiryAwareResolver struct {
	ctx       context.Context //nolint:containedctx // resolver is called per RPC without a fresh ctx
	envs      map[string]string
	refreshFn RefreshFunc
	resolveFn resolveFunc
	logger    log.Logger
}

func NewExpiryAwareResolver(ctx context.Context, envs map[string]string, refreshFn RefreshFunc, logger log.Logger) *ExpiryAwareResolver {
	return newExpiryAwareResolver(ctx, envs, refreshFn, ResolveAuthConfig, logger)
}

func newExpiryAwareResolver(ctx context.Context, envs map[string]string, refreshFn RefreshFunc, resolveFn resolveFunc, logger log.Logger) *ExpiryAwareResolver {
	return &ExpiryAwareResolver{
		ctx:       ctx,
		envs:      envs,
		refreshFn: refreshFn,
		resolveFn: resolveFn,
		logger:    logger,
	}
}

func (r *ExpiryAwareResolver) Get() CacheAuthConfig {
	cfg, source, err := r.resolveFn(r.envs)
	if err != nil {
		if r.logger != nil {
			r.logger.Warnf("ExpiryAwareResolver: ResolveAuthConfig failed: %s", err)
		}

		return cfg
	}
	if !source.StoreManaged() || r.refreshFn == nil {
		return cfg
	}

	pat, wsid, err := r.refreshFn(r.ctx)
	if err != nil {
		if r.logger != nil {
			r.logger.Debugf("ExpiryAwareResolver: refreshFn failed, serving previous credentials: %s", err)
		}

		return cfg
	}

	return CacheAuthConfig{AuthToken: pat, WorkspaceID: wsid}
}
