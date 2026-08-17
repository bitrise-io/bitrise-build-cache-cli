// Package interactive contains the huh-based picker UI for filling in an
// xcodebuild invocation spec. It lives under cmd/ so the pkg/xcode/invoke
// resolver stays free of TUI concerns.
package interactive

import (
	"context"
	"errors"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/bitrise-io/go-utils/v2/log"
	"golang.org/x/term"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/tui"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/xcode/invoke"
)

// ErrPromptUnavailable signals a prompt was needed but no interactive TTY was
// available. Callers wrap this with the resolved config path so the user knows
// where to edit the file by hand.
var ErrPromptUnavailable = errors.New("prompt unavailable: cannot request missing invocation fields")

// Prompter fills any missing fields on an invoke.InvocationSpec by walking the
// user through a huh form. Nil fields fall back to production defaults.
type Prompter struct {
	Logger log.Logger

	// XcodebuildInfo sources picker candidates. nil falls back to the exec-backed
	// implementation.
	XcodebuildInfo XcodebuildInfoProvider

	// RunForm is a seam so tests can drive the form without a real TTY.
	// nil falls back to tui.RunForm.
	RunForm func(*huh.Group) error
}

// Fill walks the user through a picker for whichever fields on spec are still
// blank. projectPath is the directory containing the .xcworkspace / .xcodeproj
// — it is used as the working directory when the exec-backed picker calls
// xcodebuild, so bare workspace/project filenames resolve. Pass "" when there
// is no project directory yet.
//
// The returned spec is a copy — ExtraArgs are preserved as-is. When no TTY is
// available, Fill returns ErrPromptUnavailable and leaves spec unchanged.
//
// End-to-end flow:
//
//	spec, cfg, err := r.Resolve(ctx, cmd, repoRoot)
//	if err != nil { ... }
//	if !spec.IsComplete() {
//	    spec, err = prompter.Fill(ctx, spec, filepath.Dir(cfg))
//	    if err != nil { ... }
//	}
//	_ = r.Persist(cfg, spec)
func (p Prompter) Fill(ctx context.Context, spec invoke.InvocationSpec, projectPath string) (invoke.InvocationSpec, error) {
	// Mirror wizard_tools.go: huh accessible mode (TERM=dumb) reads from stdin
	// so a real TTY isn't required. Everything else needs one.
	if os.Getenv("TERM") != "dumb" && !term.IsTerminal(int(os.Stdin.Fd())) {
		return spec, ErrPromptUnavailable
	}

	runForm := p.RunForm
	if runForm == nil {
		runForm = func(g *huh.Group) error { return tui.RunForm(g) } //nolint:wrapcheck // tui already wraps errors
	}

	if spec.Workspace == "" && spec.Project == "" {
		if err := runForm(huh.NewGroup(containerField(&spec))); err != nil {
			return spec, err //nolint:wrapcheck // tui already wraps errors
		}

		normalizeContainer(&spec)
	}

	provider := p.XcodebuildInfo
	if provider == nil {
		provider = execXcodebuildInfo{WorkDir: projectPath}
	}

	if err := p.fillSchemeAndConfig(ctx, provider, runForm, &spec); err != nil {
		return spec, err
	}

	if spec.Destination == "" {
		if err := p.fillDestination(ctx, provider, runForm, &spec); err != nil {
			return spec, err
		}

		spec.Destination = strings.TrimSpace(spec.Destination)
	}

	return spec, nil
}

// Scheme + configuration come from the same xcodebuild -list call.
func (p Prompter) fillSchemeAndConfig(
	ctx context.Context,
	provider XcodebuildInfoProvider,
	runForm func(*huh.Group) error,
	spec *invoke.InvocationSpec,
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

	s, c, err := provider.ListSchemesAndConfigurations(ctx, spec.Workspace, spec.Project)
	if err != nil {
		p.debug("xcodebuild -list: %s; falling back to free-text input", err)
	} else {
		schemes = s
		configs = c
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

func (p Prompter) fillDestination(
	ctx context.Context,
	provider XcodebuildInfoProvider,
	runForm func(*huh.Group) error,
	spec *invoke.InvocationSpec,
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

func (p Prompter) debug(format string, args ...any) {
	if p.Logger == nil {
		return
	}

	p.Logger.Debugf(format, args...)
}

func containerField(spec *invoke.InvocationSpec) huh.Field {
	return huh.NewInput().
		Title("Workspace or project").
		Description("Filename ending in .xcworkspace or .xcodeproj").
		Validate(nonEmpty("Workspace or project")).
		Value(&spec.Workspace)
}

func schemeField(spec *invoke.InvocationSpec, candidates []string) huh.Field {
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

func configurationField(spec *invoke.InvocationSpec, candidates []string) huh.Field {
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

func destinationField(spec *invoke.InvocationSpec, candidates []string) huh.Field {
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

func normalizeContainer(spec *invoke.InvocationSpec) {
	spec.Workspace = strings.TrimSpace(spec.Workspace)
	spec.Project = strings.TrimSpace(spec.Project)

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
