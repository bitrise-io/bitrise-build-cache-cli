package xcode

import (
	"fmt"
	"github.com/bitrise-io/go-steputils/v2/cache/compression"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
)

func CreateCacheArchive(inputPath string, fileName string, logger log.Logger) error {
	logger.Infof("(i) Creating cache archive %s from DerivedData folder at %s", fileName, inputPath)

	envRepo := env.NewRepository()
	archiver := compression.NewArchiver(
		logger,
		envRepo,
		compression.NewDependencyChecker(logger, envRepo))

	err := archiver.Compress(fileName, []string{inputPath}, 3, []string{"--format", "posix"})
	if err != nil {
		return fmt.Errorf("failed to compress: %w", err)
	}

	return nil
}
