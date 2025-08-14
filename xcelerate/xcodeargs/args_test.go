package xcodeargs_test

import (
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/xcelerate/xcodeargs"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func Test_DefaultXcodeArgs(t *testing.T) {
	flag1 := true
	flag2 := true

	command := func(args []string) cobra.Command {
		cmd := cobra.Command{Use: "testCommand"}
		cmd.Flags().BoolVarP(&flag1, "flag1", "t", true, "Test")
		cmd.Flags().BoolVarP(&flag2, "flag2", "g", true, "Test")
		cmd.FParseErrWhitelist = cobra.FParseErrWhitelist{UnknownFlags: true}
		_ = cmd.ParseFlags(args)

		return cmd
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

		SUT := xcodeargs.Default{
			Cmd:          &cmd,
			OriginalArgs: args,
		}

		// when
		result := SUT.Args()

		// then
		assert.Equal(t, args, result)
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

		SUT := xcodeargs.Default{
			Cmd:          &cmd,
			OriginalArgs: args,
		}

		// when
		result := SUT.Args()

		// then
		assert.Equal(t, []string{
			"subcommand",
			"subcommand2",
			"-f",
			"--flag",
		}, result)
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

		SUT := xcodeargs.Default{
			Cmd:          &cmd,
			OriginalArgs: args,
		}

		// when
		result := SUT.Args()

		// then
		assert.Equal(t, []string{
			"subcommand",
			"subcommand2",
			"-f",
			"--flag",
		}, result)
	})
}
