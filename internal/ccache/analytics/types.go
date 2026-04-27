package analytics

import (
	"fmt"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/analytics/multiplatform"
)

// CcacheStats holds statistics parsed from `ccache -v -v -s`.
// CacheHitRate is the only computed field (not emitted by ccache).
type CcacheStats struct {
	// Diagnostic main                              // UI usage
	TotalCalls       int `json:"total_calls"`       // `Total calls`
	CacheableCalls   int `json:"cacheable_calls"`   // `Cacheable calls` - Used as the denominator in `Cache hits & misses` -> `Cache hits`, `Cache misses` and `Remote storage` -> `Hits`, `Misses`, `Errors`, `Timeouts`
	UncacheableCalls int `json:"uncacheable_calls"` // `UncacheableCalls`

	// Cache hits & misses
	CacheHit                       int     `json:"cache_hit"`                         // `Cache hits & misses` -> `Cache hits` - Used as the denominator in `Cache hits & misses` -> `Direct hits` and `Preprocessed hits`
	CacheHitRate                   float64 `json:"cache_hit_rate"`                    // `Cache hits & misses` -> `Cache hits` percentage
	DirectCacheHit                 int     `json:"direct_cache_hit"`                  // `Cache hits & misses` -> `Direct hits`
	DirectCacheHitPercentage       float64 `json:"direct_cache_hit_percentage"`       // `Cache hits & misses` -> `Direct hits` percentage
	PreprocessedCacheHit           int     `json:"preprocessed_cache_hit"`            // `Cache hits & misses` -> `Preprocessed hits`
	PreprocessedCacheHitPercentage float64 `json:"preprocessed_cache_hit_percentage"` // `Cache hits & misses` -> `Preprocessed hits` percentage
	CacheMiss                      int     `json:"cache_miss"`                        // `Cache hits & misses` -> `Cache misses`
	CacheMissRate                  float64 `json:"cache_miss_rate"`                   // `Cache hits & misses` -> `Cache misses` percentage

	// Remote storage
	RemoteStorageHit               int     `json:"remote_storage_hit"`                // `Remote storage` -> `Hits`
	RemoteStorageHitPercentage     float64 `json:"remote_storage_hit_percentage"`     // `Remote storage` -> `Hits` percentage
	RemoteStorageMiss              int     `json:"remote_storage_miss"`               // `Remote storage` -> `Misses`
	RemoteStorageMissPercentage    float64 `json:"remote_storage_miss_percentage"`    // `Remote storage` -> `Misses` percentage
	RemoteStorageError             int     `json:"remote_storage_error"`              // `Remote storage` -> `Errors`
	RemoteStorageErrorPercentage   float64 `json:"remote_storage_error_percentage"`   // `Remote storage` -> `Errors` percentage
	RemoteStorageTimeout           int     `json:"remote_storage_timeout"`            // `Remote storage` -> `Timeouts`
	RemoteStorageTimeoutPercentage float64 `json:"remote_storage_timeout_percentage"` // `Remote storage` -> `Timeouts` percentage
	RemoteStorageWrite             int     `json:"remote_storage_write"`
	RemoteStorageReads             int     `json:"remote_storage_reads"`

	// Local storage
	FilesInCache      int     `json:"files_in_cache"`
	CacheSizeGiB      float64 `json:"cache_size_gib"`
	MaxCacheSizeGiB   float64 `json:"max_cache_size_gib"`
	CleanupsPerformed int     `json:"cleanups_performed"`
	LocalStorageHit   int     `json:"local_storage_hit"`
	LocalStorageMiss  int     `json:"local_storage_miss"`
	LocalStorageReads int     `json:"local_storage_reads"`
	LocalStorageWrite int     `json:"local_storage_write"`

	// Compiler errors and unsupported inputs
	CompileFailed                int `json:"compile_failed"`
	CompilerCheckFailed          int `json:"compiler_check_failed"`
	CompilerProducedEmptyOutput  int `json:"compiler_produced_empty_output"`
	CompilerProducedNoOutput     int `json:"compiler_produced_no_output"`
	CompilerProducedStdout       int `json:"compiler_produced_stdout"`
	PreprocessorError            int `json:"preprocessor_error"`
	CouldNotFindCompiler         int `json:"could_not_find_compiler"`
	CouldNotUseModules           int `json:"could_not_use_modules"`
	CouldNotUsePrecompiledHeader int `json:"could_not_use_precompiled_header"`
	BadInputFile                 int `json:"bad_input_file"`
	BadOutputFile                int `json:"bad_output_file"`
	ModifiedInputFile            int `json:"modified_input_file"`

	// Skipped / non-compilations
	CalledForLink                  int `json:"called_for_link"`
	CalledForPreprocessing         int `json:"called_for_preprocessing"`
	UnsupportedCodeDirective       int `json:"unsupported_code_directive"`
	UnsupportedCompilerOption      int `json:"unsupported_compiler_option"`
	UnsupportedEnvironmentVariable int `json:"unsupported_environment_variable"`
	UnsupportedSourceEncoding      int `json:"unsupported_source_encoding"`
	UnsupportedSourceLanguage      int `json:"unsupported_source_language"`
	MultipleSourceFiles            int `json:"multiple_source_files"`
	NoInputFile                    int `json:"no_input_file"`
	OutputToStdout                 int `json:"output_to_stdout"`
	BadCompilerArguments           int `json:"bad_compiler_arguments"`
	AutoconfTest                   int `json:"autoconf_test"`
	Recache                        int `json:"recache"`
	Disabled                       int `json:"disabled"`

	// Misc errors
	InternalError         int `json:"internal_error"`
	ErrorHashingExtraFile int `json:"error_hashing_extra_file"`
	MissingCacheFile      int `json:"missing_cache_file"`

	Config []CcacheConfigEntry `json:"config"`
}

