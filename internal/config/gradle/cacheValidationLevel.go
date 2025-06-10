package gradleconfig

type CacheValidationLevel string

//nolint:gochecknoglobals
var (
	CacheValidationLevelNone    CacheValidationLevel = "none"
	CacheValidationLevelWarning CacheValidationLevel = "warning"
	CacheValidationLevelError   CacheValidationLevel = "error"
)
