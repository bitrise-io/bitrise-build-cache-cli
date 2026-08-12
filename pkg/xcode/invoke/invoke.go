// Package invoke resolves + persists an xcodebuild invocation spec used by the
// `xcode build` / `xcode test` subcommands.
//
// Sources are tried in order (config file → DerivedData discovery → prompt).
// A successful Resolve rewrites the repo-local config so the next run reuses it.
package invoke

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/deriveddata"
)

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// Command identifies the xcodebuild action.
type Command int

const (
	CommandBuild Command = iota
	CommandTest
)

// InvocationSpec is the persisted set of arguments the wrapper turns into an xcodebuild invocation.
type InvocationSpec struct {
	Workspace     string   `json:"workspace,omitempty"`
	Project       string   `json:"project,omitempty"`
	Scheme        string   `json:"scheme"`
	Configuration string   `json:"configuration,omitempty"`
	Destination   string   `json:"destination,omitempty"`
	ExtraArgs     []string `json:"extraArgs,omitempty"`
}

// Prompt fills any missing fields of spec in-place. Consumers can substitute
// their own implementation via Resolver.Prompt (e.g. for tests or non-tty steps).
type Prompt interface {
	Fill(ctx context.Context, spec *InvocationSpec) error
}

// ErrPromptUnavailable signals a prompt was needed but no interactive TTY was available.
var ErrPromptUnavailable = errors.New("prompt unavailable: cannot request missing invocation fields")

//go:generate moq -stub -out prompt_mock_test.go -pkg invoke_test . Prompt

// Resolver assembles a complete InvocationSpec by combining the repo-local
// config, a DerivedData discovery pass, and an interactive prompt. Nil fields
// fall back to production defaults.
type Resolver struct {
	Logger  log.Logger
	OsProxy utils.OsProxy
	Prompt  Prompt
	Finder  *deriveddata.Finder
}

// Resolve returns a complete spec for command. On success the resolved spec is
// persisted back to <repoRoot>/.bitrise-build-cache/xcode-{build,test}.json.
func (r *Resolver) Resolve(ctx context.Context, command Command, repoRoot string) (InvocationSpec, error) {
	logger := r.logger()

	if repoRoot == "" {
		logger.Warnf("invoke: empty repoRoot — resolved spec will not be persisted")
	}

	configPath := ""
	if repoRoot != "" {
		configPath = paths.RepoLocalConfigPath(repoRoot, configFilename(command))
	}

	spec, existed, err := r.loadExisting(configPath)
	if err != nil {
		return InvocationSpec{}, err
	}

	preservedExtraArgs := spec.ExtraArgs

	if !isComplete(spec) {
		if err := r.fillFromDiscovery(&spec, command); err != nil {
			return InvocationSpec{}, err
		}
	}

	if !isComplete(spec) {
		if err := r.promptFor(ctx, &spec); err != nil {
			return InvocationSpec{}, err
		}
	}

	if !isComplete(spec) {
		return InvocationSpec{}, fmt.Errorf("resolved invocation spec is still incomplete after prompt (config=%s)", configPath)
	}

	// If we loaded a config that carried ExtraArgs, preserve them even if the
	// spec above was re-materialised through discovery/prompt paths.
	if len(preservedExtraArgs) > 0 && len(spec.ExtraArgs) == 0 {
		spec.ExtraArgs = preservedExtraArgs
	}

	// Guarantee workspace/project mutual exclusion: if both are set, keep workspace.
	if spec.Workspace != "" && spec.Project != "" {
		spec.Project = ""
	}

	if configPath != "" {
		if err := r.persist(configPath, spec, existed); err != nil {
			return InvocationSpec{}, err
		}
	}

	return spec, nil
}

// BuildArgv assembles the xcodebuild argv from a resolved spec.
// The appropriate action (build or test) is prepended.
// Unless codesign=true, CODE_SIGNING_ALLOWED=NO / CODE_SIGN_IDENTITY="" /
// CODE_SIGNING_REQUIRED=NO are appended to skip signing. ExtraArgs are appended verbatim.
func BuildArgv(spec InvocationSpec, command Command, codesign bool) []string {
	argv := make([]string, 0, 16)

	switch command {
	case CommandTest:
		argv = append(argv, "test")
	case CommandBuild:
		fallthrough
	default:
		argv = append(argv, "build")
	}

	switch {
	case spec.Workspace != "":
		argv = append(argv, "-workspace", spec.Workspace)
	case spec.Project != "":
		argv = append(argv, "-project", spec.Project)
	}

	if spec.Scheme != "" {
		argv = append(argv, "-scheme", spec.Scheme)
	}

	if spec.Configuration != "" {
		argv = append(argv, "-configuration", spec.Configuration)
	}

	if spec.Destination != "" {
		argv = append(argv, "-destination", spec.Destination)
	}

	if !codesign {
		argv = append(argv,
			"CODE_SIGNING_ALLOWED=NO",
			"CODE_SIGN_IDENTITY=",
			"CODE_SIGNING_REQUIRED=NO",
		)
	}

	argv = append(argv, spec.ExtraArgs...)

	return argv
}

