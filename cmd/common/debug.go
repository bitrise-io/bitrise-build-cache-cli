package common

// DebugEnabled reports whether debug logging is on from the passed source (a
// config or params field) OR the global -d/--debug CLI flag.
func DebugEnabled(source bool) bool { return source || IsDebugLogMode }
