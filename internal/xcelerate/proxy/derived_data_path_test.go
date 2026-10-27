//go:build unit

package proxy

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPeerDerivedDataPath_FlagWithSeparateValue(t *testing.T) {
	entries := []AncestryEntry{
		{PID: 42, Name: "swiftc"},
		{PID: 100, Name: "xcodebuild"},
	}
	resolve := func(_ context.Context, pid int) ([]string, string, error) {
		if pid != 100 {
			t.Fatalf("resolver must only be called with xcodebuild PID; got %d", pid)
		}

		return []string{"xcodebuild", "-workspace", "App.xcworkspace", "-derivedDataPath", "./DerivedData"}, "/Users/me/project", nil
	}

	got := peerDerivedDataPathCtx(context.Background(), entries, resolve)
	assert.Equal(t, "/Users/me/project/DerivedData", got)
}

func TestPeerDerivedDataPath_FlagWithEqualsForm(t *testing.T) {
	entries := []AncestryEntry{{PID: 100, Name: "xcodebuild"}}
	resolve := func(context.Context, int) ([]string, string, error) {
		return []string{"xcodebuild", "-derivedDataPath=/tmp/absolute"}, "/ignored", nil
	}

	got := peerDerivedDataPathCtx(context.Background(), entries, resolve)
	assert.Equal(t, "/tmp/absolute", got)
}

func TestPeerDerivedDataPath_AbsolutePathIgnoresCwd(t *testing.T) {
	entries := []AncestryEntry{{PID: 100, Name: "xcodebuild"}}
	resolve := func(context.Context, int) ([]string, string, error) {
		return []string{"xcodebuild", "-derivedDataPath", "/already/absolute"}, "/some/cwd", nil
	}

	assert.Equal(t, "/already/absolute", peerDerivedDataPathCtx(context.Background(), entries, resolve))
}

func TestPeerDerivedDataPath_RelativePathNoCwdFallsBackToRaw(t *testing.T) {
	entries := []AncestryEntry{{PID: 100, Name: "xcodebuild"}}
	resolve := func(context.Context, int) ([]string, string, error) {
		return []string{"xcodebuild", "-derivedDataPath", "./DD"}, "", nil
	}

	assert.Equal(t, "./DD", peerDerivedDataPathCtx(context.Background(), entries, resolve))
}

func TestPeerDerivedDataPath_NoXcodebuildAncestorIsEmpty(t *testing.T) {
	entries := []AncestryEntry{{PID: 42, Name: "swiftc"}, {PID: 7, Name: "launchd"}}
	resolve := func(context.Context, int) ([]string, string, error) {
		t.Fatal("resolver must not run without an xcodebuild entry")

		return nil, "", nil
	}

	assert.Empty(t, peerDerivedDataPathCtx(context.Background(), entries, resolve))
}

func TestPeerDerivedDataPath_NoFlagIsEmpty(t *testing.T) {
	entries := []AncestryEntry{{PID: 100, Name: "xcodebuild"}}
	resolve := func(context.Context, int) ([]string, string, error) {
		return []string{"xcodebuild", "build", "-scheme", "App"}, "/cwd", nil
	}

	assert.Empty(t, peerDerivedDataPathCtx(context.Background(), entries, resolve))
}

func TestPeerDerivedDataPath_ResolverErrorFallsThrough(t *testing.T) {
	entries := []AncestryEntry{
		{PID: 100, Name: "xcodebuild"},
		{PID: 200, Name: "xcodebuild"},
	}

	calls := 0
	resolve := func(_ context.Context, pid int) ([]string, string, error) {
		calls++
		if pid == 100 {
			return nil, "", errors.New("dead PID")
		}

		return []string{"xcodebuild", "-derivedDataPath", "/second/DD"}, "", nil
	}

	assert.Equal(t, "/second/DD", peerDerivedDataPathCtx(context.Background(), entries, resolve))
	assert.Equal(t, 2, calls)
}

func TestPeerDerivedDataPath_TrailingFlagWithoutValueIsEmpty(t *testing.T) {
	entries := []AncestryEntry{{PID: 100, Name: "xcodebuild"}}
	resolve := func(context.Context, int) ([]string, string, error) {
		return []string{"xcodebuild", "-derivedDataPath"}, "/cwd", nil
	}

	assert.Empty(t, peerDerivedDataPathCtx(context.Background(), entries, resolve))
}

func TestExtractDerivedDataFlag_LastWinsForEqualsForm(t *testing.T) {
	// Two -derivedDataPath flags: xcodebuild accepts only the first; mirror that.
	got := extractDerivedDataFlag([]string{"-derivedDataPath", "/first", "-derivedDataPath=/second"})
	assert.Equal(t, "/first", got)
}
