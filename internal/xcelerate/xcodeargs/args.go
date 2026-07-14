// nolint:gochecknoglobals
package xcodeargs

import (
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

//go:generate moq -stub -out mocks/args_mock.go -pkg mocks . XcodeArgs
type XcodeArgs interface {
	Args(additional map[string]string) []string
	Command() string
	ShortCommand() string
	DerivedDataPath() string
	ProjectTempDir() string
	ProjectDir() string
	UserOtherCFlags() string
}

var CacheArgs = map[string]string{
	"COMPILATION_CACHE_ENABLE_PLUGIN":               "YES",
	"COMPILATION_CACHE_ENABLE_INTEGRATED_QUERIES":   "YES",
	"COMPILATION_CACHE_ENABLE_DETACHED_KEY_QUERIES": "YES",
	"SWIFT_ENABLE_COMPILE_CACHE":                    "YES",
	"SWIFT_ENABLE_EXPLICIT_MODULES":                 "YES",
	"SWIFT_USE_INTEGRATED_DRIVER":                   "YES",
	"CLANG_ENABLE_COMPILE_CACHE":                    "YES",
	"CLANG_ENABLE_MODULES":                          "YES",
}

var actions = []string{
	"build",
	"build-for-testing",
	"analyze",
	"archive",
	"test",
	"test-without-building",
	"docbuild",
	"installsrc",
	"installhdrs",
	"install",
	"clean",
	"-showsdks",
	"-showBuildSettings",
	"-showdestinations",
	"-showTestPlans",
	"-version",
	"-list",
	"-exportArchive",
	"-exportLocalizations",
	"-importLocalizations",
	"-exportNotarizedApp",
	"-resolvePackageDependencies",
	"-create-xcframework",
}

type Default struct {
	Cmds         []*cobra.Command
	OriginalArgs []string
	logger       log.Logger
}

func NewDefault(
	cmd *cobra.Command,
	originalArgs []string,
	logger log.Logger,
) *Default {
	var cmds []*cobra.Command

	// collect the command hierarchy to filter out command names from args
	for {
		cmds = append(cmds, cmd)
		if cmd.HasParent() {
			cmd = cmd.Parent()
		} else {
			break
		}
	}

	return &Default{
		Cmds:         cmds,
		OriginalArgs: originalArgs,
		logger:       logger,
	}
}

func (p Default) nonCommands() []string {
	nonCommands := make([]string, 0, len(p.OriginalArgs))
	for _, cmd := range p.OriginalArgs {
		var isCommand bool
		for _, c := range p.Cmds {
			if cmd == c.Use {
				isCommand = true

				break
			}
		}
		if !isCommand {
			nonCommands = append(nonCommands, cmd)
		}
	}

	return nonCommands
}

func (p Default) Command() string {
	return strings.TrimSpace(strings.Join(p.nonCommands(), " "))
}

func (p Default) ShortCommand() string {
	base := p.shortCommandBase()
	suffix := p.shapingSuffix()
	if suffix == "" {
		return base
	}
	if base == "" {
		return suffix
	}

	return base + " " + suffix
}

// shortCommandBase resolves the action keyword for the analytics short
// command. Per xcodebuild(1), `build` is the default action and is used if no
// action is given — so when argv contains no recognized action keyword we
// return the literal "build" rather than the joined arg-string, which keeps
// the rendered short command human-readable for invocations like
// `xcodebuild -workspace Foo.xcworkspace -scheme Foo -configuration Debug`.
func (p Default) shortCommandBase() string {
	nonCommands := p.nonCommands()

	for _, action := range actions {
		for _, cmd := range nonCommands {
			if strings.TrimSpace(cmd) == action {
				p.logger.Debugf("Short command found: %s", action)

				return action
			}
		}
	}

	p.logger.Debugf("No action keyword in argv; defaulting to %q per xcodebuild(1)", "build")

	return "build"
}

// shapingSuffix returns the bracketed "[scheme / testPlan / configuration]"
// suffix appended to the short command for analytics. Missing values are
// skipped (no placeholder). Returns "" if none of the three are present.
func (p Default) shapingSuffix() string {
	scheme, testPlan, configuration := p.extractShapingFlagValues()

	parts := make([]string, 0, 3)
	if scheme != "" {
		parts = append(parts, scheme)
	}
	if testPlan != "" {
		parts = append(parts, testPlan)
	}
	if configuration != "" {
		parts = append(parts, configuration)
	}
	if len(parts) == 0 {
		return ""
	}

	return "[" + strings.Join(parts, " / ") + "]"
}

// extractShapingFlagValues scans OriginalArgs for `-scheme`, `-testPlan`, and
// `-configuration` values. Supports both `-flag value` and `-flag=value` forms
// (single- or double-dash). Last occurrence wins. A value that begins with `-`
// is treated as the next flag, not as the current flag's value.
func (p Default) extractShapingFlagValues() (string, string, string) {
	var scheme, testPlan, configuration string

	args := p.OriginalArgs
	for i := 0; i < len(args); i++ {
		name, value, hasInlineValue := splitShapingFlag(args[i])
		if name != "scheme" && name != "testPlan" && name != "configuration" {
			continue
		}

		if !hasInlineValue {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				continue
			}
			value = args[i+1]
			i++
		}

		if value == "" {
			continue
		}

		switch name {
		case "scheme":
			scheme = value
		case "testPlan":
			testPlan = value
		case "configuration":
			configuration = value
		}
	}

	return scheme, testPlan, configuration
}

// splitShapingFlag parses a single argv entry as a flag. Returns the flag name
// (without leading dashes), the inline value (if a `=` was present), and a
// boolean indicating whether the inline value form was used. Non-flag args
// return an empty name.
func splitShapingFlag(arg string) (string, string, bool) {
	if !strings.HasPrefix(arg, "-") {
		return "", "", false
	}

	trimmed := strings.TrimLeft(arg, "-")
	if eq := strings.IndexByte(trimmed, '='); eq >= 0 {
		return trimmed[:eq], trimmed[eq+1:], true
	}

	return trimmed, "", false
}

func (p Default) Args(additional map[string]string) []string {
	flagsSet := make(map[string]struct{})
	for _, cmd := range p.Cmds {
		cmd.Flags().Visit(func(flag *pflag.Flag) {
			flagsSet[flag.Name] = struct{}{}
			if flag.Shorthand != "" {
				flagsSet[flag.Shorthand] = struct{}{}
			}
		})
	}

	toPass := []string{}

next:
	for _, arg := range p.OriginalArgs {
		argName := strings.Trim(arg, "-")

		for _, cmd := range p.Cmds {
			if argName == cmd.Use {
				continue next
			}
		}

		if _, skip := flagsSet[argName]; skip {
			continue
		}

		toPass = append(toPass, arg)
	}

	for name, value := range additional {
		var found bool
		for _, arg := range toPass {
			if strings.HasPrefix(arg, name+"=") {
				found = true

				break
			}
		}
		if found {
			p.logger.TWarnf("Argument already set: %s, skipping. This may lead to unexpected behavior.", name)

			continue
		} else {
			toPass = append(toPass, name+"="+value)
		}
	}

	return toPass
}
