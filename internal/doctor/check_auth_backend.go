package doctor

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
)

const (
	backendProbeTimeout  = 5 * time.Second
	backendProbeKeyBytes = 8
)

// BackendProbeFunc performs the network round-trip the auth-backend check uses.
// Returns the round-trip latency (always populated, even on error so callers can
// surface "took N ms then failed") and the gRPC-shaped error.
type BackendProbeFunc func(ctx context.Context, cfg common.CacheAuthConfig, envs map[string]string) (time.Duration, error)

func (d *Doctor) authBackendCheck() Check {
	return Check{
		Name: "auth-backend",
		Diagnose: func(ctx context.Context) Result {
			cfg, source, err := common.ResolveAuthConfig(d.Envs)
			if err != nil {
				return Result{State: StateOK, Detail: "skipped (no credentials resolvable)"}
			}

			probe := d.BackendProbe
			if probe == nil {
				probe = defaultBackendProbe
			}

			probeCtx, cancel := context.WithTimeout(ctx, backendProbeTimeout)
			defer cancel()

			latency, err := probe(probeCtx, cfg, d.Envs)
			if err != nil {
				return Result{State: backendErrorState(err), Detail: backendErrorDetail(err, cfg, source, latency)}
			}

			return Result{
				State:  StateOK,
				Detail: fmt.Sprintf("latency %dms, source=%s, workspace=%s", latency.Milliseconds(), sourceLabel(source), cfg.WorkspaceID),
			}
		},
	}
}

func defaultBackendProbe(ctx context.Context, cfg common.CacheAuthConfig, envs map[string]string) (time.Duration, error) {
	endpoint := common.SelectCacheEndpointURL("", envs)
	host, insecureGRPC, err := kv.ParseURLGRPC(endpoint)
	if err != nil {
		return 0, fmt.Errorf("parse endpoint %q: %w", endpoint, err)
	}

	client, err := kv.NewClient(kv.NewClientParams{
		UseInsecure: insecureGRPC,
		Host:        host,
		DialTimeout: backendProbeTimeout,
		ClientName:  "doctor",
		AuthConfig:  cfg,
		Logger:      log.NewLogger(),
	})
	if err != nil {
		return 0, fmt.Errorf("dial %s: %w", host, err)
	}

	payload := []byte{0}

	start := time.Now()
	uploadErr := client.UploadStreamToBuildCache(ctx, bytes.NewReader(payload), probeKey(), int64(len(payload)))

	return time.Since(start), uploadErr //nolint:wrapcheck // caller classifies via status.FromError
}

func probeKey() string {
	var b [backendProbeKeyBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "doctor-probe-fallback"
	}

	return "doctor-probe-" + hex.EncodeToString(b[:])
}

func backendErrorState(err error) State {
	if s, ok := status.FromError(err); ok {
		switch s.Code() { //nolint:exhaustive // only auth + transport-class codes have specific handling; all others fall through.
		case codes.Unauthenticated, codes.PermissionDenied:
			return StateError
		case codes.Unavailable, codes.DeadlineExceeded:
			return StateWarn
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return StateWarn
	}

	return StateError
}

func backendErrorDetail(err error, cfg common.CacheAuthConfig, source common.AuthSource, latency time.Duration) string {
	prefix := fmt.Sprintf("latency %dms, source=%s, workspace=%s — ", latency.Milliseconds(), sourceLabel(source), cfg.WorkspaceID)

	if s, ok := status.FromError(err); ok {
		switch s.Code() { //nolint:exhaustive // only auth + transport-class codes have specific handling; all others fall through.
		case codes.Unauthenticated:
			return prefix + "auth-failed: token rejected by Build Cache (expired / revoked / wrong workspace)"
		case codes.PermissionDenied:
			return prefix + "workspace-misconfig: token accepted but no access to this workspace"
		case codes.Unavailable, codes.DeadlineExceeded:
			return prefix + "network: " + s.Message()
		}
	}

	return prefix + err.Error()
}

func sourceLabel(s common.AuthSource) string {
	switch s {
	case common.AuthSourceKeychain:
		return "keychain"
	case common.AuthSourceEnvVars:
		return "env"
	case common.AuthSourceJWT:
		return "jwt"
	case common.AuthSourceMultiplatform:
		return "multiplatform-config"
	case common.AuthSourceNone:
		return "none"
	}

	return "unknown"
}
