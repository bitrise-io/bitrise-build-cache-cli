//go:build probe

package proxy_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	llvmkv "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/kv"
)

// Drives the proxy over its unix socket, the path xcodebuild's
// compilation-cache plugin takes. Not part of any suite: it needs a running
// proxy. See docs/daemon-latency.md.
//
//	PROXY_PROBE_SOCKET    unix socket path (required)
//	PROXY_PROBE_OPS       operations (default 120)
//	PROXY_PROBE_PARALLEL  concurrent workers (default 8)
//	PROXY_PROBE_LABEL     label echoed into the result line
//	PROXY_PROBE_OUT       file to write the result line to
const (
	probeDefaultOps      = 120
	probeDefaultParallel = 8
	probeValueSize       = 32 * 1024
	probeCallTimeout     = 60 * time.Second
)

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}

	return fallback
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	return sorted[int(float64(len(sorted)-1)*p)]
}

func TestProxyLatencyProbe(t *testing.T) {
	socket := os.Getenv("PROXY_PROBE_SOCKET")
	if socket == "" {
		t.Skip("set PROXY_PROBE_SOCKET to the xcelerate proxy socket path")
	}

	label := os.Getenv("PROXY_PROBE_LABEL")
	if label == "" {
		label = "unlabelled"
	}
	ops := envInt("PROXY_PROBE_OPS", probeDefaultOps)
	parallel := envInt("PROXY_PROBE_PARALLEL", probeDefaultParallel)

	conn, err := grpc.NewClient("unix://"+socket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial proxy at %s: %v", socket, err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	client := llvmkv.NewKeyValueDBClient(conn)

	blob := make([]byte, probeValueSize)
	if _, err := rand.Read(blob); err != nil {
		t.Fatalf("random value: %v", err)
	}

	var (
		mu       sync.Mutex
		puts     []time.Duration
		gets     []time.Duration
		failures atomic.Int64
	)

	work := make(chan int, ops)
	for i := range ops {
		work <- i
	}
	close(work)

	var wg sync.WaitGroup
	started := time.Now()

	for range parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				key := fmt.Appendf(nil, "proxy-latency-probe-%s-%d", label, i)

				ctx, cancel := context.WithTimeout(context.Background(), probeCallTimeout)
				t0 := time.Now()
				_, putErr := client.PutValue(ctx, &llvmkv.PutValueRequest{
					Key:   key,
					Value: &llvmkv.Value{Entries: map[string][]byte{"data": blob}},
				})
				put := time.Since(t0)
				cancel()

				ctx, cancel = context.WithTimeout(context.Background(), probeCallTimeout)
				t1 := time.Now()
				_, getErr := client.GetValue(ctx, &llvmkv.GetValueRequest{Key: key})
				get := time.Since(t1)
				cancel()

				if putErr != nil || getErr != nil {
					failures.Add(1)
				}

				mu.Lock()
				puts = append(puts, put)
				gets = append(gets, get)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	elapsed := time.Since(started)
	sort.Slice(puts, func(i, j int) bool { return puts[i] < puts[j] })
	sort.Slice(gets, func(i, j int) bool { return gets[i] < gets[j] })

	line := fmt.Sprintf(
		"PROBE label=%s ops=%d parallel=%d cpus=%d failures=%d elapsed=%s "+
			"up_p50=%s up_p90=%s up_p99=%s down_p50=%s down_p90=%s down_p99=%s",
		label, ops, parallel, runtime.NumCPU(), failures.Load(), elapsed.Round(time.Millisecond),
		percentile(puts, 0.50).Round(time.Millisecond),
		percentile(puts, 0.90).Round(time.Millisecond),
		percentile(puts, 0.99).Round(time.Millisecond),
		percentile(gets, 0.50).Round(time.Millisecond),
		percentile(gets, 0.90).Round(time.Millisecond),
		percentile(gets, 0.99).Round(time.Millisecond),
	)

	t.Log(line)
	fmt.Println(line)

	if out := os.Getenv("PROXY_PROBE_OUT"); out != "" {
		if err := os.WriteFile(out, []byte(line+"\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", out, err)
		}
	}
}