// ---------------------------------------------------------------------------
// Private
// ---------------------------------------------------------------------------

//nolint:gochecknoglobals // shared discard logger avoids per-call allocation
var noopLogger = log.NewLogger(log.WithOutput(io.Discard))

func (r *Resolver) logger() log.Logger {
	if r.Logger == nil {
		return noopLogger
	}

	return r.Logger
}

func (r *Resolver) osProxy() utils.OsProxy {
	if r.OsProxy == nil {
		return utils.DefaultOsProxy{}
	}

	return r.OsProxy
}

func (r *Resolver) finder() *deriveddata.Finder {
	if r.Finder != nil {
		return r.Finder
	}

	return &deriveddata.Finder{Logger: r.logger(), OsProxy: r.osProxy()}
}

// loadExisting decodes the repo-local config, tolerating unknown fields.
// existed=false when no file is present.
func (r *Resolver) loadExisting(configPath string) (InvocationSpec, bool, error) {
	if configPath == "" {
		return InvocationSpec{}, false, nil
	}

	content, existed, err := r.osProxy().ReadFileIfExists(configPath)
	if err != nil {
		return InvocationSpec{}, false, fmt.Errorf("read invocation config: %w", err)
	}

	if !existed {
		return InvocationSpec{}, false, nil
	}

	var spec InvocationSpec
	if err := json.Unmarshal([]byte(content), &spec); err != nil {
		return InvocationSpec{}, true, fmt.Errorf("decode invocation config %s: %w", configPath, err)
	}

	return spec, true, nil
}

func (r *Resolver) fillFromDiscovery(spec *InvocationSpec, command Command) error {
	logger := r.logger()

	discovered, err := r.finder().LatestForCommand(discoveryCommandFor(command))
	if err != nil {
		if errors.Is(err, deriveddata.ErrNoRecentBuild) {
			logger.Debugf("invoke: no recent build in DerivedData — will prompt for all missing fields")

			return nil
		}

		return fmt.Errorf("discover recent build: %w", err)
	}

	if spec.Workspace == "" && spec.Project == "" {
		spec.Workspace = discovered.Workspace
		spec.Project = discovered.Project
	}

	if spec.Scheme == "" {
		spec.Scheme = discovered.Scheme
	}

	if spec.Configuration == "" {
		spec.Configuration = discovered.Configuration
	}

	return nil
}

func (r *Resolver) promptFor(ctx context.Context, spec *InvocationSpec) error {
	prompt := r.Prompt
	if prompt == nil {
		prompt = defaultPrompt{logger: r.logger()}
	}

	if err := prompt.Fill(ctx, spec); err != nil {
		return fmt.Errorf("prompt for invocation fields: %w", err)
	}

	return nil
}

func (r *Resolver) persist(configPath string, spec InvocationSpec, existed bool) error {
	if !existed {
		if err := r.osProxy().MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(configPath), err)
		}
	}

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("encode invocation spec: %w", err)
	}

	if err := r.osProxy().WriteFile(configPath, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	return nil
}

// isComplete returns true when the spec has enough to run xcodebuild:
// one of workspace/project is set, scheme is set, and destination is set.
// Configuration is optional.
func isComplete(s InvocationSpec) bool {
	hasContainer := s.Workspace != "" || s.Project != ""

	return hasContainer && s.Scheme != "" && s.Destination != ""
}

func configFilename(command Command) string {
	switch command {
	case CommandTest:
		return "xcode-test.json"
	case CommandBuild:
		fallthrough
	default:
		return "xcode-build.json"
	}
}

func discoveryCommandFor(command Command) deriveddata.Command {
	switch command {
	case CommandTest:
		return deriveddata.CommandTest
	case CommandBuild:
		fallthrough
	default:
		return deriveddata.CommandBuild
	}
}
