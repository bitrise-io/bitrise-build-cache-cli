//go:build probe

package proxy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/klauspost/compress/zstd"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/resolver"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv/fakebackend"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy"
	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/build/bazel/remote/execution/v2"
	kvstorage "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/kv_storage"
)

// Builds a real Xcode project through the proxy and a fake backend with and without zstd, recording build time and CPU.
// The driver re-executes this test binary as the backend and proxy so their CPU is measured apart from the build's.
// Usage: docs/xcode-compression-benchmark.md.
const (
	probeRoleEnv      = "COMPRESSION_PROBE_ROLE"
	probeWorkspaceEnv = "COMPRESSION_PROBE_WORKSPACE"
	probeSchemeEnv    = "COMPRESSION_PROBE_SCHEME"
	probeOutEnv       = "COMPRESSION_PROBE_OUT"
	probeModesEnv     = "COMPRESSION_PROBE_MODES" // comma-separated, default "off,zstd"
	probeLevelEnv     = "COMPRESSION_PROBE_LEVEL" // fastest|default|better
	probeBackendEnv   = "COMPRESSION_PROBE_BACKEND"
	probeModeEnv      = "COMPRESSION_PROBE_MODE"
	probeSocketEnv    = "COMPRESSION_PROBE_SOCKET"
	probeStatsEnv     = "COMPRESSION_PROBE_STATS"

	probeSampleEvery = 500 * time.Millisecond
)

func TestCompressionBuildProbe(t *testing.T) {
	switch os.Getenv(probeRoleEnv) {
	case "backend":
		runProbeBackend(t)
	case "proxy":
		runProbeProxy(t)
	default:
		if os.Getenv(probeWorkspaceEnv) == "" {
			t.Skipf("%s not set", probeWorkspaceEnv)
		}
		runProbeDriver(t)
	}
}

// Backend role.

func runProbeBackend(t *testing.T) {
	endpoint, stop, err := fakebackend.New(1.0).Serve()
	require.NoError(t, err)
	defer stop()

	require.NoError(t, os.WriteFile(os.Getenv(probeBackendEnv), []byte(endpoint), 0o600))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	<-ctx.Done()
}

// Proxy role.

type probeStats struct {
	Mode           string  `json:"mode"`
	Uploads        int64   `json:"uploads"`
	UploadRawB     int64   `json:"uploadRawBytes"`
	UploadWireB    int64   `json:"uploadWireBytes"`
	Downloads      int64   `json:"downloads"`
	DownloadRawB   int64   `json:"downloadRawBytes"`
	DownloadWireB  int64   `json:"downloadWireBytes"`
	Misses         int64   `json:"misses"`
	ProxyCPUSecond float64 `json:"proxyCpuSeconds"`
}

const (
	markerRaw  = 0
	markerZstd = 1
)

// compressingClient models the production kv compression; a one-byte marker stands in for the backend's name prefix.
type compressingClient struct {
	proxy.Client

	compress bool
	enc      *zstd.Encoder
	dec      *zstd.Decoder

	uploads, uploadRaw, uploadWire       atomic.Int64
	downloads, downloadRaw, downloadWire atomic.Int64
	misses                               atomic.Int64
}

func newCompressingClient(inner proxy.Client, compress bool, level zstd.EncoderLevel) (*compressingClient, error) {
	c := &compressingClient{Client: inner, compress: compress}
	if !compress {
		return c, nil
	}

	var err error
	if c.enc, err = zstd.NewWriter(nil, zstd.WithEncoderLevel(level)); err != nil {
		return nil, fmt.Errorf("zstd writer: %w", err)
	}
	if c.dec, err = zstd.NewReader(nil); err != nil {
		return nil, fmt.Errorf("zstd reader: %w", err)
	}

	return c, nil
}

func (c *compressingClient) UploadStreamToBuildCache(ctx context.Context, reader io.ReadSeeker, key string, size int64) error {
	c.uploads.Add(1)
	c.uploadRaw.Add(size)

	if !c.compress {
		c.uploadWire.Add(size)

		return c.Client.UploadStreamToBuildCache(ctx, reader, key, size) //nolint:wrapcheck
	}

	raw, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read blob: %w", err)
	}

	stored := c.enc.EncodeAll(raw, append(make([]byte, 0, len(raw)/2+1), markerZstd))
	if len(stored) > len(raw) {
		stored = append([]byte{markerRaw}, raw...)
	}
	c.uploadWire.Add(int64(len(stored)))

	return c.Client.UploadStreamToBuildCache(ctx, bytes.NewReader(stored), key, int64(len(stored))) //nolint:wrapcheck
}

