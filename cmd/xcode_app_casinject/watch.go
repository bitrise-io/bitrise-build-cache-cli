package xcode_app_casinject

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	xcelerateconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	injector "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcode_app_casinject"
)

//nolint:gochecknoglobals
var (
	watchRoots              []string
	watchSocketPath         string
	watchPoll               time.Duration
	watchAllowMissingSocket bool
)

//nolint:gochecknoglobals
var watchCmd = &cobra.Command{
	Use:          "watch",
	Short:        "Watch DerivedData for .cas-config files and inject RemoteService",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		roots, err := resolveRoots(watchRoots)
		if err != nil {
			return err
		}

		socketPath := watchSocketPath
		if socketPath == "" {
			socketPath = xcelerateconfig.ResolveProxySocketPath("", envsMap(), utils.DefaultOsProxy{})
		}
		if err := injector.ValidateSocket(socketPath); err != nil {
			if !errors.Is(err, injector.ErrSocketMissing) {
				return err //nolint:wrapcheck
			}
			if !watchAllowMissingSocket {
				return fmt.Errorf("%w — start the xcelerate proxy first, or pass --allow-missing-socket", err)
			}
			logger.Warnf("proxy socket missing at %s — continuing (--allow-missing-socket)", socketPath)
		}

		w, err := injector.NewWatcher(injector.WatchParams{
			Roots:        roots,
			SocketPath:   socketPath,
			Logger:       logger,
			PollInterval: watchPoll,
		})
		if err != nil {
			return fmt.Errorf("watcher: %w", err)
		}

		logger.Infof("watching %d roots for .cas-config; socket=%s poll=%s", len(roots), socketPath, watchPoll)

		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		if err := w.Run(ctx); err != nil {
			return fmt.Errorf("watch: %w", err)
		}

		return nil
	},
}

func resolveRoots(explicit []string) ([]string, error) {
	if len(explicit) > 0 {
		return explicit, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home dir: %w", err)
	}

	return []string{filepath.Join(home, "Library", "Developer", "Xcode", "DerivedData")}, nil
}

func envsMap() map[string]string {
	out := map[string]string{}
	for _, e := range os.Environ() {
		for i := range len(e) {
			if e[i] == '=' {
				out[e[:i]] = e[i+1:]

				break
			}
		}
	}

	return out
}

func init() {
	watchCmd.Flags().StringSliceVar(&watchRoots, "root", nil,
		"DerivedData roots to watch recursively (defaults to ~/Library/Developer/Xcode/DerivedData)")
	watchCmd.Flags().StringVar(&watchSocketPath, "socket", "",
		"xcelerate proxy socket path (defaults to xcelerate config resolution)")
	watchCmd.Flags().DurationVar(&watchPoll, "poll-interval", 2*time.Second,
		"periodic rescan interval for late-created directories")
	watchCmd.Flags().BoolVar(&watchAllowMissingSocket, "allow-missing-socket", false,
		"start watcher even if the proxy socket does not exist yet")
	rootCmd.AddCommand(watchCmd)
}
