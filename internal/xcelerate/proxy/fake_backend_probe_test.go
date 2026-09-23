//go:build probe

package proxy_test

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv/fakebackend"
)

// TestFakeBackendServe runs the fake backend until killed, writing its endpoint
// to FAKE_BACKEND_ENDPOINT_FILE. The runner starts this once and points the
// proxy at it, so every arm talks to the same local server.
func TestFakeBackendServe(t *testing.T) {
	if os.Getenv("FAKE_BACKEND_SERVE") != "1" {
		t.Skip("set FAKE_BACKEND_SERVE=1 to run the fake backend")
	}

	hitRate := 0.5
	if v := os.Getenv("FAKE_BACKEND_HIT_RATE"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			hitRate = parsed
		}
	}

	backend := fakebackend.New(hitRate)
	if v := os.Getenv("FAKE_BACKEND_DELAY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			backend.Delay = d
		}
	}

	endpoint, stop, err := backend.Serve()
	if err != nil {
		t.Fatalf("serve fake backend: %v", err)
	}
	defer stop()

	t.Logf("fake backend listening on %s (hit rate %.2f)", endpoint, hitRate)

	if out := os.Getenv("FAKE_BACKEND_ENDPOINT_FILE"); out != "" {
		if err := os.WriteFile(out, []byte(endpoint+"\n"), 0o600); err != nil {
			t.Fatalf("write endpoint file: %v", err)
		}
	}

	// Killed by the runner when the arms are done.
	select {}
}
