package analytics

import "time"

// CcacheStats holds statistics parsed from `ccache -v -v -s`.
// CacheHitRate is the only computed field (not emitted by ccache).
type CcacheStats struct {
	// Cache outcomes
	DirectCacheHit       int     `json:"direct_cache_hit"`
	PreprocessedCacheHit int     `json:"preprocessed_cache_hit"`
	CacheMiss            int     `json:"cache_miss"`
	CacheHitRate         float64 `json:"cache_hit_rate"`
	CacheableCalls       int     `json:"cacheable_calls"`
	TotalCalls           int     `json:"total_calls"`
	UncacheableCalls     int     `json:"uncacheable_calls"`

	// Local storage
	FilesInCache      int     `json:"files_in_cache"`
	CacheSizeGiB      float64 `json:"cache_size_gib"`
	MaxCacheSizeGiB   float64 `json:"max_cache_size_gib"`
	CleanupsPerformed int     `json:"cleanups_performed"`
	LocalStorageHit   int     `json:"local_storage_hit"`
	LocalStorageMiss  int     `json:"local_storage_miss"`
	LocalStorageReads int     `json:"local_storage_reads"`
	LocalStorageWrite int     `json:"local_storage_write"`

	// Remote storage
	RemoteStorageHit     int `json:"remote_storage_hit"`
	RemoteStorageMiss    int `json:"remote_storage_miss"`
	RemoteStorageError   int `json:"remote_storage_error"`
	RemoteStorageTimeout int `json:"remote_storage_timeout"`
	RemoteStorageWrite   int `json:"remote_storage_write"`
	RemoteStorageReads   int `json:"remote_storage_reads"`

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
}

// HasActivity returns true if ccache processed any compilations (hits or misses).
func (s CcacheStats) HasActivity() bool {
	return s.DirectCacheHit+s.PreprocessedCacheHit+s.CacheMiss > 0
}

// CcacheInvocation is the analytics payload for ccache statistics captured during a run.
// It references the parent Invocation and contains only ccache-specific data.
type CcacheInvocation struct {
	InvocationID       string      `json:"invocationId"`
	ParentInvocationID string      `json:"parentInvocationId"`
	InvocationDate     time.Time   `json:"invocationDate"`
	BuildToolStats     CcacheStats `json:"buildToolStats"`
	DownloadedBytes    int64       `json:"downloadedBytes"`
	UploadedBytes      int64       `json:"uploadedBytes"`
	BuildTool          string      `json:"buildTool"`
}
