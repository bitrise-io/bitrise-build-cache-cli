package invoke

import (
	"context"
	"errors"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/bitrise-io/go-utils/v2/log"
	"golang.org/x/term"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/tui"
)

// defaultPrompt is the huh-backed Prompt used when no override is supplied.
type defaultPrompt struct {
	logger log.Logger

	// xcodebuildInfo sources picker candidates. nil falls back to the exec-backed
	// implementation.
	xcodebuildInfo xcodebuildInfoProvider

	// runForm is a seam so tests can drive the form without a real TTY.
	// nil falls back to tui.RunForm.
	runForm func(*huh.Group) error
}

func (p defaultPrompt) Fill(ctx context.Context, spec *InvocationSpec) error {
	// Mirror wizard_tools.go: huh accessible mode (TERM=dumb) reads from stdin
	// so a real TTY isn't required. Everything else needs one.
	if os.Getenv("TERM") != "dumb" && !term.IsTerminal(int(os.Stdin.Fd())) {
		return ErrPromptUnavailable
	}

	runForm := p.runForm
	if runForm == nil {
		runForm = func(g *huh.Group) error { return tui.RunForm(g) } //nolint:wrapcheck // tui already wraps errors
	}

	// Stage 1: container. Everything else queries xcodebuild against it.
	if spec.Workspace == "" && spec.Project == "" {
		if err := runForm(huh.NewGroup(containerField(spec))); err != nil {
			return err //nolint:wrapcheck // tui already wraps errors
		}

		normalizeContainer(spec)
	}

	provider := p.xcodebuildInfo
	if provider == nil {
		provider = execXcodebuildInfo{}
	}

	// Stage 2: scheme + configuration. Both come from the same -list -json call.
	if err := p.fillSchemeAndConfig(ctx, provider, runForm, spec); err != nil {
		return err
	}

	// Stage 3: destination. Requires scheme resolved.
	if spec.Destination == "" {
		if err := p.fillDestination(ctx, provider, runForm, spec); err != nil {
			return err
		}

		spec.Destination = strings.TrimSpace(spec.Destination)
	}

	return nil
}

// Private — helpers

func (p defaultPrompt) fillSchemeAndConfig(
	ctx context.Context,
	provider xcodebuildInfoProvider,
	runForm func(*huh.Group) error,
	spec *InvocationSpec,
) error {
	needScheme := spec.Scheme == ""
	needConfig := spec.Configuration == ""

	if !needScheme && !needConfig {
		return nil
	}

	var (
		schemes []string
		configs []string
	)

	if needScheme {
		s, err := provider.ListSchemes(ctx, spec.Workspace, spec.Project)
		if err != nil {
			p.debug("xcodebuild -list schemes: %s; falling back to free-text input", err)
		} else {
			schemes = s
		}
	}

	if needConfig {
		c, err := provider.ListConfigurations(ctx, spec.Workspace, spec.Project)
		if err != nil {
			p.debug("xcodebuild -list configurations: %s; falling back to free-text input", err)
		} else {
			configs = c
		}
	}

	fields := []huh.Field{}
	if needScheme {
		fields = append(fields, schemeField(spec, schemes))
	}

	if needConfig {
		fields = append(fields, configurationField(spec, configs))
	}

	if err := runForm(huh.NewGroup(fields...)); err != nil {
		return err //nolint:wrapcheck // tui already wraps errors
	}

	spec.Scheme = strings.TrimSpace(spec.Scheme)
	spec.Configuration = strings.TrimSpace(spec.Configuration)

	return nil
}

func (p defaultPrompt) fillDestination(
	ctx context.Context,
	provider xcodebuildInfoProvider,
	runForm func(*huh.Group) error,
	spec *InvocationSpec,
) error {
	var dests []string

	if spec.Scheme != "" {
		d, err := provider.ShowDestinations(ctx, spec.Workspace, spec.Project, spec.Scheme)
		if err != nil {
			p.debug("xcodebuild -showdestinations: %s; falling back to free-text input", err)
		} else {
			dests = d
		}
	}

	if err := runForm(huh.NewGroup(destinationField(spec, dests))); err != nil {
		return err //nolint:wrapcheck // tui already wraps errors
	}

	return nil
}

func (p defaultPrompt) debug(format string, args ...any) {
	if p.logger == nil {
		return
	}

	p.logger.Debugf(format, args...)
}

func containerField(spec *InvocationSpec) huh.Field {
	return huh.NewInput().
		Title("Workspace or project").
		Description("Filename ending in .xcworkspace or .xcodeproj").
		Validate(nonEmpty("Workspace or project")).
		Value(&spec.Workspace)
}

func schemeField(spec *InvocationSpec, candidates []string) huh.Field {
	if len(candidates) == 0 {
		return huh.NewInput().
			Title("Scheme").
			Validate(nonEmpty("Scheme")).
			Value(&spec.Scheme)
	}

	return huh.NewSelect[string]().
		Title("Scheme").
		Options(huh.NewOptions(candidates...)...).
		Height(tui.SelectHeight).
		Value(&spec.Scheme)
}

func configurationField(spec *InvocationSpec, candidates []string) huh.Field {
	if len(candidates) == 0 {
		return huh.NewInput().
			Title("Configuration").
			Description("Optional — leave empty to accept Xcode's default").
			Value(&spec.Configuration)
	}

	return huh.NewSelect[string]().
		Title("Configuration").
		Options(huh.NewOptions(candidates...)...).
		Height(tui.SelectHeight).
		Value(&spec.Configuration)
}

func destinationField(spec *InvocationSpec, candidates []string) huh.Field {
	if len(candidates) == 0 {
		return huh.NewInput().
			Title("Destination").
			Description(`xcodebuild -destination string, e.g. "platform=iOS Simulator,name=iPhone 15"`).
			Validate(nonEmpty("Destination")).
			Value(&spec.Destination)
	}

	return huh.NewSelect[string]().
		Title("Destination").
		Options(huh.NewOptions(candidates...)...).
		Height(tui.SelectHeight).
		Value(&spec.Destination)
}

func normalizeContainer(spec *InvocationSpec) {
	spec.Workspace = strings.TrimSpace(spec.Workspace)
	spec.Project = strings.TrimSpace(spec.Project)

	// If the container answer wasn't an .xcworkspace, treat it as the project field.
	if spec.Workspace != "" && !strings.HasSuffix(spec.Workspace, ".xcworkspace") {
		spec.Project = spec.Workspace
		spec.Workspace = ""
	}
}

func nonEmpty(label string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(label + " cannot be empty")
		}

		return nil
	}
}
