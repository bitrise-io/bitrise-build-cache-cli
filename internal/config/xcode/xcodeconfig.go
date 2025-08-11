package xcode

type Xcode struct {
	ProxyVersion           string `json:"proxy_version"`
	WrapperVersion         string `json:"wrapper_version"`
	OriginalXcodebuildPath string `json:"original_xcodebuild_path"`
	BuildCacheEnabled      bool   `json:"build_cache_enabled"`
}