// HasActivity returns true if ccache processed any compilations (hits or misses).
func (s CcacheStats) HasActivity() bool {
	return s.CacheableCalls+s.UncacheableCalls > 0
}

// Success reports whether ccache did its job without internal/IO/compile errors
// during the run. Storage hits/misses are normal and do NOT affect Success —
// only conditions that indicate ccache itself misbehaved or the user's build
// hit a hard error are counted. The intent is the same as the Success bool on
// the wrapper invocation: did the tool perform its task correctly?
func (s CcacheStats) Success() bool {
	return s.errorCount() == 0
}

// ErrorSummary returns a comma-separated `field=count` list of every non-zero
// error counter, or an empty string when ccache reported no errors. Designed
// for the multiplatform invocation's `error` field so the BE can surface a
// short reason string without re-implementing the parser.
func (s CcacheStats) ErrorSummary() string {
	parts := make([]string, 0, errorFieldCount)
	for _, f := range errorFields(s) {
		if f.count > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", f.name, f.count))
		}
	}

	return strings.Join(parts, ", ")
}

// errorFieldCount is len(errorFields(...)) — kept in sync with the helper
// to size the slice in ErrorSummary without re-counting at every call.
const errorFieldCount = 10

func (s CcacheStats) errorCount() int {
	total := 0
	for _, f := range errorFields(s) {
		total += f.count
	}

	return total
}

type errorField struct {
	name  string
	count int
}

func errorFields(s CcacheStats) []errorField {
	return []errorField{
		{"internal_error", s.InternalError},
		{"compile_failed", s.CompileFailed},
		{"compiler_check_failed", s.CompilerCheckFailed},
		{"preprocessor_error", s.PreprocessorError},
		{"could_not_find_compiler", s.CouldNotFindCompiler},
		{"bad_input_file", s.BadInputFile},
		{"bad_output_file", s.BadOutputFile},
		{"error_hashing_extra_file", s.ErrorHashingExtraFile},
		{"missing_cache_file", s.MissingCacheFile},
		{"modified_input_file", s.ModifiedInputFile},
	}
}

// CcacheConfigEntry is a single configuration key-value pair from `ccache --show-config`,
// annotated with its source (e.g. "default" or the path of the config file that set it).
type CcacheConfigEntry struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

// CcacheInvocation is the analytics payload for ccache statistics captured during a run.
// It embeds multiplatform.Invocation so the BE receives the full set of CI / host /
// repository metadata at the top level (the BE does not unwrap fields nested under
// `buildToolStats`). The ccache-specific fields live alongside the embedded ones.
type CcacheInvocation struct {
	multiplatform.Invocation

	ParentInvocationID string      `json:"parentInvocationId"`
	BuildToolStats     CcacheStats `json:"buildToolStats"`
	DownloadedBytes    int64       `json:"downloadedBytes"`
	UploadedBytes      int64       `json:"uploadedBytes"`
}
