//go:build unit

package analytics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
)

// sampleStatsOutput is representative `ccache -v -v -s` text output.
const sampleStatsOutput = `Cache directory:                          /home/user/.cache/ccache
Config file:                              /home/user/.config/ccache/ccache.conf
Stats updated:                            Mon Apr 14 10:00:00 2025
Stats zeroed:                             Mon Apr 14 09:00:00 2025
Cacheable calls:                            18
  Hits:                                      5 / 18  (27.78 %)
    Direct:                                  3
    Preprocessed:                            2
  Misses:                                   13
Uncacheable calls:                           2
  Autoconf compile/link:                     0
  Bad compiler arguments:                    0
  Called for linking:                        2
  Called for preprocessing:                  0
  Ccache disabled:                           0
  Compilation failed:                        0
  Compiler output file missing:              0
  Compiler produced empty output:            0
  Compiler produced stdout:                  0
  Could not use modules:                     0
  Could not use precompiled header:          0
  Forced recache:                            0
  Multiple source files:                     0
  No input file:                             0
  Output to stdout:                          0
  Preprocessing failed:                      0
  Unsupported code directive:                0
  Unsupported compiler option:               0
  Unsupported environment variable:          0
  Unsupported source encoding:               0
  Unsupported source language:               0
Errors:                                      0
  Compiler check failed:                     0
  Could not find compiler:                   0
  Could not read or parse input file:        0
  Could not write to output file:            0
  Error hashing extra file:                  0
  Input file modified during compilation:    0
  Internal error:                            0
  Missing cache file:                        0
Local storage:
  Cache size (GiB):                        0.4 / 10.0 ( 3.85%)
  Files:                                  4520
  Cleanups:                                  0
  Hits:                                      5
  Misses:                                   13
  Reads:                                    18
  Writes:                                   13
Remote storage:
  Hits:                                      3
  Misses:                                   10
  Reads:                                    13
  Writes:                                    3
  Errors:                                    1
  Timeouts:                                  0
`

const sampleConfigOutput = `(default) absolute_paths_in_stderr = false
(default) base_dir =
(default) cache_dir = /Users/user/Library/Caches/ccache
(default) compiler_check = mtime
(/Users/user/Library/Preferences/ccache/ccache.conf) hash_dir = false
(/Users/user/Library/Preferences/ccache/ccache.conf) max_size = 10.0 GiB
(/Users/user/Library/Preferences/ccache/ccache.conf) remote_only = true
(/Users/user/Library/Preferences/ccache/ccache.conf) remote_storage = crsh:/var/folders/tmp/ccache-ipc.sock
`

func Test_ParseCcacheConfig(t *testing.T) {
	t.Run("parses default source entries", func(t *testing.T) {
		entries := ParseCcacheConfig([]byte(sampleConfigOutput))

		assert.Contains(t, entries, CcacheConfigEntry{Key: "absolute_paths_in_stderr", Value: "false", Source: "default"})
		assert.Contains(t, entries, CcacheConfigEntry{Key: "compiler_check", Value: "mtime", Source: "default"})
	})

	t.Run("parses file source entries", func(t *testing.T) {
		entries := ParseCcacheConfig([]byte(sampleConfigOutput))

		assert.Contains(t, entries, CcacheConfigEntry{
			Key:    "max_size",
			Value:  "10.0 GiB",
			Source: "/Users/user/Library/Preferences/ccache/ccache.conf",
		})
		assert.Contains(t, entries, CcacheConfigEntry{
			Key:    "remote_storage",
			Value:  "crsh:/var/folders/tmp/ccache-ipc.sock",
			Source: "/Users/user/Library/Preferences/ccache/ccache.conf",
		})
	})

	t.Run("empty value trimmed to empty string", func(t *testing.T) {
		entries := ParseCcacheConfig([]byte(sampleConfigOutput))

		assert.Contains(t, entries, CcacheConfigEntry{Key: "base_dir", Value: "", Source: "default"})
	})

	t.Run("returns all entries", func(t *testing.T) {
		entries := ParseCcacheConfig([]byte(sampleConfigOutput))

		assert.Len(t, entries, 8)
	})

	t.Run("malformed lines silently ignored", func(t *testing.T) {
		entries := ParseCcacheConfig([]byte("not a valid line\nalso bad\n"))

		assert.Empty(t, entries)
	})

	t.Run("empty input returns empty slice", func(t *testing.T) {
		entries := ParseCcacheConfig([]byte(""))

		assert.Empty(t, entries)
	})
}

