package analytics

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ParseCcacheStats parses the text output of `ccache -v -v -s`.
// CacheHitRate and TotalCalls are derived fields computed after parsing.
// Always returns nil — unrecognised lines are silently ignored.
func ParseCcacheStats(data []byte) (CcacheStats, error) {
	var stats CcacheStats
	lines := strings.Split(string(data), "\n")

	// sectionParents[level] = section name at that indent level (2 spaces = 1 level).
	const maxDepth = 6
	sectionParents := make([]string, maxDepth)

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		indent := countLeadingSpaces(line)
		level := indent / 2
		if level >= maxDepth {
			continue
		}

		trimmed := strings.TrimSpace(line)
		key, rest, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		rest = strings.TrimSpace(rest)

		// Clear deeper levels (stale from a previous branch at this depth).
		for i := range maxDepth - level - 1 {
			sectionParents[level+1+i] = ""
		}
		sectionParents[level] = key

		// Build full path from ancestor sections + current key.
		parts := make([]string, 0, level+1)
		for i := range level {
			if sectionParents[i] != "" {
				parts = append(parts, sectionParents[i])
			}
		}
		parts = append(parts, key)
		fullKey := strings.Join(parts, " / ")

		nums := extractNumbers(rest)
		first := 0.0
		second := 0.0
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

	total := stats.DirectCacheHit + stats.PreprocessedCacheHit + stats.CacheMiss
	if total > 0 {
		stats.CacheHitRate = float64(stats.DirectCacheHit+stats.PreprocessedCacheHit) / float64(total)
	}

	return stats, nil
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
var numberRe = regexp.MustCompile(`\d+(?:\.\d+)?`)

func countLeadingSpaces(s string) int {
	n := 0
	for _, ch := range s {
		if ch != ' ' {
			break
		}
		n++
	}

	return n
}

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
