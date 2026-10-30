//go:build unit

package host

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

func resetSystemProfilerCache(t *testing.T) {
	t.Helper()

	systemProfilerMu.Lock()
	defer systemProfilerMu.Unlock()

	loadedOrig := systemProfilerLoaded
	modelOrig := systemProfilerModel
	chipOrig := systemProfilerChip

	systemProfilerLoaded = false
	systemProfilerModel = ""
	systemProfilerChip = ""

	t.Cleanup(func() {
		systemProfilerMu.Lock()
		defer systemProfilerMu.Unlock()
		systemProfilerLoaded = loadedOrig
		systemProfilerModel = modelOrig
		systemProfilerChip = chipOrig
	})
}

func TestCapture_composesBaseWithoutMutation(t *testing.T) {
	original := run
	t.Cleanup(func() { run = original })

	resetSystemProfilerCache(t)

	run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch name {
		case "pmset":
			return []byte("Note: No AC adapter is attached.\nCPU_Scheduler_Limit \t = 100"), nil
		case "vm_stat":
			return []byte("Mach Virtual Memory Statistics: (page size of 16384 bytes)\n" +
				"Pages free:                            256000.\n" +
				"Pages inactive:                        128000.\n"), nil
		case "sysctl":
			if len(args) > 0 && args[len(args)-1] == "kern.hv_vmm_present" {
				return []byte("0\n"), nil
			}

			if len(args) > 0 && args[len(args)-1] == "kern.boottime" {
				return []byte("{ sec = 1700000000, usec = 0 } Mon Nov 13 00:00:00 2023"), nil
			}

			if len(args) > 0 && args[len(args)-1] == "vm.swapusage" {
				return []byte("total = 4096.00M  used = 1024.00M  free = 3072.00M  (encrypted)"), nil
			}

			return []byte(""), nil
		case "date":
			return []byte("1700003600\n"), nil
		case "mdutil":
			return []byte("/:\n\tIndexing enabled."), nil
		case "tmutil":
			return []byte("Backup session status:\n{\n    Running = 0;\n}"), nil
		case "system_profiler":
			return []byte("Hardware Overview:\n\n      Model Identifier: Mac15,10\n      Chip: Apple M4 Max"), nil
		}

		return nil, nil
	}

	base := common.HostMetadata{
		OS:       "Darwin arm64",
		CPUCores: 16,
		MemSize:  int64(128 * 1024 * 1024 * 1024),
		Hostname: "test-mac.local",
		Username: "tester",
	}

	snap := Capture(context.Background(), base)

	// Composed base remains intact.
	assert.Equal(t, "Darwin arm64", snap.OS)
	assert.Equal(t, 16, snap.CPUCores)
	assert.Equal(t, "test-mac.local", snap.Hostname)

	// Xcode-specific probes populated.
	assert.Contains(t, snap.ThermalPressure, "CPU_Scheduler_Limit")
	// (256000 + 128000) pages * 16384 bytes / 1 MB = 6_000 MB
	assert.Equal(t, uint64(6000), snap.MemFreeMB)
	assert.Equal(t, uint64(4096), snap.SwapTotalMB)
	assert.Equal(t, uint64(3072), snap.SwapFreeMB)
	assert.Equal(t, uint64(3600), snap.UptimeSeconds)
	assert.False(t, snap.IsVirtual)
	assert.True(t, snap.SpotlightActive)
	assert.False(t, snap.TimeMachineActive)
	assert.Equal(t, "Mac15,10", snap.ModelID)
	assert.Equal(t, "Apple M4 Max", snap.Chip)

	// Base untouched (value-type, so this is tautological, but it guards
	// against anyone later turning the field into a pointer).
	require.Equal(t, "test-mac.local", base.Hostname)
}

func TestCapture_toleratesCommandFailures(t *testing.T) {
	original := run
	t.Cleanup(func() { run = original })

	resetSystemProfilerCache(t)

	run = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, assert.AnError
	}

	snap := Capture(context.Background(), common.HostMetadata{OS: "Darwin"})

	// Every probe falls back to zero without failing enrichment.
	assert.Equal(t, "Darwin", snap.OS)
	assert.Empty(t, snap.ThermalPressure)
	assert.Zero(t, snap.MemFreeMB)
	assert.False(t, snap.IsVirtual)
	assert.Empty(t, snap.ModelID)
}
