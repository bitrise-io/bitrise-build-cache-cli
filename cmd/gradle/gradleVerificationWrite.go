package gradle

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"

	"github.com/beevik/etree"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

//go:embed "asset/verification-metadata.xml"
var referenceVerificationDepsContent string

//nolint:gochecknoglobals
var metadataPath string

// gradleVerification ...
var gradleVerification = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "gradle-verification",
	Short: "Bitrise build cache support for projects using Gradle verification",
	Long: `Bitrise build cache support for projects using Gradle verification
See https://docs.gradle.org/current/userguide/dependency_verification.html for more information.`,
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		return nil
	},
}

var writeGradleVerificationDeps = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "write",
	Short: "Add Bitrise build cache dependencies to verification-metadata.xml",
	Long: `Add Bitrise build cache dependencies to verification-metadata.xml
Missing dependencies of Bitrise build cache are appended.
`,
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		logger := log.NewLogger()

		logger.TInfof("Adding Gradle verification dependencies")

		projectMetadataPath, err := pathutil.NewPathModifier().AbsPath(metadataPath)
		if err != nil {
			return fmt.Errorf("expand metadata path (%s): %w", metadataPath, err)
		}

		if err := addGradleVerification(logger, projectMetadataPath, utils.AllEnvs()); err != nil {
			return fmt.Errorf("adding Gradle verification: %w", err)
		}

		logger.TInfof("✅ Updated verification-metadata.xml")

		return nil
	},
}

func init() {
	common.RootCmd.AddCommand(gradleVerification)
	gradleVerification.AddCommand(writeGradleVerificationDeps)
	writeGradleVerificationDeps.Flags().StringVar(&metadataPath, "metadata-path", "", "Path of verification-metadata.xml")
}

func addGradleVerification(logger log.Logger, projectMetadataPath string, _ map[string]string) error {
	logger.Infof("(i) Checking parameters")
	logger.Infof("(i) Metadata path: %s", projectMetadataPath)

	referenceDepsReader := bytes.NewBufferString(referenceVerificationDepsContent)
	projectDepsReader, err := os.Open(projectMetadataPath)
	if err != nil {
		return fmt.Errorf("failed to open project verification-metadata.xml: %w", err)
	}

	resultProjectMetadata, err := writeVerificationDeps(logger, referenceDepsReader, projectDepsReader)
	if err != nil {
		return err
	}

	if err = os.WriteFile(projectMetadataPath, []byte(resultProjectMetadata), os.ModeAppend); err != nil {
		return fmt.Errorf("failed to write project verification-metadata.xml: %w", err)
	}

	logger.Debugf("Updated contents: %s", resultProjectMetadata)

	return nil
}

func writeVerificationDeps(logger log.Logger, referenceDepsReader io.Reader, projectDepsReader io.Reader) (string, error) {
	referenceDeps := etree.NewDocument()
	if _, err := referenceDeps.ReadFrom(referenceDepsReader); err != nil {
		return "", fmt.Errorf("failed to parse reference verification-metadata.xml: %w", err)
	}

	projectDeps := etree.NewDocument()
	if _, err := projectDeps.ReadFrom(projectDepsReader); err != nil {
		return "", fmt.Errorf("failed to parse project verification-metadata.xml: %w", err)
	}

	projectRoot := projectDeps.SelectElement("verification-metadata")
	if projectRoot == nil {
		return "", fmt.Errorf("project metadata missing verification-metadata")
	}

	projectComponents := projectRoot.SelectElement("components")
	if projectComponents == nil {
		return "", fmt.Errorf("project metadata missing components")
	}

	referenceRoot := referenceDeps.SelectElement("verification-metadata")
	if referenceRoot == nil {
		return "", fmt.Errorf("reference metadata missing verification-metadata")
	}

	referenceComponents := referenceRoot.SelectElement("components")
	if referenceComponents == nil {
		return "", fmt.Errorf("reference metadata missing components")
	}

	referenceComponentList := referenceComponents.SelectElements("component")
	if referenceComponentList == nil {
		return "", fmt.Errorf("reference metadata has no components")
	}

	for _, e := range referenceComponentList {
		projectComponents.AddChild(e)
	}

	logger.Infof("Added %d dependecies to verification-metadata.xml", len(referenceComponentList))

	result, err := projectDeps.WriteToString()
	if err != nil {
		return "", fmt.Errorf("failed to serialize updated verification-metadata.xml: %w", err)
	}

	return result, nil
}
