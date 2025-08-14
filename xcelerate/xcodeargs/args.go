package xcodeargs

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

//go:generate moq -out mocks/args_mock.go -pkg mocks . XcodeArgs
type XcodeArgs interface {
	Args() []string
}

type Default struct {
	Cmd          *cobra.Command
	OriginalArgs []string
}

func (provider Default) Args() []string {
	flagsSet := make(map[string]struct{})
	provider.Cmd.Flags().Visit(func(flag *pflag.Flag) {
		flagsSet[flag.Name] = struct{}{}
		if flag.Shorthand != "" {
			flagsSet[flag.Shorthand] = struct{}{}
		}
	})

	toPass := []string{}
	for _, arg := range provider.OriginalArgs {
		argName := strings.Trim(arg, "-")

		if argName == provider.Cmd.Use {
			continue
		}

		if _, skip := flagsSet[argName]; skip {
			continue
		}

		toPass = append(toPass, arg)
	}

	return toPass
}