func Test_ParseCcacheStats(t *testing.T) {
	t.Run("parses cacheable call counts", func(t *testing.T) {
		stats, err := ParseCcacheStats([]byte(sampleStatsOutput))

		assert.NoError(t, err)
		assert.Equal(t, 3, stats.DirectCacheHit)
		assert.Equal(t, 2, stats.PreprocessedCacheHit)
		assert.Equal(t, 13, stats.CacheMiss)
		assert.Equal(t, 18, stats.CacheableCalls)
	})

	t.Run("parses uncacheable call counts", func(t *testing.T) {
		stats, err := ParseCcacheStats([]byte(sampleStatsOutput))

		assert.NoError(t, err)
		assert.Equal(t, 2, stats.UncacheableCalls)
		assert.Equal(t, 2, stats.CalledForLink)
		assert.Equal(t, 0, stats.CalledForPreprocessing)
	})

	t.Run("derives total calls", func(t *testing.T) {
		stats, err := ParseCcacheStats([]byte(sampleStatsOutput))

		assert.NoError(t, err)
		assert.Equal(t, 20, stats.TotalCalls) // 18 cacheable + 2 uncacheable
	})

	t.Run("computes cache hit rate", func(t *testing.T) {
		// 3 direct + 2 preprocessed = 5 hits out of 18 total cacheable calls → 5/18
		stats, err := ParseCcacheStats([]byte(sampleStatsOutput))

		assert.NoError(t, err)
		assert.InDelta(t, 5.0/18.0, stats.CacheHitRate, 1e-9)
	})

	t.Run("parses local storage fields", func(t *testing.T) {
		stats, err := ParseCcacheStats([]byte(sampleStatsOutput))

		assert.NoError(t, err)
		assert.InDelta(t, 0.4, stats.CacheSizeGiB, 1e-9)
		assert.InDelta(t, 10.0, stats.MaxCacheSizeGiB, 1e-9)
		assert.Equal(t, 4520, stats.FilesInCache)
		assert.Equal(t, 0, stats.CleanupsPerformed)
		assert.Equal(t, 5, stats.LocalStorageHit)
		assert.Equal(t, 13, stats.LocalStorageMiss)
		assert.Equal(t, 18, stats.LocalStorageReads)
		assert.Equal(t, 13, stats.LocalStorageWrite)
	})

	t.Run("parses remote storage fields", func(t *testing.T) {
		stats, err := ParseCcacheStats([]byte(sampleStatsOutput))

		assert.NoError(t, err)
		assert.Equal(t, 3, stats.RemoteStorageHit)
		assert.Equal(t, 10, stats.RemoteStorageMiss)
		assert.Equal(t, 13, stats.RemoteStorageReads)
		assert.Equal(t, 3, stats.RemoteStorageWrite)
		assert.Equal(t, 1, stats.RemoteStorageError)
		assert.Equal(t, 0, stats.RemoteStorageTimeout)
	})

	t.Run("hit rate is zero when no cacheable calls", func(t *testing.T) {
		stats, err := ParseCcacheStats([]byte(`Cacheable calls:                             0
  Hits:                                      0
    Direct:                                  0
    Preprocessed:                            0
  Misses:                                    0
`))

		assert.NoError(t, err)
		assert.Equal(t, 0.0, stats.CacheHitRate)
	})

	t.Run("hit rate is 1.0 when all calls are direct hits", func(t *testing.T) {
		stats, err := ParseCcacheStats([]byte(`Cacheable calls:                             5
  Hits:                                      5 / 5  (100.00 %)
    Direct:                                  5
    Preprocessed:                            0
  Misses:                                    0
`))

		assert.NoError(t, err)
		assert.Equal(t, 1.0, stats.CacheHitRate)
	})

	t.Run("returns no error on empty input", func(t *testing.T) {
		stats, err := ParseCcacheStats([]byte(""))

		assert.NoError(t, err)
		assert.Equal(t, CcacheStats{}, stats)
	})

	t.Run("unknown lines are silently ignored", func(t *testing.T) {
		_, err := ParseCcacheStats([]byte("Future field: 99\n"))

		assert.NoError(t, err)
	})
}

