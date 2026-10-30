// Package host captures Xcode-only host probes (thermal, memory, VM marker,
// Spotlight / Time Machine state, model identity) and composes them onto
// common.HostMetadata without extending the common type.
package host

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

// Snapshot composes HostMetadata (embedded by value) with Xcode-only probes
// and doubles as the wire shape attached to the enrichment payload.
type Snapshot struct {
	common.HostMetadata

	ThermalPressure   string `json:"thermalPressure,omitempty"`
	MemFreeMB         uint64 `json:"memFreeMB,omitempty"`
	SwapTotalMB       uint64 `json:"swapTotalMB,omitempty"`
	SwapFreeMB        uint64 `json:"swapFreeMB,omitempty"`
	UptimeSeconds     uint64 `json:"uptimeSeconds,omitempty"`
	IsVirtual         bool   `json:"isVirtual,omitempty"`
	SpotlightActive   bool   `json:"spotlightActive,omitempty"`
	TimeMachineActive bool   `json:"timeMachineActive,omitempty"`
	ModelID           string `json:"modelID,omitempty"`
	Chip              string `json:"chip,omitempty"`
}

// systemProfiler output is cached for the process lifetime (hardware identity
// never changes); other probes re-run every Capture since they move during a
// build. Mutex rather than sync.Once so tests can reset without copying.
//
//nolint:gochecknoglobals
var (
	systemProfilerMu     sync.Mutex
	systemProfilerLoaded bool
	systemProfilerModel  string
	systemProfilerChip   string
)

// Capture returns a Snapshot composed of the read-only base and fresh probe
// readings.
func Capture(ctx context.Context, base common.HostMetadata) Snapshot {
	s := Snapshot{HostMetadata: base}

	s.ThermalPressure = probeThermal(ctx)
	s.MemFreeMB, s.SwapTotalMB, s.SwapFreeMB = probeMemory(ctx)
	s.UptimeSeconds = probeUptime(ctx)
	s.IsVirtual = probeIsVirtual(ctx)
	s.SpotlightActive = probeSpotlight(ctx)
	s.TimeMachineActive = probeTimeMachine(ctx)
	s.ModelID, s.Chip = probeHardwareIdentity(ctx)

	return s
}

// run is swapped by tests; probes return zero values on error so a missing
// probe never breaks enrichment.
//
//nolint:gochecknoglobals
var run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	return cmd.Output()
}

func probeThermal(ctx context.Context) string {
	out, err := run(ctx, "pmset", "-g", "therm")
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CPU_Scheduler_Limit") || strings.HasPrefix(line, "CPU_Speed_Limit") {
			return line
		}
	}

	return strings.TrimSpace(string(out))
}

var vmStatPageRe = regexp.MustCompile(`page size of (\d+) bytes`) //nolint:gochecknoglobals

func probeMemory(ctx context.Context) (uint64, uint64, uint64) {
	out, err := run(ctx, "vm_stat")
	if err != nil {
		return 0, 0, 0
	}

	pageSize := uint64(4096)
	if m := vmStatPageRe.FindStringSubmatch(string(out)); len(m) == 2 {
		if v, convErr := strconv.ParseUint(m[1], 10, 64); convErr == nil && v > 0 {
			pageSize = v
		}
	}

	var pagesFree, pagesInactive uint64

	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "Pages free:"):
			pagesFree = parsePagesLine(line)
		case strings.HasPrefix(line, "Pages inactive:"):
			pagesInactive = parsePagesLine(line)
		}
	}

	free := (pagesFree + pagesInactive) * pageSize / (1024 * 1024)
	swapTotal, swapFree := probeSwap(ctx)

	return free, swapTotal, swapFree
}

func parsePagesLine(line string) uint64 {
	colon := strings.Index(line, ":")
	if colon < 0 {
		return 0
	}

	value := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line[colon+1:]), "."))

	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}

	return n
}

var swapRe = regexp.MustCompile(`total = (\d+\.?\d*)M.*used = (\d+\.?\d*)M.*free = (\d+\.?\d*)M`) //nolint:gochecknoglobals

func probeSwap(ctx context.Context) (uint64, uint64) {
	out, err := run(ctx, "sysctl", "-n", "vm.swapusage")
	if err != nil {
		return 0, 0
	}

	m := swapRe.FindStringSubmatch(strings.TrimSpace(string(out)))
	if len(m) != 4 {
		return 0, 0
	}

	totalF, _ := strconv.ParseFloat(m[1], 64)
	freeF, _ := strconv.ParseFloat(m[3], 64)

	return uint64(totalF), uint64(freeF)
}

func probeUptime(ctx context.Context) uint64 {
	out, err := run(ctx, "sysctl", "-n", "kern.boottime")
	if err != nil {
		return 0
	}

	// Output format: `{ sec = 1700000000, usec = 0 } Mon Nov 13 ...`.
	trimmed := strings.TrimSpace(string(out))

	secIdx := strings.Index(trimmed, "sec = ")
	if secIdx < 0 {
		return 0
	}

	rest := trimmed[secIdx+len("sec = "):]

	comma := strings.Index(rest, ",")
	if comma < 0 {
		return 0
	}

	boot, err := strconv.ParseInt(strings.TrimSpace(rest[:comma]), 10, 64)
	if err != nil || boot <= 0 {
		return 0
	}

	now, err := run(ctx, "date", "+%s")
	if err != nil {
		return 0
	}

	nowSec, err := strconv.ParseInt(strings.TrimSpace(string(now)), 10, 64)
	if err != nil || nowSec <= boot {
		return 0
	}

	diff := nowSec - boot
	if diff < 0 {
		return 0
	}

	return uint64(diff)
}

func probeIsVirtual(ctx context.Context) bool {
	out, err := run(ctx, "sysctl", "-n", "kern.hv_vmm_present")
	if err != nil {
		return false
	}

	return strings.TrimSpace(string(out)) == "1"
}

func probeSpotlight(ctx context.Context) bool {
	out, err := run(ctx, "mdutil", "-s", "/")
	if err != nil {
		return false
	}

	s := strings.ToLower(string(out))

	return strings.Contains(s, "indexing enabled")
}

func probeTimeMachine(ctx context.Context) bool {
	out, err := run(ctx, "tmutil", "status")
	if err != nil {
		return false
	}

	return strings.Contains(string(out), "Running = 1")
}

func probeHardwareIdentity(ctx context.Context) (string, string) {
	systemProfilerMu.Lock()
	defer systemProfilerMu.Unlock()

	if systemProfilerLoaded {
		return systemProfilerModel, systemProfilerChip
	}

	systemProfilerLoaded = true

	out, err := run(ctx, "system_profiler", "SPHardwareDataType")
	if err != nil {
		return "", ""
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "Model Identifier:"):
			systemProfilerModel = strings.TrimSpace(strings.TrimPrefix(line, "Model Identifier:"))
		case strings.HasPrefix(line, "Chip:"):
			systemProfilerChip = strings.TrimSpace(strings.TrimPrefix(line, "Chip:"))
		case strings.HasPrefix(line, "Processor Name:") && systemProfilerChip == "":
			systemProfilerChip = strings.TrimSpace(strings.TrimPrefix(line, "Processor Name:"))
		}
	}

	return systemProfilerModel, systemProfilerChip
}
