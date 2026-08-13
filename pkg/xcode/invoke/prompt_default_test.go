//go:build unit

package invoke

import (
	"context"
	"errors"
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// huh.Group has no public Fields() accessor, so these tests inspect picker
// behaviour via the provider mock's call log and via direct assertions on the
// per-field builders (schemeField / configurationField / destinationField).

func Test_defaultPrompt_Fill_UsesSchemeListFromProvider(t *testing.T) {
	provider := &xcodebuildInfoProviderMock{
		ListSchemesFunc: func(_ context.Context, ws, proj string) ([]string, error) {
			assert.Equal(t, "App.xcworkspace", ws)
			assert.Empty(t, proj)

			return []string{"App", "AppTests"}, nil
		},
	}

	t.Setenv("TERM", "dumb")

	spec := &InvocationSpec{
		Workspace:     "App.xcworkspace",
		Configuration: "Debug",
		Destination:   "generic/platform=iOS",
	}

	prompt := defaultPrompt{
		xcodebuildInfo: provider,
		runForm:        func(*huh.Group) error { return nil },
	}

	require.NoError(t, prompt.Fill(context.Background(), spec))
	assert.Len(t, provider.ListSchemesCalls(), 1)
	assert.Empty(t, provider.ListConfigurationsCalls(), "config already set — provider must not be queried")
	assert.Empty(t, provider.ShowDestinationsCalls(), "destination already set — provider must not be queried")

	// Direct builder assertion: with candidates → Select.
	f := schemeField(&InvocationSpec{}, []string{"App", "AppTests"})
	_, ok := f.(*huh.Select[string])
	assert.True(t, ok, "schemeField with candidates must return *huh.Select[string]")
}

func Test_defaultPrompt_Fill_FallsBackToInputWhenListEmpty(t *testing.T) {
	provider := &xcodebuildInfoProviderMock{
		ListSchemesFunc: func(context.Context, string, string) ([]string, error) {
			return nil, nil
		},
	}

	t.Setenv("TERM", "dumb")

	spec := &InvocationSpec{
		Workspace:     "App.xcworkspace",
		Configuration: "Debug",
		Destination:   "generic/platform=iOS",
	}

	prompt := defaultPrompt{
		xcodebuildInfo: provider,
		runForm:        func(*huh.Group) error { return nil },
	}

	require.NoError(t, prompt.Fill(context.Background(), spec))
	assert.Len(t, provider.ListSchemesCalls(), 1)

	// Direct builder assertion: empty candidates → Input.
	f := schemeField(&InvocationSpec{}, nil)
	_, ok := f.(*huh.Input)
	assert.True(t, ok, "schemeField with no candidates must return *huh.Input")
}

func Test_defaultPrompt_Fill_SkipsFieldsAlreadyInSpec(t *testing.T) {
	provider := &xcodebuildInfoProviderMock{}

	t.Setenv("TERM", "dumb")

	spec := &InvocationSpec{
		Workspace:     "App.xcworkspace",
		Scheme:        "App",
		Configuration: "Debug",
		Destination:   "generic/platform=iOS",
	}

	prompt := defaultPrompt{
		xcodebuildInfo: provider,
		runForm: func(*huh.Group) error {
			t.Fatal("runForm must not be called when nothing is missing")

			return nil
		},
	}

	require.NoError(t, prompt.Fill(context.Background(), spec))
	assert.Empty(t, provider.ListSchemesCalls())
	assert.Empty(t, provider.ListConfigurationsCalls())
	assert.Empty(t, provider.ShowDestinationsCalls())
}

func Test_defaultPrompt_Fill_QueriesShowDestinationsAfterSchemeResolved(t *testing.T) {
	var order []string

	provider := &xcodebuildInfoProviderMock{
		ListSchemesFunc: func(context.Context, string, string) ([]string, error) {
			order = append(order, "ListSchemes")

			return []string{"App"}, nil
		},
		ListConfigurationsFunc: func(context.Context, string, string) ([]string, error) {
			order = append(order, "ListConfigurations")

			return []string{"Debug"}, nil
		},
		ShowDestinationsFunc: func(_ context.Context, ws, proj, scheme string) ([]string, error) {
			order = append(order, "ShowDestinations:"+scheme)
			assert.Equal(t, "App.xcworkspace", ws)
			assert.Empty(t, proj)

			return []string{"platform=iOS Simulator,name=iPhone 15"}, nil
		},
	}

	t.Setenv("TERM", "dumb")

	spec := &InvocationSpec{Workspace: "App.xcworkspace"}

	// huh's Options() eagerly writes the first option to the bound pointer at
	// field-construction time. That gives our fake runForm nothing to do — the
	// spec is already "populated" by the time it fires. Fill still routes
	// through both stages, so provider calls are what tests assert on.
	prompt := defaultPrompt{
		xcodebuildInfo: provider,
		runForm:        func(*huh.Group) error { return nil },
	}

	require.NoError(t, prompt.Fill(context.Background(), spec))

	require.Len(t, order, 3)
	assert.Contains(t, order[:2], "ListSchemes", "stage 2 queries schemes")
	assert.Contains(t, order[:2], "ListConfigurations", "stage 2 queries configurations")
	assert.Equal(t, "ShowDestinations:App", order[2], "stage 3 queries destinations with the resolved scheme")

	assert.Equal(t, "App", spec.Scheme)
	assert.Equal(t, "Debug", spec.Configuration)
	assert.Equal(t, "platform=iOS Simulator,name=iPhone 15", spec.Destination)
}

func Test_defaultPrompt_Fill_ProviderErrorFallsBackSilently(t *testing.T) {
	provider := &xcodebuildInfoProviderMock{
		ListSchemesFunc: func(context.Context, string, string) ([]string, error) {
			return nil, errors.New("xcodebuild missing")
		},
	}

	t.Setenv("TERM", "dumb")

	spec := &InvocationSpec{
		Workspace:     "App.xcworkspace",
		Configuration: "Debug",
		Destination:   "generic/platform=iOS",
	}

	runFormCalled := false
	prompt := defaultPrompt{
		xcodebuildInfo: provider,
		runForm: func(*huh.Group) error {
			runFormCalled = true

			return nil
		},
	}

	require.NoError(t, prompt.Fill(context.Background(), spec))
	assert.True(t, runFormCalled, "runForm still fires with the free-text fallback field")
}

func Test_defaultPrompt_Fill_PromptUnavailable_whenNoTTY(t *testing.T) {
	t.Setenv("TERM", "not-dumb")

	prompt := defaultPrompt{}
	err := prompt.Fill(context.Background(), &InvocationSpec{Workspace: "App.xcworkspace"})
	require.ErrorIs(t, err, ErrPromptUnavailable)
}

func Test_normalizeContainer_treatsNonWorkspaceAsProject(t *testing.T) {
	spec := &InvocationSpec{Workspace: "App.xcodeproj"}
	normalizeContainer(spec)
	assert.Empty(t, spec.Workspace)
	assert.Equal(t, "App.xcodeproj", spec.Project)
}

func Test_normalizeContainer_keepsWorkspaceSuffix(t *testing.T) {
	spec := &InvocationSpec{Workspace: "App.xcworkspace"}
	normalizeContainer(spec)
	assert.Equal(t, "App.xcworkspace", spec.Workspace)
	assert.Empty(t, spec.Project)
}

func Test_destinationField_isSelectWhenCandidatesProvided(t *testing.T) {
	f := destinationField(&InvocationSpec{}, []string{"platform=iOS,name=iPhone 15"})
	_, ok := f.(*huh.Select[string])
	assert.True(t, ok)
}

func Test_destinationField_fallsBackToInput(t *testing.T) {
	f := destinationField(&InvocationSpec{}, nil)
	_, ok := f.(*huh.Input)
	assert.True(t, ok)
}

func Test_configurationField_isSelectWhenCandidatesProvided(t *testing.T) {
	f := configurationField(&InvocationSpec{}, []string{"Debug", "Release"})
	_, ok := f.(*huh.Select[string])
	assert.True(t, ok)
}