func (c *compressingClient) DownloadStream(ctx context.Context, writer io.Writer, key string) error {
	if !c.compress {
		counter := &countingWriter{w: writer}
		err := c.Client.DownloadStream(ctx, counter, key)
		c.noteDownload(err, counter.n, counter.n)

		return err //nolint:wrapcheck
	}

	var stored bytes.Buffer
	if err := c.Client.DownloadStream(ctx, &stored, key); err != nil {
		c.noteDownload(err, 0, 0)

		return err //nolint:wrapcheck
	}

	body := stored.Bytes()
	if len(body) == 0 {
		return fmt.Errorf("empty stored blob for %s", key)
	}

	raw := body[1:]
	if body[0] == markerZstd {
		var err error
		if raw, err = c.dec.DecodeAll(raw, nil); err != nil {
			return fmt.Errorf("zstd decode %s: %w", key, err)
		}
	}
	c.noteDownload(nil, int64(len(raw)), int64(len(body)))

	_, err := writer.Write(raw)

	return err //nolint:wrapcheck
}

func (c *compressingClient) noteDownload(err error, raw, wire int64) {
	if err != nil {
		c.misses.Add(1)

		return
	}
	c.downloads.Add(1)
	c.downloadRaw.Add(raw)
	c.downloadWire.Add(wire)
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)

	return n, err //nolint:wrapcheck
}

func runProbeProxy(t *testing.T) {
	resolver.SetDefaultScheme("passthrough")

	conn, err := grpc.NewClient(
		strings.TrimPrefix(os.Getenv(probeBackendEnv), "grpc://"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer conn.Close()

	logger := log.NewLogger()

	//nolint:exhaustruct // the injected clients replace the dialed pool
	cacheClient, err := kv.NewClient(kv.NewClientParams{
		ClientName:         "compression-probe",
		Logger:             logger,
		BitriseKVClient:    kvstorage.NewKVStorageClient(conn),
		CapabilitiesClient: remoteexecution.NewCapabilitiesClient(conn),
	})
	require.NoError(t, err)

	mode := os.Getenv(probeModeEnv)
	client, err := newCompressingClient(cacheClient, mode == "zstd", probeLevel(os.Getenv(probeLevelEnv)))
	require.NoError(t, err)

	socket := os.Getenv(probeSocketEnv)
	_ = os.Remove(socket)
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", socket)
	require.NoError(t, err)

	p := proxy.NewProxy(client, true, logger, func(string) (log.Logger, error) { return logger, nil }, nil)
	go func() { _ = p.Serve(listener) }()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	<-ctx.Done()

	p.GracefulStop()

	self, err := process.NewProcess(int32(os.Getpid())) //nolint:gosec
	require.NoError(t, err)
	times, err := self.Times()
	require.NoError(t, err)

	stats := probeStats{
		Mode:           mode,
		Uploads:        client.uploads.Load(),
		UploadRawB:     client.uploadRaw.Load(),
		UploadWireB:    client.uploadWire.Load(),
		Downloads:      client.downloads.Load(),
		DownloadRawB:   client.downloadRaw.Load(),
		DownloadWireB:  client.downloadWire.Load(),
		Misses:         client.misses.Load(),
		ProxyCPUSecond: times.User + times.System,
	}
	data, err := json.Marshal(stats)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(os.Getenv(probeStatsEnv), data, 0o600))
}

func probeLevel(name string) zstd.EncoderLevel {
	switch name {
	case "default":
		return zstd.SpeedDefault
	case "better":
		return zstd.SpeedBetterCompression
	default:
		return zstd.SpeedFastest
	}
}

// Driver role.

type probeBuildResult struct {
	Mode      string      `json:"mode"`
	Phase     string      `json:"phase"`
	WallSec   float64     `json:"wallSeconds"`
	SysCPU    cpuSummary  `json:"systemCpuPercent"`
	ProxyCPU  cpuSummary  `json:"proxyCpuPercentOfCore"`
	SysCPUSec float64     `json:"systemCpuSeconds"`
	Proxy     *probeStats `json:"proxy"`
}

type cpuSummary struct {
	Peak, Avg, Median, P95 float64
}

func summarize(samples []float64) cpuSummary {
	if len(samples) == 0 {
		return cpuSummary{}
	}

	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)

	sum := 0.0
	for _, s := range sorted {
		sum += s
	}

	return cpuSummary{
		Peak:   sorted[len(sorted)-1],
		Avg:    sum / float64(len(sorted)),
		Median: sorted[len(sorted)/2],
		P95:    sorted[min(len(sorted)-1, int(float64(len(sorted))*0.95))],
	}
}

