package xcode

import (
	"fmt"
	"github.com/bitrise-io/go-steputils/v2/cache/compression"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"os"
	"path/filepath"
)

func CreateCacheArchive(fileName, inputDir string, logger log.Logger) error {
	logger.Debugf("Creating cache archive %s from DerivedData folder at %s", fileName, inputDir)

	envRepo := env.NewRepository()
	archiver := compression.NewArchiver(
		logger,
		envRepo,
		compression.NewDependencyChecker(logger, envRepo))

	dir := filepath.Dir(fileName)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create cache archive directory: %w", err)
	}

	err := archiver.Compress(fileName, []string{inputDir}, 3, []string{"--format", "posix"})
	if err != nil {
		return fmt.Errorf("failed to compress: %w", err)
	}

	return nil
}

func ExtractCacheArchive(fileName string, logger log.Logger) error {
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