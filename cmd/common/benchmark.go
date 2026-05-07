package common

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/ccache"
	rnconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/reactnative"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/status"
)

//nolint:gochecknoglobals
var (
	benchmarkJSONOutput bool
	benchmarkRuns       int
)

//nolint:gochecknoglobals
var benchmarkCmd = &cobra.Command{
	Use:           "benchmark -- <build-command> [args...]",
	Short:         "Measure build time with and without the build cache",
	Long:          "Runs the build command --runs times with the cache disabled (baseline), then --runs times with the cache enabled, and reports per-run durations and averages.\n\nExample:\n  bitrise-build-cache benchmark -- ./gradlew assembleDebug\n  bitrise-build-cache benchmark -- xcodebuild -scheme MyApp -configuration Release",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("no build command provided — usage: benchmark -- <build-command> [args...]")
		}

		logger := log.NewLogger(log.WithDebugLog(IsDebugLogMode))

		checker := status.NewChecker(status.CheckerParams{})
		s := checker.Status()

		if !s.Gradle && !s.Xcode && !s.Cpp && !s.ReactNative {
			err := fmt.Errorf("no build caches are active — run the appropriate activate command first")
			if benchmarkJSONOutput {
				_ = WriteJSON(cmd.OutOrStdout(), map[string]any{"error": err.Error()})
			}

			return err
		}

		restorers, err := disableCaches(s, logger)
		if err != nil {
			wrappedErr := fmt.Errorf("disable caches for baseline run: %w", err)
			if benchmarkJSONOutput {
				_ = WriteJSON(cmd.OutOrStdout(), map[string]any{"error": wrappedErr.Error()})
			}

			return wrappedErr
		}

		restored := false
		defer func() {
			if !restored {
				runRestorers(restorers)
			}
		}()

		logger.TInfof("Running %d build(s) WITHOUT cache (baseline)…", benchmarkRuns)
		withoutCache := runBuilds(cmd, args, benchmarkRuns)

		runRestorers(restorers)
		restored = true

		logger.TInfof("Running %d build(s) WITH cache…", benchmarkRuns)
		withCache := runBuilds(cmd, args, benchmarkRuns)

		result := buildBenchmarkResult(args, withoutCache, withCache)

		if benchmarkJSONOutput {
			return WriteJSON(cmd.OutOrStdout(), result)
		}

		printBenchmarkResult(cmd, result)

		return nil
	},
}

func init() {
	RootCmd.AddCommand(benchmarkCmd)
	benchmarkCmd.Flags().BoolVar(&benchmarkJSONOutput, "json", false, "Emit machine-readable JSON to stdout")
	benchmarkCmd.Flags().IntVar(&benchmarkRuns, "runs", 3, "Number of builds to run in each mode")
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type restoreFn func()

type runResult struct {
	Run        int    `json:"run"`
	DurationMs int64  `json:"durationMs"`
	ExitCode   int    `json:"exitCode"`
	Error      string `json:"error,omitempty"`
}

type benchmarkResult struct {
	BuildCommand []string    `json:"buildCommand"`
	WithoutCache []runResult `json:"withoutCache"`
	WithCache    []runResult `json:"withCache"`
	Averages     averages    `json:"averages"`
}

type averages struct {
	WithoutCacheMs float64 `json:"withoutCacheDurationMs"`
	WithCacheMs    float64 `json:"withCacheDurationMs"`
}

// ---------------------------------------------------------------------------
// Cache disable / restore
// ---------------------------------------------------------------------------

func disableCaches(s status.Status, logger log.Logger) ([]restoreFn, error) {
	osProxy := utils.DefaultOsProxy{}
	decoderFactory := utils.DefaultDecoderFactory{}
	encoderFactory := utils.DefaultEncoderFactory{}

	var restorers []restoreFn

	if s.Xcode {
		restore, err := disableXcodeCache(osProxy, decoderFactory, encoderFactory, logger)
		if err != nil {
			return nil, fmt.Errorf("xcode: %w", err)
		}

		restorers = append(restorers, restore)
	}

	if s.Cpp {
		restore, err := disableCcacheCache(osProxy, decoderFactory, encoderFactory, logger)
		if err != nil {
			return nil, fmt.Errorf("ccache: %w", err)
		}

		restorers = append(restorers, restore)
	}

	if s.ReactNative {
		restore, err := disableRNCache(osProxy, decoderFactory, encoderFactory, logger)
		if err != nil {
			return nil, fmt.Errorf("react-native: %w", err)
		}

		restorers = append(restorers, restore)
	}

	if s.Gradle {
		restore, err := disableGradleCache()
		if err != nil {
			return nil, fmt.Errorf("gradle: %w", err)
		}

		restorers = append(restorers, restore)
	}

	return restorers, nil
}

func disableXcodeCache(osProxy utils.OsProxy, dec utils.DecoderFactory, enc utils.EncoderFactory, logger log.Logger) (restoreFn, error) {
	cfg, err := xcelerate.ReadConfig(osProxy, dec)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	original := cfg.BuildCacheEnabled
	cfg.BuildCacheEnabled = false

	if err := cfg.Save(logger, osProxy, enc); err != nil {
		return nil, fmt.Errorf("save config: %w", err)
	}

	return func() {
		cfg.BuildCacheEnabled = original
		_ = cfg.Save(logger, osProxy, enc)
	}, nil
}

func disableCcacheCache(osProxy utils.OsProxy, dec utils.DecoderFactory, enc utils.EncoderFactory, logger log.Logger) (restoreFn, error) {
	cfg, err := ccacheconfig.ReadConfig(osProxy, dec)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	original := cfg.Enabled
	cfg.Enabled = false

	if err := cfg.Save(logger, osProxy, enc); err != nil {
		return nil, fmt.Errorf("save config: %w", err)
	}

	return func() {
		cfg.Enabled = original
		_ = cfg.Save(logger, osProxy, enc)
	}, nil
}

func disableRNCache(osProxy utils.OsProxy, dec utils.DecoderFactory, enc utils.EncoderFactory, logger log.Logger) (restoreFn, error) {
	cfg, err := rnconfig.ReadConfig(osProxy, dec)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	original := cfg.Enabled
	cfg.Enabled = false

	if err := cfg.Save(logger, osProxy, enc); err != nil {
		return nil, fmt.Errorf("save config: %w", err)
	}

	return func() {
		cfg.Enabled = original
		_ = cfg.Save(logger, osProxy, enc)
	}, nil
}

func disableGradleCache() (restoreFn, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}

	initFile := filepath.Join(home, ".gradle", "init.d", "bitrise-build-cache.init.gradle.kts")
	disabledFile := initFile + ".benchmarkdisabled"

	if err := os.Rename(initFile, disabledFile); err != nil {
		return nil, fmt.Errorf("rename gradle init file: %w", err)
	}

	return func() {
		_ = os.Rename(disabledFile, initFile)
	}, nil
}

