//go:build unit

package interactive

import (
	"context"
	"errors"
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/xcode/invoke"
)

// huh.Group has no public Fields() accessor, so these tests inspect picker
// behaviour via the provider mock's call log and via direct assertions on the
// per-field builders (schemeField / configurationField / destinationField).

func Test_Fill_UsesSchemeListFromProvider(t *testing.T) {
	provider := &XcodebuildInfoProviderMock{
		ListSchemesAndConfigurationsFunc: func(_ context.Context, ws, proj string) ([]string, []string, error) {
			assert.Equal(t, "App.xcworkspace", ws)
			assert.Empty(t, proj)

			return []string{"App", "AppTests"}, nil, nil
		},
	}

	t.Setenv("TERM", "dumb")

	spec := invoke.InvocationSpec{
		Workspace:     "App.xcworkspace",
		Configuration: "Debug",
		Destination:   "generic/platform=iOS",
	}

	prompter := Prompter{
		XcodebuildInfo: provider,
		RunForm:        func(*huh.Group) error { return nil },
	}

	_, err := prompter.Fill(context.Background(), spec, "")
	require.NoError(t, err)
	assert.Len(t, provider.ListSchemesAndConfigurationsCalls(), 1)
	assert.Empty(t, provider.ShowDestinationsCalls(), "destination already set — provider must not be queried")

	// Direct builder assertion: with candidates → Select.
	f := schemeField(&invoke.InvocationSpec{}, []string{"App", "AppTests"})
	_, ok := f.(*huh.Select[string])
	assert.True(t, ok, "schemeField with candidates must return *huh.Select[string]")
}

func Test_Fill_FallsBackToInputWhenListEmpty(t *testing.T) {
	provider := &XcodebuildInfoProviderMock{
		ListSchemesAndConfigurationsFunc: func(context.Context, string, string) ([]string, []string, error) {
			return nil, nil, nil
		},
	}

	t.Setenv("TERM", "dumb")

	spec := invoke.InvocationSpec{
		Workspace:     "App.xcworkspace",
		Configuration: "Debug",
		Destination:   "generic/platform=iOS",
	}

	prompter := Prompter{
		XcodebuildInfo: provider,
		RunForm:        func(*huh.Group) error { return nil },
	}

	_, err := prompter.Fill(context.Background(), spec, "")
	require.NoError(t, err)
	assert.Len(t, provider.ListSchemesAndConfigurationsCalls(), 1)

	// Direct builder assertion: empty candidates → Input.
	f := schemeField(&invoke.InvocationSpec{}, nil)
	_, ok := f.(*huh.Input)
	assert.True(t, ok, "schemeField with no candidates must return *huh.Input")
}

func Test_Fill_SkipsFieldsAlreadyInSpec(t *testing.T) {
	provider := &XcodebuildInfoProviderMock{}

	t.Setenv("TERM", "dumb")

	spec := invoke.InvocationSpec{
		Workspace:     "App.xcworkspace",
		Scheme:        "App",
		Configuration: "Debug",
		Destination:   "generic/platform=iOS",
	}

	prompter := Prompter{
		XcodebuildInfo: provider,
		RunForm: func(*huh.Group) error {
			t.Fatal("runForm must not be called when nothing is missing")

			return nil
		},
	}

	_, err := prompter.Fill(context.Background(), spec, "")
	require.NoError(t, err)
	assert.Empty(t, provider.ListSchemesAndConfigurationsCalls())
	assert.Empty(t, provider.ShowDestinationsCalls())
}

func Test_Fill_QueriesShowDestinationsAfterSchemeResolved(t *testing.T) {
	var order []string

	provider := &XcodebuildInfoProviderMock{
		ListSchemesAndConfigurationsFunc: func(context.Context, string, string) ([]string, []string, error) {
			order = append(order, "ListSchemesAndConfigurations")

			return []string{"App"}, []string{"Debug"}, nil
		},
		ShowDestinationsFunc: func(_ context.Context, ws, proj, scheme string) ([]string, error) {
			order = append(order, "ShowDestinations:"+scheme)
			assert.Equal(t, "App.xcworkspace", ws)
			assert.Empty(t, proj)

			return []string{"platform=iOS Simulator,name=iPhone 15"}, nil
		},
	}

	t.Setenv("TERM", "dumb")

	spec := invoke.InvocationSpec{Workspace: "App.xcworkspace"}

	// huh's Options() eagerly writes the first option to the bound pointer at
	// field-construction time. That gives our fake runForm nothing to do — the
	// spec is already "populated" by the time it fires. Fill still routes
	// through both stages, so provider calls are what tests assert on.
	prompter := Prompter{
		XcodebuildInfo: provider,
		RunForm:        func(*huh.Group) error { return nil },
	}

	got, err := prompter.Fill(context.Background(), spec, "")
	require.NoError(t, err)

	require.Len(t, order, 2)
	assert.Equal(t, "ListSchemesAndConfigurations", order[0], "scheme + configuration share one xcodebuild -list call")
	assert.Equal(t, "ShowDestinations:App", order[1], "destination query runs after scheme resolved")

	assert.Equal(t, "App", got.Scheme)
	assert.Equal(t, "Debug", got.Configuration)
	assert.Equal(t, "platform=iOS Simulator,name=iPhone 15", got.Destination)
}

