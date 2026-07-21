package doctor

import (
	"context"
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/updater"
)

type UpdateFixer struct {
	Update func(ctx context.Context) error
	Debug  bool
}

//nolint:contextcheck // Fixer.Fix is ctx-less by design; Background is correct here.
func (f UpdateFixer) Fix() (string, error) {
	run := f.Update
	if run == nil {
		run = defaultUpdate(f.Debug)
	}

	if err := run(context.Background()); err != nil {
		return "", fmt.Errorf("update: %w", err)
	}

	return "ran update", nil
}

func defaultUpdate(debug bool) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve cli executable: %w", err)
		}

		if err := updater.Update(ctx, updater.Options{
			Executable: exe,
			Logger:     log.NewLogger(log.WithDebugLog(debug)),
		}); err != nil {
			return fmt.Errorf("updater: %w", err)
		}

		return nil
	}
}
