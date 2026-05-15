package xcodeargs_test

import (
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/xcelerate/xcodeargs"
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

func Test_ShortCommand(t *testing.T) {
	type testCase struct {
		name     string
		args     string
		expected string
	}

	tcs := []testCase{
		{
			name:     "just the short command",
			args:     "xcodebuild test",
			expected: "test",
		},
		{
			name:     "with command at the end",
			args:     "xcodebuild -destination 'platform=iOS Simulator,OS=18.1,name=iPhone 16 Pro' -scheme WordPress -workspace WordPress.xcworkspace  CODE_SIGN_IDENTITY= CODE_SIGNING_REQUIRED=NO -showBuildTimingSummary test\n",
			expected: "test [WordPress]",
		},
		{
			name:     "with command in the middle",
			args:     "xcodebuild test -destination 'platform=iOS Simulator,OS=18' CODE_SIGN_IDENTITY= CODE_SIGNING_REQUIRED=NO",
			expected: "test",
		},
		{
			name:     "with dashed command",
			args:     "xcodebuild -exportArchive -destination 'platform=iOS Simulator,OS=18' CODE_SIGN_IDENTITY= CODE_SIGNING_REQUIRED=NO",
			expected: "-exportArchive",
		},
		{
			name:     "clean followed by archive returns archive",
			args:     "xcodebuild clean archive -workspace /Users/vagrant/git/GuestSelfService.xcworkspace -scheme HyattApp -configuration Release -xcconfig /var/folders/f6/wf2hj3cj75qdwmt5rn814r_00000gn/T/2300449354/temp.xcconfig -archivePath /var/folders/f6/wf2hj3cj75qdwmt5rn814r_00000gn/T/xcodeArchive2352012375/HyattApp.xcarchive -destination generic/platform=iOS -skipPackagePluginValidation -disableAutomaticPackageResolution PROVISIONING_PROFILE= PROVISIONING_PROFILE_SPECIFIER=HyattApp_InHouse_Kermit",
			expected: "archive [HyattApp / Release]",
		},
		{
			name:     "clean alone returns clean",
			args:     "xcodebuild clean",
			expected: "clean",
		},
		{
			name:     "with no action defaults to build per xcodebuild(1)",
			args:     "-destination mars 'platform=iOS Simulator,OS=18' CODE_SIGN_IDENTITY= CODE_SIGNING_REQUIRED=NO",
			expected: "build",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			args := strings.Split(tc.args, " ")
			cmd := &cobra.Command{Use: "xcodebuild"}
			SUT := xcodeargs.NewDefault(cmd, args, mockLogger)

			// when
			result := SUT.ShortCommand()

			// then
			assert.Equal(t, tc.expected, result)
		})
	}
}

