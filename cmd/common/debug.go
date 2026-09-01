package common

// DebugEnabled reports whether debug logging is on from the passed source (a
// config or params field) OR the global -d/--debug CLI flag.
func DebugEnabled(source bool) bool { return source || IsDebugLogMode }

// DebugFromFlag is DebugEnabled with no config source: the caller has nothing
// persisted to consult, so only the CLI flag decides. Spelling that out keeps
// the "did someone drop a config value?" question answerable by grep.
func DebugFromFlag() bool { return DebugEnabled(false) }
