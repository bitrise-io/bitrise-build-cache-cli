package internal

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

//go:generate moq -out mocks/xcode_arg_provider_mock.go -pkg mocks . XcodeArgProvider
type XcodeArgProvider interface {
	XcodeArgs() []string
}

var _ XcodeArgProvider = &DefaultXcodeArgProvider{
	Cmd:                 &cobra.Command{},
	OriginalArgProvider: &DefaultOriginalArgProvider{},
}

type DefaultXcodeArgProvider struct {
	Cmd                 *cobra.Command
	OriginalArgProvider OriginalArgProvider
}

func (provider DefaultXcodeArgProvider) XcodeArgs() []string {
	flagsSet := make(map[string]struct{})
	provider.Cmd.Flags().Visit(func(flag *pflag.Flag) {
		flagsSet[flag.Name] = struct{}{}
		if flag.Shorthand != "" {
			flagsSet[flag.Shorthand] = struct{}{}
		}
	})

	argsAndFlagsToPass := []string{}
	for _, arg := range provider.OriginalArgProvider.GetOriginalArgs() {
		argName := strings.Trim(arg, "-")

		if argName == provider.Cmd.Use {
			continue
		}

		if _, skip := flagsSet[argName]; skip {
			continue
		}

		argsAndFlagsToPass = append(argsAndFlagsToPass, arg)
	}

	return argsAndFlagsToPass
}
