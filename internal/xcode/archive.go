package xcode

import (
	"fmt"
	"github.com/bitrise-io/go-steputils/v2/cache/compression"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
)

func CreateCacheArchive(fileName, inputDir string, logger log.Logger) error {
	logger.TInfof("Creating cache archive %s from DerivedData folder at %s", fileName, inputDir)

	envRepo := env.NewRepository()
	archiver := compression.NewArchiver(
		logger,
		envRepo,
		compression.NewDependencyChecker(logger, envRepo))

	err := archiver.Compress(fileName, []string{inputDir}, 3, []string{"--format", "posix"})
	if err != nil {
		return fmt.Errorf("failed to compress: %w", err)
	}

	return nil
}

func ExtractCacheArchive(fileName string, logger log.Logger) error {
	logger.TInfof("Extracting cache archive %s", fileName)

	envRepo := env.NewRepository()
	archiver := compression.NewArchiver(
		logger,
		envRepo,
		compression.NewDependencyChecker(logger, envRepo))

	err := archiver.Decompress(fileName, "")
	if err != nil {
		return fmt.Errorf("failed to decompress: %w", err)
	}

	return nil
}
