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

	// runForm is a seam so tests can drive the form without a real TTY.
	// nil falls back to tui.RunForm.
	runForm func(*huh.Group) error
}

func (p defaultPrompt) Fill(_ context.Context, spec *InvocationSpec) error {
	// Mirror wizard_tools.go: huh accessible mode (TERM=dumb) reads from stdin
	// so a real TTY isn't required. Everything else needs one.
	if os.Getenv("TERM") != "dumb" && !term.IsTerminal(int(os.Stdin.Fd())) {
		return ErrPromptUnavailable
	}

	fields := []huh.Field{}

	// Container: only prompt when both workspace and project are empty.
	if spec.Workspace == "" && spec.Project == "" {
		fields = append(fields,
			huh.NewInput().
				Title("Workspace or project").
				Description("Filename ending in .xcworkspace or .xcodeproj").
				Validate(nonEmpty("Workspace or project")).
				Value(&spec.Workspace),
		)
	}

	if spec.Scheme == "" {
		fields = append(fields,
			huh.NewInput().
				Title("Scheme").
				Validate(nonEmpty("Scheme")).
				Value(&spec.Scheme),
		)
	}

	if spec.Destination == "" {
		fields = append(fields,
			huh.NewInput().
				Title("Destination").
				Description(`xcodebuild -destination string, e.g. "platform=iOS Simulator,name=iPhone 15"`).
				Validate(nonEmpty("Destination")).
				Value(&spec.Destination),
		)
	}

	if len(fields) == 0 {
		return nil
	}

	runForm := p.runForm
	if runForm == nil {
		runForm = func(g *huh.Group) error { return tui.RunForm(g) } //nolint:wrapcheck // tui already wraps errors
	}

	if err := runForm(huh.NewGroup(fields...)); err != nil {
		return err //nolint:wrapcheck // tui already wraps errors
	}

	spec.Workspace = strings.TrimSpace(spec.Workspace)
	spec.Project = strings.TrimSpace(spec.Project)
	spec.Scheme = strings.TrimSpace(spec.Scheme)
	spec.Destination = strings.TrimSpace(spec.Destination)

	// If the container answer wasn't an .xcworkspace, treat it as the project field.
	if spec.Workspace != "" && !strings.HasSuffix(spec.Workspace, ".xcworkspace") {
		spec.Project = spec.Workspace
		spec.Workspace = ""
	}

	return nil
}

func nonEmpty(label string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(label + " cannot be empty")
		}

		return nil
	}
}
