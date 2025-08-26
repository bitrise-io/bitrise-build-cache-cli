package xcodeargs_test

import (
	"testing"

	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcelerate/xcodeargs"
)

func Test_DefaultXcodeArgs(t *testing.T) {
	flag1 := true
	flag2 := true

	command := func(args []string) *cobra.Command {
		cmd := cobra.Command{Use: "testCommand"}
		cmd.Flags().BoolVarP(&flag1, "flag1", "t", true, "Test")
		cmd.Flags().BoolVarP(&flag2, "flag2", "g", true, "Test")
		cmd.FParseErrWhitelist = cobra.FParseErrWhitelist{UnknownFlags: true}
		_ = cmd.ParseFlags(args)

		return &cmd
	}

	t.Run("DefaultXcodeArgs passes all flags and args when none is part of the command", func(t *testing.T) {
		// given
		args := []string{
			"subcommand",
			"subcommand2",
			"-f",
			"--flag",
		}
		cmd := command(args)

		SUT := xcodeargs.NewDefault(cmd, args, &mocks.Logger{})

		// when
		result := SUT.Args(map[string]string{})

		// then
		assert.Subset(t, result, args)
	})

	t.Run("DefaultXcodeArgs filters out command use", func(t *testing.T) {
		// given
		args := []string{
			"testCommand",
			"subcommand",
			"subcommand2",
			"-f",
			"--flag",
		}
		cmd := command(args)

		SUT := xcodeargs.NewDefault(cmd, args, &mocks.Logger{})

		// when
		result := SUT.Args(map[string]string{})

		// then
		assert.Subset(t, result, []string{
			"subcommand",
			"subcommand2",
			"-f",
			"--flag",
		})
	})

	t.Run("DefaultXcodeArgProvider filters flags of its command", func(t *testing.T) {
		// given
		args := []string{
			"subcommand",
			"subcommand2",
			"-f",
			"--flag",
			"--flag1",
			"-g",
		}

		cmd := command(args)

		SUT := xcodeargs.NewDefault(cmd, args, &mocks.Logger{})

		// when
		result := SUT.Args(map[string]string{})

		// then
		assert.Subset(t, result, []string{
			"subcommand",
			"subcommand2",
			"-f",
			"--flag",
		})
	})

	t.Run("DefaultXcodeArgProvider adds additional args also", func(t *testing.T) {
		// given
		args := []string{
			"subcommand",
		}

		cmd := command(args)

		SUT := xcodeargs.NewDefault(cmd, args, &mocks.Logger{})

		// when
		result := SUT.Args(map[string]string{"testArg": "testValue"})

		// then (not all asserted)
		assert.Subset(t, result, []string{
			"subcommand",
			"testArg=testValue",
		})
	})

	t.Run("DefaultXcodeArgProvider not overrides existing args", func(t *testing.T) {
		// given
		args := []string{
			"subcommand",
			"COMPILATION_CACHE_ENABLE_PLUGIN=NO",
			"testArg=testValue",
		}

		cmd := command(args)

		// if there is an existing arg, a warning is logged
		logger := &mocks.Logger{}
		logger.On("TWarnf", mock.Anything, mock.Anything).Return()

		SUT := xcodeargs.NewDefault(cmd, args, logger)

		// when
		result := SUT.Args(map[string]string{"testArg": "anotherValue"})

		// then (not all asserted)
		assert.Subset(t, result, []string{
			"subcommand",
			"COMPILATION_CACHE_ENABLE_PLUGIN=NO",
			"testArg=testValue",
		})
	})
}