func runProbeDriver(t *testing.T) {
	out := os.Getenv(probeOutEnv)
	require.NotEmpty(t, out, probeOutEnv)
	require.NoError(t, os.MkdirAll(out, 0o755))

	modes := strings.Split(envOr(probeModesEnv, "off,zstd"), ",")

	var results []probeBuildResult
	for _, mode := range modes {
		results = append(results, runProbeScenario(t, out, mode)...)
	}

	data, err := json.MarshalIndent(results, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(out, "results.json"), data, 0o600))

	for _, r := range results {
		t.Logf("%-5s %-4s wall=%6.1fs sysCPU%%[peak %.0f avg %.0f med %.0f p95 %.0f] cpu-s=%.0f proxyCPU%%[peak %.0f avg %.0f med %.0f p95 %.0f] proxy-cpu-s=%.1f wire up/down MiB=%.0f/%.0f raw up/down MiB=%.0f/%.0f misses=%d",
			r.Mode, r.Phase, r.WallSec,
			r.SysCPU.Peak, r.SysCPU.Avg, r.SysCPU.Median, r.SysCPU.P95, r.SysCPUSec,
			r.ProxyCPU.Peak, r.ProxyCPU.Avg, r.ProxyCPU.Median, r.ProxyCPU.P95, r.Proxy.ProxyCPUSecond,
			mib(r.Proxy.UploadWireB), mib(r.Proxy.DownloadWireB), mib(r.Proxy.UploadRawB), mib(r.Proxy.DownloadRawB), r.Proxy.Misses)
	}
}

func mib(b int64) float64 { return float64(b) / (1 << 20) }

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return def
}

// One backend per mode: a cold build fills it, then a build from wiped local state reads it back.
func runProbeScenario(t *testing.T, out, mode string) []probeBuildResult {
	dir := filepath.Join(out, mode)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	backendFile := filepath.Join(dir, "backend.addr")
	_ = os.Remove(backendFile)

	backend := exec.Command(os.Args[0], "-test.run=^TestCompressionBuildProbe$", "-test.timeout=0") //nolint:gosec,noctx
	backend.Env = append(os.Environ(), probeRoleEnv+"=backend", probeBackendEnv+"="+backendFile)
	require.NoError(t, backend.Start())
	defer func() {
		_ = backend.Process.Signal(syscall.SIGTERM)
		_ = backend.Wait()
	}()

	require.Eventually(t, func() bool {
		b, err := os.ReadFile(backendFile)

		return err == nil && len(b) > 0
	}, 30*time.Second, 100*time.Millisecond)

	var results []probeBuildResult
	for _, phase := range []string{"cold", "warm"} {
		results = append(results, runProbeBuild(t, dir, mode, phase, backendFile))
	}

	return results
}

