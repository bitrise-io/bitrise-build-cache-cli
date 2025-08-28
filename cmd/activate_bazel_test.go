package cmd_test

import (
	"errors"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd"
	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/bazel"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
)

func Test_activateBazelCmdFn(t *testing.T) {
	t.Run("When no error activateBazelCmdFn creates template inventory and writes bazelrc", func(t *testing.T) {
		templateInventory := bazelconfig.TemplateInventory{
			Common: bazelconfig.CommonTemplateInventory{
				AuthToken: "AuthTokenValue",
			},
		}

		var actualTemplateInventory *bazelconfig.TemplateInventory
		var actualPath *string

		// when
		err := cmd.ActivateBazelCmdFn(
			mockLogger,
			"~/.bazelrc",
			map[string]string{},
			func(_ string, _ ...string) (string, error) {
				return "", nil
			},
			func(log.Logger, map[string]string, common.CommandFunc, bool) (bazelconfig.TemplateInventory, error) {
				return templateInventory, nil
			},
			func(
				inventory bazelconfig.TemplateInventory,
				path string,
			) error {
				actualTemplateInventory = &inventory
				actualPath = &path

				return nil
			},
		)

		// then
		require.NoError(t, err)
		require.Equal(t, templateInventory, *actualTemplateInventory)
		require.Equal(t, "~/.bazelrc", *actualPath)
	})

	t.Run("When templateInventory creation fails activateBazelCmdFn throws error", func(t *testing.T) {
		inventoryCreationError := errors.New("failed to create inventory")

		// when
		err := cmd.ActivateBazelCmdFn(
			mockLogger,
			"~/.bazelrc",
			map[string]string{},
			func(_ string, _ ...string) (string, error) {
				return "", nil
			},
			func(log.Logger, map[string]string, common.CommandFunc, bool) (bazelconfig.TemplateInventory, error) {
				return bazelconfig.TemplateInventory{}, inventoryCreationError
			},
			func(
				bazelconfig.TemplateInventory,
				string,
			) error {
				return nil
			},
		)

		// then
		require.EqualError(t, err, inventoryCreationError.Error())
	})

	t.Run("When template writing fails activateBazelCmdFn throws error", func(t *testing.T) {
		templateWriteError := errors.New("failed to write template")

		// when
		err := cmd.ActivateBazelCmdFn(
			mockLogger,
			"~/.bazelrc",
			map[string]string{},
			func(_ string, _ ...string) (string, error) {
				return "", nil
			},
			func(log.Logger, map[string]string, common.CommandFunc, bool) (bazelconfig.TemplateInventory, error) {
				return bazelconfig.TemplateInventory{}, nil
			},
			func(
				bazelconfig.TemplateInventory,
				string,
			) error {
				return templateWriteError
			},
		)

		// then
		require.EqualError(t, err, templateWriteError.Error())
	})
}