func TestNewCcacheInvocation_PopulatesTopLevelMetadata(t *testing.T) {
	stats := CcacheStats{CacheableCalls: 10, CacheHit: 7}
	auth := common.CacheAuthConfig{WorkspaceID: "ws-1", AuthToken: "tok"}
	meta := common.CacheConfigMetadata{
		BitriseAppID:           "app-1",
		BitriseBuildID:         "build-1",
		BitriseStepExecutionID: "step-1",
		BitriseWorkflowName:    "primary",
		CIProvider:             "bitrise",
		CLIVersion:             "9.9.9",
		HostMetadata: common.HostMetadata{
			Hostname:       "host-1",
			Username:       "user-1",
			OS:             "darwin",
			CPUCores:       8,
			MemSize:        17179869184,
			DefaultCharset: "UTF-8",
			Locale:         "en_US",
		},
		GitMetadata: common.GitMetadata{
			CommitHash:  "deadbeef",
			Branch:      "main",
			RepoURL:     "git@example.com:org/repo.git",
			CommitEmail: "dev@example.com",
		},
		Datacenter:     "iad1",
		ExternalAppID:  "ext-app",
		ExternalBuildID: "ext-build",
	}

	invDate := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	inv := NewCcacheInvocation("child-1", "parent-1", invDate, stats, 100, 200, auth, meta)

	require.NotNil(t, inv)
	// Top-level metadata propagated from common metadata.
	assert.Equal(t, "child-1", inv.InvocationID)
	assert.Equal(t, "parent-1", inv.ParentInvocationID)
	assert.Equal(t, invDate, inv.InvocationDate)
	assert.Equal(t, "ws-1", inv.BitriseWorkspaceSlug)
	assert.Equal(t, "app-1", inv.BitriseAppSlug)
	assert.Equal(t, "build-1", inv.BitriseBuildSlug)
	assert.Equal(t, "step-1", inv.BitriseStepID)
	assert.Equal(t, "host-1", inv.Hostname)
	assert.Equal(t, "user-1", inv.Username)
	assert.Equal(t, "deadbeef", inv.CommitHash)
	assert.Equal(t, "main", inv.Branch)
	assert.Equal(t, "darwin", inv.OS)
	assert.Equal(t, "primary", inv.WorkflowName)
	assert.Equal(t, "bitrise", inv.ProviderID)
	assert.Equal(t, "9.9.9", inv.CLIVersion)
	assert.Equal(t, "ccache", inv.BuildTool)
	// Ccache-specific fields preserved.
	assert.Equal(t, stats, inv.BuildToolStats)
	assert.Equal(t, int64(100), inv.DownloadedBytes)
	assert.Equal(t, int64(200), inv.UploadedBytes)
}

func TestCcacheStats_Success(t *testing.T) {
	t.Run("no errors → success", func(t *testing.T) {
		s := CcacheStats{CacheHit: 5, CacheMiss: 1}
		assert.True(t, s.Success())
	})

	t.Run("internal error → not success", func(t *testing.T) {
		s := CcacheStats{InternalError: 1}
		assert.False(t, s.Success())
	})

	t.Run("compile failed → not success", func(t *testing.T) {
		s := CcacheStats{CompileFailed: 2}
		assert.False(t, s.Success())
	})

	t.Run("storage misses do not flip success", func(t *testing.T) {
		// remote_storage_miss / cache_miss are normal, not errors.
		s := CcacheStats{RemoteStorageMiss: 100, CacheMiss: 100}
		assert.True(t, s.Success())
	})
}

func TestCcacheStats_ErrorSummary(t *testing.T) {
	t.Run("empty when no errors", func(t *testing.T) {
		assert.Empty(t, CcacheStats{}.ErrorSummary())
	})

	t.Run("lists every non-zero error counter", func(t *testing.T) {
		s := CcacheStats{InternalError: 2, CompileFailed: 1, BadInputFile: 4}
		got := s.ErrorSummary()
		assert.Contains(t, got, "internal_error=2")
		assert.Contains(t, got, "compile_failed=1")
		assert.Contains(t, got, "bad_input_file=4")
	})
}

func TestNewCcacheInvocation_DerivesHitRateSuccessError(t *testing.T) {
	auth := common.CacheAuthConfig{}
	meta := common.CacheConfigMetadata{}

	t.Run("clean run propagates hit rate, success=true, empty error", func(t *testing.T) {
		stats := CcacheStats{
			CacheableCalls: 10,
			CacheHit:       7,
			CacheHitRate:   0.7,
		}

		inv := NewCcacheInvocation("c", "p", time.Now(), stats, 0, 0, auth, meta)

		assert.InDelta(t, 0.7, inv.HitRate, 1e-6)
		assert.True(t, inv.Success)
		assert.Empty(t, inv.Error)
	})

	t.Run("internal errors propagate as success=false + summary string", func(t *testing.T) {
		stats := CcacheStats{
			CacheableCalls: 10,
			CacheHit:       5,
			CacheHitRate:   0.5,
			InternalError:  1,
			CompileFailed:  3,
		}

		inv := NewCcacheInvocation("c", "p", time.Now(), stats, 0, 0, auth, meta)

		assert.InDelta(t, 0.5, inv.HitRate, 1e-6)
		assert.False(t, inv.Success)
		assert.Contains(t, inv.Error, "internal_error=1")
		assert.Contains(t, inv.Error, "compile_failed=3")
	})
}
