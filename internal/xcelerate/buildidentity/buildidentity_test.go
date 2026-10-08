//go:build unit

package buildidentity

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDerive_IsStableForFixedAnchor(t *testing.T) {
	a := Anchor{Hostname: "host", PID: 1234, StartTimeMS: 1_700_000_000_000}

	got1 := Derive(a)
	got2 := Derive(a)
	assert.Equal(t, got1, got2)

	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`), got1)
}

func TestDerive_DifferentAnchorsDifferIDs(t *testing.T) {
	base := Anchor{Hostname: "host", PID: 1234, StartTimeMS: 1_700_000_000_000}

	other := base
	other.PID = 1235

	assert.NotEqual(t, Derive(base), Derive(other))
}

func TestAnchorFromAncestry_MatchesAnchorableName(t *testing.T) {
	ancestry := []AncestryEntry{
		{PID: 100, Name: "swiftc", StartTimeMS: 1},
		{PID: 101, Name: "xcodebuild", StartTimeMS: 2},
		{PID: 102, Name: "zsh", StartTimeMS: 3},
	}

	got, ok := AnchorFromAncestry("host", ancestry)
	assert.True(t, ok)
	assert.Equal(t, 101, got.PID)
	assert.Equal(t, int64(2), got.StartTimeMS)
	assert.Equal(t, "host", got.Hostname)
}

func TestAnchorFromAncestry_SWBBuildServiceMatches(t *testing.T) {
	ancestry := []AncestryEntry{
		{PID: 200, Name: "clang", StartTimeMS: 1},
		{PID: 201, Name: "SWBBuildService", StartTimeMS: 2},
	}

	got, ok := AnchorFromAncestry("host", ancestry)
	assert.True(t, ok)
	assert.Equal(t, 201, got.PID)
}

func TestAnchorFromAncestry_NoMatchReturnsFalse(t *testing.T) {
	ancestry := []AncestryEntry{
		{PID: 100, Name: "someRandomProbe", StartTimeMS: 1},
		{PID: 101, Name: "sh", StartTimeMS: 2},
	}

	_, ok := AnchorFromAncestry("host", ancestry)
	assert.False(t, ok, "AnchorFromAncestry must NOT fall back to the peer PID — callers would overcount probes")
}

// Guards the invariant that proxy-only ancestry (which also contains
// xcodebuild in test fixtures where the proxy was spawned by it) must never
// be fed into this helper from the proxy's own PID chain. The test name
// documents the contract — the caller side (connection_session.go) must only
// pass peer ancestry.
func TestAnchorFromAncestry_UsingProxyAncestryWouldOvercount(t *testing.T) {
	ancestry := []AncestryEntry{
		{PID: 1000, Name: "bitrise-build-cache", StartTimeMS: 1},
		{PID: 1001, Name: "xcodebuild", StartTimeMS: 2}, // irrelevant to the current peer!
	}

	// If a caller mistakenly passes proxy ancestry, this helper still fires —
	// which is precisely why the proxy-side invariant exists.
	anchor, ok := AnchorFromAncestry("host", ancestry)
	assert.True(t, ok)
	assert.Equal(t, 1001, anchor.PID, "the helper has no way to tell proxy vs peer apart; callers must gate")
}