func runRestorers(restorers []restoreFn) {
	for _, restore := range restorers {
		restore()
	}
}

// ---------------------------------------------------------------------------
// Build execution
// ---------------------------------------------------------------------------

func runBuilds(cmd *cobra.Command, args []string, n int) []runResult {
	results := make([]runResult, 0, n)

	for i := range n {
		start := time.Now()
		exitCode, runErr := runCommand(cmd, args)
		elapsed := time.Since(start)

		r := runResult{
			Run:        i + 1,
			DurationMs: elapsed.Milliseconds(),
			ExitCode:   exitCode,
		}

		if runErr != nil {
			r.Error = runErr.Error()
		}

		results = append(results, r)
	}

	return results
}

func runCommand(cmd *cobra.Command, args []string) (int, error) {
	//nolint:gosec // args are user-supplied by design — this command's purpose is to run arbitrary build commands
	c := exec.CommandContext(cmd.Context(), args[0], args[1:]...)
	c.Stdout = os.Stderr // build output to stderr; benchmark results to stdout
	c.Stderr = os.Stderr

	if err := c.Run(); err != nil {
		exitErr := new(exec.ExitError)
		if ok := errors.As(err, &exitErr); ok {
			return exitErr.ExitCode(), fmt.Errorf("build exited %d: %w", exitErr.ExitCode(), err)
		}

		return 1, fmt.Errorf("run command: %w", err)
	}

	return 0, nil
}

// ---------------------------------------------------------------------------
// Output
// ---------------------------------------------------------------------------

func buildBenchmarkResult(args []string, withoutCache, withCache []runResult) benchmarkResult {
	return benchmarkResult{
		BuildCommand: args,
		WithoutCache: withoutCache,
		WithCache:    withCache,
		Averages: averages{
			WithoutCacheMs: avgMs(withoutCache),
			WithCacheMs:    avgMs(withCache),
		},
	}
}

func avgMs(runs []runResult) float64 {
	if len(runs) == 0 {
		return 0
	}

	var total int64
	for _, r := range runs {
		total += r.DurationMs
	}

	return float64(total) / float64(len(runs))
}

func printBenchmarkResult(cmd *cobra.Command, r benchmarkResult) {
	out := cmd.OutOrStdout()

	fmt.Fprintf(out, "\nBenchmark Results\n")
	fmt.Fprintf(out, "Build command: %s\n\n", strings.Join(r.BuildCommand, " "))

	fmt.Fprintf(out, "Without cache (baseline):\n")
	for _, run := range r.WithoutCache {
		fmt.Fprintf(out, "  Run %d: %s", run.Run, formatDuration(run.DurationMs))
		if run.ExitCode != 0 {
			fmt.Fprintf(out, " (exit %d)", run.ExitCode)
		}
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "  Average: %s\n\n", formatDuration(int64(r.Averages.WithoutCacheMs)))

	fmt.Fprintf(out, "With cache:\n")
	for _, run := range r.WithCache {
		fmt.Fprintf(out, "  Run %d: %s", run.Run, formatDuration(run.DurationMs))
		if run.ExitCode != 0 {
			fmt.Fprintf(out, " (exit %d)", run.ExitCode)
		}
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "  Average: %s\n\n", formatDuration(int64(r.Averages.WithCacheMs)))

	if r.Averages.WithCacheMs > 0 && r.Averages.WithoutCacheMs > r.Averages.WithCacheMs {
		speedup := r.Averages.WithoutCacheMs / r.Averages.WithCacheMs
		saved := 100 * (1 - r.Averages.WithCacheMs/r.Averages.WithoutCacheMs)
		fmt.Fprintf(out, "Cache speedup: %.1fx (%.0f%% faster)\n", speedup, saved)
	}
}

func formatDuration(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}

	mins := int(d.Minutes())
	secs := d.Seconds() - float64(mins)*60

	return fmt.Sprintf("%dm%.1fs", mins, secs)
}
