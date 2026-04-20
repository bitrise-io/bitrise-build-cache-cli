package analytics

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

//nolint:gochecknoglobals
var (
	configLineRe = regexp.MustCompile(`^\(([^)]+)\)\s+(\S+)\s*=\s*(.*)$`)
)

// ParseCcacheStats parses the text output of `ccache -v -v -s`.
// CacheHitRate and TotalCalls are derived fields computed after parsing.
// Always returns nil — unrecognised lines are silently ignored.
func ParseCcacheStats(data []byte) (CcacheStats, error) {
	var stats CcacheStats
	var path []string

	for _, line := range strings.Split(string(data), "\n") {
		m := statsLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		level := len(m[1]) / 2
		key := m[2]
		rest := m[3]

		if level > len(path) {
			continue // skip malformed indent jumps
		}

		// Truncate to parent depth, then append current key.
		// e.g. at level 2 under ["Cacheable calls", "Hits"] → append "Direct"
		// At level 1 after ["Cacheable calls", "Hits", "Direct"] → truncate to ["Cacheable calls"], append "Misses"
		path = append(path[:level], key)
		fullKey := strings.Join(path, " / ")

		nums := extractNumbers(rest)
		var first, second float64
		if len(nums) > 0 {
			first = nums[0]
		}
		if len(nums) > 1 {
			second = nums[1]
		}

		applyStatField(&stats, fullKey, first, second)
	}

	// Derived fields.
	stats.TotalCalls = stats.CacheableCalls + stats.UncacheableCalls
	stats.CacheHit = stats.DirectCacheHit + stats.PreprocessedCacheHit

	if stats.CacheHit > 0 {
		stats.DirectCacheHitPercentage = float64(stats.DirectCacheHit) / float64(stats.CacheHit)
		stats.PreprocessedCacheHitPercentage = float64(stats.PreprocessedCacheHit) / float64(stats.CacheHit)
	}

	if stats.CacheableCalls > 0 {
		cacheable := float64(stats.CacheableCalls)
		stats.CacheHitRate = float64(stats.CacheHit) / cacheable
		stats.CacheMissRate = float64(stats.CacheMiss) / cacheable
		stats.RemoteStorageHitPercentage = float64(stats.RemoteStorageHit) / cacheable
		stats.RemoteStorageMissPercentage = float64(stats.RemoteStorageMiss) / cacheable
		stats.RemoteStorageErrorPercentage = float64(stats.RemoteStorageError) / cacheable
		stats.RemoteStorageTimeoutPercentage = float64(stats.RemoteStorageTimeout) / cacheable
	}

	return stats, nil
}

// ParseCcacheConfig parses the text output of `ccache --show-config` into a slice of CcacheConfigEntry.
// Each line has the form: (source) key = value
// Blank and malformed lines are silently ignored.
func ParseCcacheConfig(data []byte) []CcacheConfigEntry {
	lines := strings.Split(string(data), "\n")
	entries := make([]CcacheConfigEntry, 0, len(lines))
	for _, line := range lines {
		m := configLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		entries = append(entries, CcacheConfigEntry{
			Source: strings.TrimSpace(m[1]),
			Key:    strings.TrimSpace(m[2]),
			Value:  strings.TrimSpace(m[3]),
		})
	}

	return entries
}

// NewCcacheInvocation assembles a CcacheInvocation from a ccache stats snapshot and transfer byte counts.
// It references the parent Invocation via parentInvocationID and contains only ccache-specific data.
func NewCcacheInvocation(invocationID, parentInvocationID string, invocationDate time.Time, stats CcacheStats, downloadedBytes, uploadedBytes int64) *CcacheInvocation {
	return &CcacheInvocation{
		InvocationID:       invocationID,
		ParentInvocationID: parentInvocationID,
		InvocationDate:     invocationDate,
		BuildToolStats:     stats,
		DownloadedBytes:    downloadedBytes,
		UploadedBytes:      uploadedBytes,
		BuildTool:          "ccache",
	}
}