func runProbeBuild(t *testing.T, dir, mode, phase, backendFile string) probeBuildResult {
	t.Logf("=== %s %s", mode, phase)

	derivedData := filepath.Join(dir, "dd")
	casPath := filepath.Join(dir, "cas")
	require.NoError(t, os.RemoveAll(derivedData))
	require.NoError(t, os.RemoveAll(casPath))

	backendAddr, err := os.ReadFile(backendFile)
	require.NoError(t, err)

	socket := filepath.Join(os.TempDir(), fmt.Sprintf("cprobe-%d.sock", os.Getpid()))
	statsFile := filepath.Join(dir, phase+"-proxy.json")
	_ = os.Remove(statsFile)

	proxyLog, err := os.Create(filepath.Join(dir, phase+"-proxy.log"))
	require.NoError(t, err)
	defer proxyLog.Close()

	proxyCmd := exec.Command(os.Args[0], "-test.run=^TestCompressionBuildProbe$", "-test.timeout=0") //nolint:gosec,noctx
	proxyCmd.Env = append(os.Environ(),
		probeRoleEnv+"=proxy", probeBackendEnv+"="+string(backendAddr), probeModeEnv+"="+mode,
		probeSocketEnv+"="+socket, probeStatsEnv+"="+statsFile)
	proxyCmd.Stdout, proxyCmd.Stderr = proxyLog, proxyLog
	require.NoError(t, proxyCmd.Start())

	require.Eventually(t, func() bool {
		_, err := os.Stat(socket)

		return err == nil
	}, 30*time.Second, 100*time.Millisecond)

	buildLog, err := os.Create(filepath.Join(dir, phase+"-build.log"))
	require.NoError(t, err)
	defer buildLog.Close()

	//nolint:gosec,noctx
	build := exec.Command("/usr/bin/xcodebuild", "build",
		"-workspace", os.Getenv(probeWorkspaceEnv), "-scheme", os.Getenv(probeSchemeEnv),
		"-destination", "generic/platform=iOS Simulator",
		"-derivedDataPath", derivedData,
		"-skipPackagePluginValidation", "-skipMacroValidation",
		"CODE_SIGNING_ALLOWED=NO",
		"COMPILATION_CACHE_ENABLE_CACHING=YES", "COMPILATION_CACHE_ENABLE_PLUGIN=YES",
		"COMPILATION_CACHE_ENABLE_INTEGRATED_QUERIES=YES", "COMPILATION_CACHE_ENABLE_DETACHED_KEY_QUERIES=YES",
		"COMPILATION_CACHE_CAS_PATH="+casPath, "COMPILATION_CACHE_REMOTE_SERVICE_PATH="+socket,
		"SWIFT_ENABLE_COMPILE_CACHE=YES", "SWIFT_ENABLE_EXPLICIT_MODULES=YES", "SWIFT_USE_INTEGRATED_DRIVER=YES",
		"CLANG_ENABLE_COMPILE_CACHE=YES", "CLANG_ENABLE_MODULES=YES")
	build.Stdout, build.Stderr = buildLog, buildLog

	proxyProc, err := process.NewProcess(int32(proxyCmd.Process.Pid)) //nolint:gosec
	require.NoError(t, err)

	// Prime the deltas so the first sample covers only its own interval.
	_, _ = cpu.Percent(0, false)
	startTimes, err := cpu.Times(false)
	require.NoError(t, err)
	prevProxy, err := proxyProc.Times()
	require.NoError(t, err)

	var sysSamples, proxySamples []float64
	stopSampling := make(chan struct{})
	sampled := make(chan struct{})
	go func() {
		defer close(sampled)
		ticker := time.NewTicker(probeSampleEvery)
		defer ticker.Stop()
		last := time.Now()
		for {
			select {
			case <-stopSampling:
				return
			case now := <-ticker.C:
				if pct, err := cpu.Percent(0, false); err == nil && len(pct) == 1 {
					sysSamples = append(sysSamples, pct[0])
				}
				if times, err := proxyProc.Times(); err == nil {
					delta := (times.User + times.System) - (prevProxy.User + prevProxy.System)
					proxySamples = append(proxySamples, delta/now.Sub(last).Seconds()*100)
					prevProxy = times
				}
				last = now
			}
		}
	}()

	start := time.Now()
	buildErr := build.Run()
	wall := time.Since(start)

	close(stopSampling)
	<-sampled

	endTimes, err := cpu.Times(false)
	require.NoError(t, err)

	require.NoError(t, proxyCmd.Process.Signal(syscall.SIGTERM))
	require.NoError(t, proxyCmd.Wait())
	require.NoError(t, buildErr, "xcodebuild failed, see %s", buildLog.Name())

	data, err := os.ReadFile(statsFile)
	require.NoError(t, err)
	var stats probeStats
	require.NoError(t, json.Unmarshal(data, &stats))

	busy := func(c cpu.TimesStat) float64 { return c.User + c.System + c.Nice }

	return probeBuildResult{
		Mode: mode, Phase: phase, WallSec: wall.Seconds(),
		SysCPU: summarize(sysSamples), ProxyCPU: summarize(proxySamples),
		SysCPUSec: busy(endTimes[0]) - busy(startTimes[0]),
		Proxy:     &stats,
	}
}
