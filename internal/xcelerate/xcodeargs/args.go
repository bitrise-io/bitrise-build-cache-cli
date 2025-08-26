package xcodeargs

import (
	"maps"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

//go:generate moq -out mocks/args_mock.go -pkg mocks . XcodeArgs
type XcodeArgs interface {
	Args(additional map[string]string) []string
	Command() string
	ShortCommand() string
}

// nolint:gochecknoglobals
var cacheArgs = map[string]string{
	"COMPILATION_CACHE_ENABLE_PLUGIN":               "YES",
	"COMPILATION_CACHE_ENABLE_STRICT_CAS_ERRORS":    "YES",
	"COMPILATION_CACHE_ENABLE_DIAGNOSTIC_REMARKS":   "YES",
	"COMPILATION_CACHE_ENABLE_INTEGRATED_QUERIES":   "YES",
	"COMPILATION_CACHE_ENABLE_DETACHED_KEY_QUERIES": "YES",
	"SWIFT_ENABLE_COMPILE_CACHE":                    "YES",
	"SWIFT_ENABLE_EXPLICIT_MODULES":                 "YES",
	"CLANG_ENABLE_COMPILE_CACHE":                    "YES",
	"CLANG_ENABLE_MODULES":                          "YES",
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
	return strings.Join(p.nonCommands(), " ")
}

func (p Default) ShortCommand() string {
	nonCommands := p.nonCommands()
	if len(nonCommands) == 0 {
		return ""
	}

	return nonCommands[0]
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

	defaultArgs := maps.Clone(cacheArgs)
	maps.Copy(defaultArgs, additional)

	for name, value := range defaultArgs {
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