func Test_ShortCommand_ShapingFlags(t *testing.T) {
	type testCase struct {
		name     string
		args     []string
		expected string
	}

	tcs := []testCase{
		{
			name:     "scheme only",
			args:     []string{"xcodebuild", "test", "-scheme", "StockTwits(Production)"},
			expected: "test [StockTwits(Production)]",
		},
		{
			name:     "scheme and testPlan",
			args:     []string{"xcodebuild", "test", "-scheme", "StockTwits(Production)", "-testPlan", "StockTwits(Develop)_bitrise"},
			expected: "test [StockTwits(Production) / StockTwits(Develop)_bitrise]",
		},
		{
			name:     "scheme and configuration without testPlan (no double slash)",
			args:     []string{"xcodebuild", "archive", "-scheme", "AccorHotelsApp", "-configuration", "SandboxDevelopmentRelease"},
			expected: "archive [AccorHotelsApp / SandboxDevelopmentRelease]",
		},
		{
			name:     "all three flags",
			args:     []string{"xcodebuild", "test-without-building", "-scheme", "Substack", "-testPlan", "SubstackUITests", "-configuration", "Debug"},
			expected: "test-without-building [Substack / SubstackUITests / Debug]",
		},
		{
			name:     "testPlan without scheme still renders brackets",
			args:     []string{"xcodebuild", "test", "-testPlan", "SubstackUITests"},
			expected: "test [SubstackUITests]",
		},
		{
			name:     "configuration without scheme still renders brackets",
			args:     []string{"xcodebuild", "build", "-configuration", "Debug"},
			expected: "build [Debug]",
		},
		{
			name:     "testPlan and configuration without scheme",
			args:     []string{"xcodebuild", "test", "-testPlan", "Smoke", "-configuration", "Debug"},
			expected: "test [Smoke / Debug]",
		},
		{
			name:     "scheme value with spaces",
			args:     []string{"xcodebuild", "test", "-scheme", "DysonLink UI Tests"},
			expected: "test [DysonLink UI Tests]",
		},
		{
			name:     "scheme value containing hyphen segment",
			args:     []string{"xcodebuild", "archive", "-scheme", "Terminal - Certifications"},
			expected: "archive [Terminal - Certifications]",
		},
		{
			name:     "non-action base showBuildSettings with no shaping flags is unchanged",
			args:     []string{"xcodebuild", "-showBuildSettings", "-workspace", "Foo.xcworkspace"},
			expected: "-showBuildSettings",
		},
		{
			name:     "non-action base showBuildSettings with scheme still gets brackets",
			args:     []string{"xcodebuild", "-showBuildSettings", "-scheme", "Foo"},
			expected: "-showBuildSettings [Foo]",
		},
		{
			name:     "equals form: -scheme=Value",
			args:     []string{"xcodebuild", "test", "-scheme=WordPress"},
			expected: "test [WordPress]",
		},
		{
			name:     "equals form: all three with =",
			args:     []string{"xcodebuild", "test", "-scheme=Substack", "-testPlan=SubstackUITests", "-configuration=Debug"},
			expected: "test [Substack / SubstackUITests / Debug]",
		},
		{
			name:     "double-dash form",
			args:     []string{"xcodebuild", "test", "--scheme", "WordPress", "--configuration", "Release"},
			expected: "test [WordPress / Release]",
		},
		{
			name:     "empty scheme value is treated as absent",
			args:     []string{"xcodebuild", "test", "-scheme", "", "-configuration", "Debug"},
			expected: "test [Debug]",
		},
		{
			name:     "scheme followed by another flag has no value",
			args:     []string{"xcodebuild", "test", "-scheme", "-workspace", "Foo.xcworkspace"},
			expected: "test",
		},
		{
			name:     "last occurrence of scheme wins",
			args:     []string{"xcodebuild", "test", "-scheme", "First", "-scheme", "Second"},
			expected: "test [Second]",
		},
		{
			name:     "no action keyword + workspace + scheme defaults base to build",
			args:     []string{"xcodebuild", "-workspace", "Seek.xcworkspace", "-scheme", "Seek", "-destination", "generic/platform=iOS Simulator"},
			expected: "build [Seek]",
		},
		{
			name:     "no action keyword + workspace + scheme + configuration defaults base to build",
			args:     []string{"xcodebuild", "-workspace", "Seek.xcworkspace", "-configuration", "Debug", "-scheme", "Seek", "-destination", "generic/platform=iOS Simulator"},
			expected: "build [Seek / Debug]",
		},
		{
			name:     "no action keyword + project + scheme + configuration defaults base to build",
			args:     []string{"xcodebuild", "-project", "Pods.xcodeproj", "-scheme", "AppAuth", "-destination", "generic/platform=iOS", "-configuration", "Debug"},
			expected: "build [AppAuth / Debug]",
		},
		{
			name:     "no action keyword and no shaping flags renders bare build",
			args:     []string{"xcodebuild", "-destination", "mars", "CODE_SIGN_IDENTITY=", "CODE_SIGNING_REQUIRED=NO"},
			expected: "build",
		},
		{
			name:     "scheme value with spaces, no action keyword",
			args:     []string{"xcodebuild", "-workspace", "ios/mobile.xcworkspace", "-configuration", "ReleaseHimsStaging", "-scheme", "hims (staging)", "-sdk", "iphonesimulator"},
			expected: "build [hims (staging) / ReleaseHimsStaging]",
		},
		{
			name:     "explicit build action keyword is unchanged when also using shaping flags",
			args:     []string{"xcodebuild", "build", "-workspace", "Foo.xcworkspace", "-scheme", "Foo", "-configuration", "Release"},
			expected: "build [Foo / Release]",
		},
		{
			name:     "installhdrs is recognized as an action keyword",
			args:     []string{"xcodebuild", "installhdrs", "-scheme", "Foo"},
			expected: "installhdrs [Foo]",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			cmd := &cobra.Command{Use: "xcodebuild"}
			SUT := xcodeargs.NewDefault(cmd, tc.args, mockLogger)

			// when
			result := SUT.ShortCommand()

			// then
			assert.Equal(t, tc.expected, result)
		})
	}
}

func Test_Command(t *testing.T) {
	// given
	args := strings.Split("xcodebuild -destination 'platform=iOS Simulator,OS=18.1,name=iPhone 16 Pro' -scheme WordPress -workspace WordPress.xcworkspace  CODE_SIGN_IDENTITY= CODE_SIGNING_REQUIRED=NO -showBuildTimingSummary test\n", " ")
	cmd := &cobra.Command{Use: "xcodebuild"}
	SUT := xcodeargs.NewDefault(cmd, args, mockLogger)

	// when
	result := SUT.Command()

	// then
	assert.Equal(t, "-destination 'platform=iOS Simulator,OS=18.1,name=iPhone 16 Pro' -scheme WordPress -workspace WordPress.xcworkspace  CODE_SIGN_IDENTITY= CODE_SIGNING_REQUIRED=NO -showBuildTimingSummary test", result)
}