func Test_Fill_ProviderErrorFallsBackSilently(t *testing.T) {
	provider := &XcodebuildInfoProviderMock{
		ListSchemesAndConfigurationsFunc: func(context.Context, string, string) ([]string, []string, error) {
			return nil, nil, errors.New("xcodebuild missing")
		},
	}

	t.Setenv("TERM", "dumb")

	spec := invoke.InvocationSpec{
		Workspace:     "App.xcworkspace",
		Configuration: "Debug",
		Destination:   "generic/platform=iOS",
	}

	runFormCalled := false
	prompter := Prompter{
		XcodebuildInfo: provider,
		RunForm: func(*huh.Group) error {
			runFormCalled = true

			return nil
		},
	}

	_, err := prompter.Fill(context.Background(), spec, "")
	require.NoError(t, err)
	assert.True(t, runFormCalled, "runForm still fires with the free-text fallback field")
}

func Test_Fill_PromptUnavailable_whenNoTTY(t *testing.T) {
	t.Setenv("TERM", "not-dumb")

	spec := invoke.InvocationSpec{Workspace: "App.xcworkspace"}
	got, err := Prompter{}.Fill(context.Background(), spec, "")
	require.ErrorIs(t, err, ErrPromptUnavailable)
	assert.Equal(t, spec, got, "spec must not be mutated when no TTY is available")
}

func Test_Fill_PreservesExtraArgs(t *testing.T) {
	provider := &XcodebuildInfoProviderMock{
		ListSchemesAndConfigurationsFunc: func(context.Context, string, string) ([]string, []string, error) {
			return []string{"App"}, nil, nil
		},
	}

	t.Setenv("TERM", "dumb")

	spec := invoke.InvocationSpec{
		Workspace:   "App.xcworkspace",
		Destination: "generic/platform=iOS",
		ExtraArgs:   []string{"-quiet", "OTHER_LDFLAGS=-lfoo"},
	}

	got, err := Prompter{
		XcodebuildInfo: provider,
		RunForm:        func(*huh.Group) error { return nil },
	}.Fill(context.Background(), spec, "")
	require.NoError(t, err)
	assert.Equal(t, spec.ExtraArgs, got.ExtraArgs, "ExtraArgs must survive the picker")
}

func Test_normalizeContainer_treatsNonWorkspaceAsProject(t *testing.T) {
	spec := &invoke.InvocationSpec{Workspace: "App.xcodeproj"}
	normalizeContainer(spec)
	assert.Empty(t, spec.Workspace)
	assert.Equal(t, "App.xcodeproj", spec.Project)
}

func Test_normalizeContainer_keepsWorkspaceSuffix(t *testing.T) {
	spec := &invoke.InvocationSpec{Workspace: "App.xcworkspace"}
	normalizeContainer(spec)
	assert.Equal(t, "App.xcworkspace", spec.Workspace)
	assert.Empty(t, spec.Project)
}

func Test_destinationField_isSelectWhenCandidatesProvided(t *testing.T) {
	f := destinationField(&invoke.InvocationSpec{}, []string{"platform=iOS,name=iPhone 15"})
	_, ok := f.(*huh.Select[string])
	assert.True(t, ok)
}

func Test_destinationField_fallsBackToInput(t *testing.T) {
	f := destinationField(&invoke.InvocationSpec{}, nil)
	_, ok := f.(*huh.Input)
	assert.True(t, ok)
}

func Test_configurationField_isSelectWhenCandidatesProvided(t *testing.T) {
	f := configurationField(&invoke.InvocationSpec{}, []string{"Debug", "Release"})
	_, ok := f.(*huh.Select[string])
	assert.True(t, ok)
}
