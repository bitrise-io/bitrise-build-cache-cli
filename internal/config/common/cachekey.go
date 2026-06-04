package common

import "strings"

// SanitizeCacheKeyComponent makes a dynamic value safe to embed in a build
// cache resource name.
//
// The KV backend addresses entries by a "kv/<key>" resource name and segments
// that name on '/'. A key component that itself contains a slash — most
// commonly a git branch such as "renovate/all-non-major-updates" — therefore
// pushes everything after the slash (including the trailing per-OS suffix) into
// a separate resource-name segment that the backend does not treat as part of
// the key. The result is that the otherwise-distinct "...-linux" and
// "...-darwin" keys collapse onto the same entry: whichever OS saves last wins,
// and a restore on the other OS receives wrong-OS metadata (which then fails
// with "no applicable metadata found with compatible OS"). Branches without a
// slash (e.g. "main") never collide, which is why the symptom only appears on
// feature/PR branches.
//
// Replacing '/' with '_' keeps each component inside a single resource-name
// segment so branch- and OS-scoped keys stay distinct.
func SanitizeCacheKeyComponent(s string) string {
	return strings.ReplaceAll(s, "/", "_")
}
