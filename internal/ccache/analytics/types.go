package analytics

import "time"

// CcacheStats holds all statistics parsed from `ccache --print-stats --format=json`.
// JSON tags match ccache's output keys directly.
// CacheHitRate is the only computed field (not emitted by ccache).
type CcacheStats struct {
	// Cache outcomes
	DirectCacheHit        int     `json:"direct_cache_hit"`
	PreprocessedCacheHit  int     `json:"preprocessed_cache_hit"`
	CacheMiss             int     `json:"cache_miss"`
	CacheHitRate          float64 `json:"cache_hit_rate"`
	DirectCacheMiss       int     `json:"direct_cache_miss"`
	PreprocessedCacheMiss int     `json:"preprocessed_cache_miss"`

	// Storage
	FilesInCache         int   `json:"files_in_cache"`
	CacheSizeKibibyte    int64 `json:"cache_size_kibibyte"`
	MaxCacheSizeKibibyte int64 `json:"max_cache_size_kibibyte"`
	MaxFilesInCache      int   `json:"max_files_in_cache"`
	CleanupsPerformed    int   `json:"cleanups_performed"`

	// Remote storage
	RemoteStorageHit      int `json:"remote_storage_hit"`
	RemoteStorageMiss     int `json:"remote_storage_miss"`
	RemoteStorageError    int `json:"remote_storage_error"`
	RemoteStorageTimeout  int `json:"remote_storage_timeout"`
	RemoteStorageWrite    int `json:"remote_storage_write"`
	RemoteStorageReadHit  int `json:"remote_storage_read_hit"`
	RemoteStorageReadMiss int `json:"remote_storage_read_miss"`

	// Local storage
	LocalStorageHit      int `json:"local_storage_hit"`
	LocalStorageMiss     int `json:"local_storage_miss"`
	LocalStorageReadHit  int `json:"local_storage_read_hit"`
	LocalStorageReadMiss int `json:"local_storage_read_miss"`
	LocalStorageWrite    int `json:"local_storage_write"`

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
	BadInputFile                   int `json:"bad_input_file"`
	BadOutputFile                  int `json:"bad_output_file"`
	AutoconfTest                   int `json:"autoconf_test"`
	ModifiedInputFile              int `json:"modified_input_file"`

	// Misc
	Recache               int   `json:"recache"`
	Disabled              int   `json:"disabled"`
	InternalError         int   `json:"internal_error"`
	ErrorHashingExtraFile int   `json:"error_hashing_extra_file"`
	MissingCacheFile      int   `json:"missing_cache_file"`
	StatsUpdatedTimestamp int64 `json:"stats_updated_timestamp"`
	StatsZeroedTimestamp  int64 `json:"stats_zeroed_timestamp"`
}

// Invocation is the analytics payload for the run command, sent for every execution regardless of ccache availability.
type Invocation struct {
	InvocationID         string            `json:"invocationId"`
	InvocationDate       time.Time         `json:"invocationDate"`
	BitriseOrgSlug       string            `json:"bitriseOrgSlug"`
	BitriseAppSlug       string            `json:"bitriseAppSlug"`
	BitriseBuildSlug     string            `json:"bitriseBuildSlug"`
	BitriseStepID        string            `json:"bitriseStepId"`
	Hostname             string            `json:"hostname"`
	Username             string            `json:"username"`
	CommitHash           string            `json:"commitHash"`
	Branch               string            `json:"branch"`
	RepositoryURL        string            `json:"repositoryUrl"`
	CommitEmail          string            `json:"commitEmail"`
	Command              string            `json:"command"`
	FullCommand          string            `json:"fullCommand"`
	DurationMs           int64             `json:"durationMs"`
	Success              bool              `json:"success"`
	Error                string            `json:"error"`
	WorkflowName         string            `json:"workflowName"`
	ProviderID           string            `json:"providerId"`
	CLIVersion           string            `json:"cliVersion"`
	Envs                 map[string]string `json:"envs"`
	OS                   string            `json:"os"`
	HwCPUCores           int               `json:"hwCpuCores"`
	HwMemSize            int64             `json:"hwMemSize"`
	Datacenter           string            `json:"datacenter"`
	DefaultCharset       string            `json:"defaultCharset"`
	Locale               string            `json:"locale"`
	ExternalAppID        string            `json:"externalAppId,omitempty"`
	ExternalBuildID      string            `json:"externalBuildId,omitempty"`
	ExternalWorkflowName string            `json:"externalWorkflowName,omitempty"`
}

// InvocationRelation records a parent→child relationship between two invocations.
// It is sent at the start of a child invocation before any stats are available.
type InvocationRelation struct {
	ParentInvocationID string    `json:"parentInvocationId"`
	ChildInvocationID  string    `json:"childInvocationId"`
	InvocationDate     time.Time `json:"invocationDate"`
}

// CcacheInvocation is the analytics payload for ccache statistics captured during a run.
// It references the parent Invocation and contains only ccache-specific data.
type CcacheInvocation struct {
	InvocationID       string      `json:"invocationId"`
	ParentInvocationID string      `json:"parentInvocationId"`
	InvocationDate     time.Time   `json:"invocationDate"`
	CcacheStats        CcacheStats `json:"ccacheStats"`
}
