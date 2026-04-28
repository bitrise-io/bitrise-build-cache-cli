//go:build unit

package wrap

import (
	"testing"

	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/stretchr/testify/assert"
)

func TestWrap_NoWrapWhenDetectionEmpty(t *testing.T) {
	name, args := Wrap(Detection{}, "gradlew", []string{"assembleDebug"})

	assert.Equal(t, "gradlew", name)
	assert.Equal(t, []string{"assembleDebug"}, args)
}

func TestWrap_NoWrapWhenRNDisabled(t *testing.T) {
	name, args := Wrap(Detection{CLIPath: "/usr/local/bin/bitrise-build-cache"}, "xcodebuild", []string{"-project", "App.xcodeproj"})

	assert.Equal(t, "xcodebuild", name)
	assert.Equal(t, []string{"-project", "App.xcodeproj"}, args)
}

func TestWrap_RewritesWhenRNEnabled(t *testing.T) {
	det := Detection{
		CLIPath:            "/usr/local/bin/bitrise-build-cache",
		ReactNativeEnabled: true,
	}

	name, args := Wrap(det, "gradlew", []string{"assembleDebug", "-PsomeFlag"})

	assert.Equal(t, "/usr/local/bin/bitrise-build-cache", name)
	assert.Equal(t, []string{"react-native", "run", "--", "gradlew", "assembleDebug", "-PsomeFlag"}, args)
}

func TestWrap_DoesNotMutateInputArgs(t *testing.T) {
	det := Detection{
		CLIPath:            "/cli",
		ReactNativeEnabled: true,
	}
	in := []string{"a", "b"}
	original := []string{"a", "b"}

	_, _ = Wrap(det, "gradlew", in)

	assert.Equal(t, original, in, "input args must not be mutated")
}

// recordingFactory captures Create calls so the factory wrapper can be
// asserted without wiring up a real exec runner.
type recordingFactory struct {
	calls []recordedCall
}

type recordedCall struct {
	name string
	args []string
}

func (r *recordingFactory) Create(name string, args []string, _ *command.Opts) command.Command {
	r.calls = append(r.calls, recordedCall{name: name, args: append([]string{}, args...)})

	return nil
}

func TestNewWrappingCommandFactory_ReturnsInnerWhenDetectionEmpty(t *testing.T) {
	inner := &recordingFactory{}
	got := NewWrappingCommandFactory(inner, Detection{}, "xcodebuild")

	assert.Same(t, inner, got, "with empty detection the factory must be unchanged")
}

func TestNewWrappingCommandFactory_ReturnsInnerWhenNoTargetsConfigured(t *testing.T) {
	inner := &recordingFactory{}
	det := Detection{CLIPath: "/cli", ReactNativeEnabled: true}

	got := NewWrappingCommandFactory(inner, det)

	assert.Same(t, inner, got)
}

func TestNewWrappingCommandFactory_PassesThroughNonTargetCommands(t *testing.T) {
	inner := &recordingFactory{}
	det := Detection{CLIPath: "/cli", ReactNativeEnabled: true}

	f := NewWrappingCommandFactory(inner, det, "xcodebuild")
	f.Create("xcbeautify", []string{"--report"}, nil)

	if assert.Len(t, inner.calls, 1) {
		assert.Equal(t, "xcbeautify", inner.calls[0].name)
		assert.Equal(t, []string{"--report"}, inner.calls[0].args)
	}
}

func TestNewWrappingCommandFactory_RewritesTargetCommands(t *testing.T) {
	inner := &recordingFactory{}
	det := Detection{CLIPath: "/cli", ReactNativeEnabled: true}

	f := NewWrappingCommandFactory(inner, det, "xcodebuild", "gradlew")
	f.Create("xcodebuild", []string{"-project", "App.xcodeproj"}, nil)
	f.Create("gradlew", []string{"assembleDebug"}, nil)

	if assert.Len(t, inner.calls, 2) {
		assert.Equal(t, "/cli", inner.calls[0].name)
		assert.Equal(t,
			[]string{"react-native", "run", "--", "xcodebuild", "-project", "App.xcodeproj"},
			inner.calls[0].args)

		assert.Equal(t, "/cli", inner.calls[1].name)
		assert.Equal(t,
			[]string{"react-native", "run", "--", "gradlew", "assembleDebug"},
			inner.calls[1].args)
	}
}
