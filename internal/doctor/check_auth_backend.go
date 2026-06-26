package doctor

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
)

const (
	backendProbeTimeout  = 5 * time.Second
	backendProbeKeyBytes = 4 // 4 bytes → 8 hex chars in the sentinel key
	backendProbeHint     = " — re-run `bitrise-build-cache activate --interactive` to refresh credentials"
)

// BackendProbeFunc returns latency (always populated, even on error so callers can surface "took N ms then failed").
type BackendProbeFunc func(ctx context.Context, cfg common.CacheAuthConfig, envs map[string]string) (time.Duration, error)

func (d *Doctor) authBackendCheck() Check {
	return Check{
		Name: "auth-backend",
		Diagnose: func(ctx context.Context) Result {
			cfg, source, err := common.ResolveAuthConfig(d.Envs)
			if err != nil {
				return Result{State: StateOK, Detail: "skipped (source=none, no credentials resolvable: " + err.Error() + ")"}
			}

			probe := d.BackendProbe
			if probe == nil {
				probe = defaultBackendProbe
			}

			probeCtx, cancel := context.WithTimeout(ctx, backendProbeTimeout)
			defer cancel()

			latency, err := probe(probeCtx, cfg, d.Envs)
			if err != nil {
				return Result{
					State:   backendErrorState(err),
					Detail:  backendErrorDetail(err, cfg, source, latency),
					Fixable: backendErrorFixable(err),
				}
			}

			return Result{
				State:  StateOK,
				Detail: fmt.Sprintf("latency %dms, source=%s, workspace=%s", latency.Milliseconds(), sourceLabel(source), cfg.WorkspaceID),
			}
		},
		Fix: d.activateWizardFix,
	}
}

func backendErrorFixable(err error) bool {
	if errors.Is(err, kv.ErrCacheUnauthenticated) {
		return true
	}

	if s, ok := status.FromError(err); ok {
		switch s.Code() { //nolint:exhaustive // only the two auth-related codes are fixable.
		case codes.Unauthenticated, codes.PermissionDenied:
			return true
		}
	}

	return false
}

func (d *Doctor) activateWizardFix() (string, error) {
	launcher := d.LaunchActivateWizard
	if launcher == nil {
		launcher = defaultLaunchActivateWizard
	}

	if err := launcher(); err != nil {
		return "", fmt.Errorf("launch activate --interactive: %w", err)
	}

	return "ran `bitrise-build-cache activate --interactive`", nil
}

// defaultLaunchActivateWizard re-execs the running CLI binary as a child process.
// The wizard takes over stdin/stdout/stderr and runs to completion before returning.
// context.Background is correct here: the wizard's lifetime is the user's interactive
// session, not the parent's check timeout. Signals (SIGINT) reach the child via the
// shared controlling terminal's process group.
func defaultLaunchActivateWizard() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate cli executable: %w", err)
	}

	cmd := exec.CommandContext(context.Background(), exe, "activate", "--interactive")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("activate --interactive: %w", err)
	}

	return nil
}

func defaultBackendProbe(ctx context.Context, cfg common.CacheAuthConfig, envs map[string]string) (time.Duration, error) {
	endpoint := common.SelectCacheEndpointURL("", envs)
	host, insecureGRPC, err := kv.ParseURLGRPC(endpoint)
	if err != nil {
		return 0, fmt.Errorf("parse endpoint %q: %w", endpoint, err)
	}

	// kv.NewClient ignores DialTimeout — grpc.NewClient is lazy. The probeCtx
	// deadline on the caller is the real budget for dial + handshake + RPC.
	client, err := kv.NewClient(kv.NewClientParams{
		UseInsecure: insecureGRPC,
		Host:        host,
		ClientName:  "doctor-backend-probe",
		AuthConfig:  cfg,
		Logger:      log.NewLogger(),
	})
	if err != nil {
		return 0, fmt.Errorf("dial %s: %w", host, err)
	}
	defer func() { _ = client.Close() }()

	key, err := probeKey()
	if err != nil {
		return 0, err
	}

	payload := []byte{0}

	start := time.Now()
	uploadErr := client.UploadStreamToBuildCache(ctx, bytes.NewReader(payload), key, int64(len(payload)))
	latency := time.Since(start)

	if uploadErr != nil {
		return latency, uploadErr //nolint:wrapcheck // caller classifies via kv sentinel + status.FromError.
	}

	// Best-effort cleanup.
	_ = client.Delete(ctx, key)

	return latency, nil
}

func probeKey() (string, error) {
	var b [backendProbeKeyBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random bytes for probe key: %w", err)
	}

	return "doctor-probe-" + hex.EncodeToString(b[:]), nil
}

func backendErrorState(err error) State {
	if errors.Is(err, kv.ErrCacheUnauthenticated) {
		return StateError
	}

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

	// The kv client converts gRPC Unauthenticated into a plain sentinel error
	// before returning, so status.FromError can't see it. Check the sentinel first.
	if errors.Is(err, kv.ErrCacheUnauthenticated) {
		return prefix + "auth-failed: token rejected by Build Cache (expired / revoked / wrong workspace)" + backendProbeHint
	}

	if s, ok := status.FromError(err); ok {
		switch s.Code() { //nolint:exhaustive // only auth + transport-class codes have specific handling; all others fall through.
		case codes.Unauthenticated:
			return prefix + "auth-failed: token rejected by Build Cache (expired / revoked / wrong workspace)" + backendProbeHint
		case codes.PermissionDenied:
			return prefix + "workspace-misconfig: token accepted but no access to this workspace" + backendProbeHint
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