// PutCcacheInvocation sends a CcacheInvocation to the analytics backend via HTTP PUT.
func (c *Client) PutCcacheInvocation(inv CcacheInvocation) error {
	if err := c.Put(fmt.Sprintf("/v1/invocations/%s", inv.InvocationID), inv); err != nil {
		return fmt.Errorf("put ccache invocation: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Private
// ---------------------------------------------------------------------------

//nolint:gochecknoglobals
var (
	// statsLineRe captures: (indent)(key): (rest)
	// Non-greedy key matches up to the first colon.
	statsLineRe = regexp.MustCompile(`^( *)(.*?): *(.*)$`)
	numberRe    = regexp.MustCompile(`\d+(?:\.\d+)?`)
)

func extractNumbers(s string) []float64 {
	matches := numberRe.FindAllString(s, -1)
	result := make([]float64, 0, len(matches))
	for _, m := range matches {
		f, err := strconv.ParseFloat(m, 64)
		if err == nil {
			result = append(result, f)
		}
	}

	return result
}

// applyStatField maps a parsed (section / key) path to the corresponding CcacheStats field.
// Unrecognised paths are silently ignored — the field stays at zero.
func applyStatField(s *CcacheStats, key string, first, second float64) { //nolint:gocognit,gocyclo
	switch key {
	// ── Cacheable calls ──────────────────────────────────────────────────────
	case "Cacheable calls":
		s.CacheableCalls = int(first)
	case "Cacheable calls / Hits / Direct":
		s.DirectCacheHit = int(first)
	case "Cacheable calls / Hits / Preprocessed":
		s.PreprocessedCacheHit = int(first)
	case "Cacheable calls / Misses":
		s.CacheMiss = int(first)

	// ── Uncacheable calls ────────────────────────────────────────────────────
	case "Uncacheable calls":
		s.UncacheableCalls = int(first)
	case "Uncacheable calls / Called for linking":
		s.CalledForLink = int(first)
	case "Uncacheable calls / Called for preprocessing":
		s.CalledForPreprocessing = int(first)
	case "Uncacheable calls / Autoconf compile/link":
		s.AutoconfTest = int(first)
	case "Uncacheable calls / Bad compiler arguments":
		s.BadCompilerArguments = int(first)
	case "Uncacheable calls / Ccache disabled":
		s.Disabled = int(first)
	case "Uncacheable calls / Compilation failed":
		s.CompileFailed = int(first)
	case "Uncacheable calls / Compiler output file missing":
		s.CompilerProducedNoOutput = int(first)
	case "Uncacheable calls / Compiler produced empty output":
		s.CompilerProducedEmptyOutput = int(first)
	case "Uncacheable calls / Compiler produced stdout":
		s.CompilerProducedStdout = int(first)
	case "Uncacheable calls / Could not use modules":
		s.CouldNotUseModules = int(first)
	case "Uncacheable calls / Could not use precompiled header":
		s.CouldNotUsePrecompiledHeader = int(first)
	case "Uncacheable calls / Forced recache":
		s.Recache = int(first)
	case "Uncacheable calls / Multiple source files":
		s.MultipleSourceFiles = int(first)
	case "Uncacheable calls / No input file":
		s.NoInputFile = int(first)
	case "Uncacheable calls / Output to stdout":
		s.OutputToStdout = int(first)
	case "Uncacheable calls / Preprocessing failed":
		s.PreprocessorError = int(first)
	case "Uncacheable calls / Unsupported code directive":
		s.UnsupportedCodeDirective = int(first)
	case "Uncacheable calls / Unsupported compiler option":
		s.UnsupportedCompilerOption = int(first)
	case "Uncacheable calls / Unsupported environment variable":
		s.UnsupportedEnvironmentVariable = int(first)
	case "Uncacheable calls / Unsupported source encoding":
		s.UnsupportedSourceEncoding = int(first)
	case "Uncacheable calls / Unsupported source language":
		s.UnsupportedSourceLanguage = int(first)

	// ── Errors ───────────────────────────────────────────────────────────────
	case "Errors / Compiler check failed":
		s.CompilerCheckFailed = int(first)
	case "Errors / Could not find compiler":
		s.CouldNotFindCompiler = int(first)
	case "Errors / Could not read or parse input file":
		s.BadInputFile = int(first)
	case "Errors / Could not write to output file":
		s.BadOutputFile = int(first)
	case "Errors / Error hashing extra file":
		s.ErrorHashingExtraFile = int(first)
	case "Errors / Input file modified during compilation":
		s.ModifiedInputFile = int(first)
	case "Errors / Internal error":
		s.InternalError = int(first)
	case "Errors / Missing cache file":
		s.MissingCacheFile = int(first)

	// ── Local storage ────────────────────────────────────────────────────────
	case "Local storage / Cache size (GiB)":
		s.CacheSizeGiB = first
		s.MaxCacheSizeGiB = second
	case "Local storage / Files":
		s.FilesInCache = int(first)
	case "Local storage / Cleanups":
		s.CleanupsPerformed = int(first)
	case "Local storage / Hits":
		s.LocalStorageHit = int(first)
	case "Local storage / Misses":
		s.LocalStorageMiss = int(first)
	case "Local storage / Reads":
		s.LocalStorageReads = int(first)
	case "Local storage / Writes":
		s.LocalStorageWrite = int(first)

	// ── Remote storage ───────────────────────────────────────────────────────
	case "Remote storage / Hits":
		s.RemoteStorageHit = int(first)
	case "Remote storage / Misses":
		s.RemoteStorageMiss = int(first)
	case "Remote storage / Reads":
		s.RemoteStorageReads = int(first)
	case "Remote storage / Writes":
		s.RemoteStorageWrite = int(first)
	case "Remote storage / Errors":
		s.RemoteStorageError = int(first)
	case "Remote storage / Timeouts":
		s.RemoteStorageTimeout = int(first)
	}
}
