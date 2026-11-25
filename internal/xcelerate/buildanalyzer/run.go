package buildanalyzer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// RunOptions bundles the inputs Run needs to orchestrate the four analyzer
// stages. A zero RunOptions auto-discovers everything via derived-data climb
// from logPath.
type RunOptions struct {
	// DerivedDataPath overrides the derived-data climb from logPath. Empty
	// means: strip `<DD>/Logs/Build/UUID.xcactivitylog` down to <DD>.
	DerivedDataPath string
	// ExplicitManifest short-circuits LocateManifest when non-empty.
	ExplicitManifest string
	// ExplicitBuildDB short-circuits LocateBuildDB (and snapshot pick) when
	// non-empty.
	ExplicitBuildDB string
	// DiagMode selects the diagnostics source. Zero value (empty string) is
	// coerced to DiagModeAuto to match the `xcode analyze` default.
	DiagMode DiagMode
	// SnapshotStoreRoot overrides DefaultSnapshotRoot. Empty = default.
	SnapshotStoreRoot string
	// DisableSnapshot forces the live-DB path even when a matching snapshot
	// exists (debugging switch; mirrors `xcode analyze --no-snapshot`).
	DisableSnapshot bool
}

// Run composes ReadReport → JoinManifest (best-effort) → Classify (best-effort)
// → AttachDiagnostics (opts-gated) into a single call shared by the `xcode
// analyze` CLI and the wrapper / wrapperless sidecar hooks. Honors ctx.Done()
// between stages. Non-fatal errors from the manifest / classifier stages come
// back through the returned Warnings slice so hooks can surface them to a
// debug logger and the CLI can still print its stderr fallback strings
// without changing user-visible output.
func Run(ctx context.Context, logPath string, opts RunOptions) (Report, []error, error) {
	if logPath == "" {
		return Report{}, nil, errors.New("buildanalyzer.Run: logPath is required")
	}

	if err := ctx.Err(); err != nil {
		return Report{}, nil, fmt.Errorf("buildanalyzer.Run: %w", err)
	}

	report, err := ReadReport(logPath)
	if err != nil {
		return Report{}, nil, fmt.Errorf("read report %s: %w", logPath, err)
	}

	derivedData := opts.DerivedDataPath
	if derivedData == "" {
		derivedData = derivedDataFromLogPath(logPath)
	}

	var warnings []error

	if err := ctx.Err(); err != nil {
		return report, warnings, fmt.Errorf("buildanalyzer.Run: %w", err)
	}

	if warn := joinManifestStage(&report, derivedData, opts.ExplicitManifest); warn != nil {
		warnings = append(warnings, warn)
	}

	if err := ctx.Err(); err != nil {
		return report, warnings, fmt.Errorf("buildanalyzer.Run: %w", err)
	}

	if warn := classifyStage(&report, derivedData, opts); warn != nil { //nolint:contextcheck // ReadBuildDB owns its own 30s timeout
		warnings = append(warnings, warn)
	}

	if err := ctx.Err(); err != nil {
		return report, warnings, fmt.Errorf("buildanalyzer.Run: %w", err)
	}

	mode := opts.DiagMode
	if mode == "" {
		mode = DiagModeAuto
	}

	AttachDiagnostics(&report, derivedData, mode)

	return report, warnings, nil
}

// Private — stage wrappers that mirror the CLI's soft-fail semantics without
// taking a stderr writer. Both return nil on happy-path or no-source-available;
// errors flow back as warnings the caller can log or format.

func joinManifestStage(report *Report, derivedData, explicit string) error {
	path, err := resolveManifestPath(explicit, derivedData)
	if err != nil {
		return err
	}

	if path == "" {
		return nil
	}

	manifest, err := ReadManifest(path)
	if err != nil {
		return fmt.Errorf("manifest.json read failed: %w", err)
	}

	JoinManifest(report, manifest)

	return nil
}

func classifyStage(report *Report, derivedData string, opts RunOptions) error {
	livePath, err := resolveBuildDBPath(opts.ExplicitBuildDB, derivedData)
	if err != nil {
		return err
	}

	currentPath, priorPath := pickClassifySources(report, livePath, opts, derivedData)
	if currentPath == "" {
		return nil
	}

	snap, err := ReadSnapshotPair(currentPath, priorPath)
	if err != nil {
		if priorPath != "" && snap.Rules != nil {
			Classify(report, snap)

			return fmt.Errorf("prior snapshot read failed: %w", err)
		}

		return fmt.Errorf("build.db read failed: %w", err)
	}

	Classify(report, snap)

	return nil
}

// pickClassifySources mirrors cmd/xcode/analyze.go's precedence logic. Explicit
// --build-db / empty derived-data / DisableSnapshot short-circuits to the live
// DB. Otherwise, the snapshot store wins.
func pickClassifySources(report *Report, livePath string, opts RunOptions, derivedData string) (string, string) {
	if opts.ExplicitBuildDB != "" || opts.DisableSnapshot || derivedData == "" {
		return livePath, ""
	}

	buildHex, err := resolveBuildHexForLog(derivedData)
	if err != nil || buildHex == "" {
		return livePath, ""
	}

	storeRoot := opts.SnapshotStoreRoot
	if storeRoot == "" {
		storeRoot = DefaultSnapshotRoot()
	}

	current, prior, ok := PickSnapshotForLog(storeRoot, buildHex, report.Build.FinishedAt)
	if !ok {
		return livePath, ""
	}

	return current.Path, prior.Path
}

func resolveManifestPath(explicit, derivedData string) (string, error) {
	if explicit != "" {
		return filepath.Clean(explicit), nil
	}

	if derivedData == "" {
		return "", nil
	}

	path, err := LocateManifest(derivedData)
	if err != nil {
		return "", fmt.Errorf("locate manifest.json under %s: %w", derivedData, err)
	}

	return path, nil
}

func resolveBuildDBPath(explicit, derivedData string) (string, error) {
	if explicit != "" {
		return filepath.Clean(explicit), nil
	}

	if derivedData == "" {
		return "", nil
	}

	path, err := LocateBuildDB(derivedData)
	if err != nil {
		return "", fmt.Errorf("locate build.db under %s: %w", derivedData, err)
	}

	return path, nil
}

func resolveBuildHexForLog(derivedData string) (string, error) {
	manifest, err := LocateManifest(derivedData)
	if err != nil {
		return "", fmt.Errorf("locate manifest for build-hex: %w", err)
	}

	if manifest == "" {
		return "", nil
	}

	dir := filepath.Dir(manifest)
	base := filepath.Base(dir)

	return strings.TrimSuffix(base, ".xcbuilddata"), nil
}

// derivedDataFromLogPath climbs `<DD>/Logs/Build/UUID.xcactivitylog` down to
// `<DD>`. Returns "" when the input shape does not match — callers then run
// log-only with no manifest / DB stages.
func derivedDataFromLogPath(logPath string) string {
	if logPath == "" {
		return ""
	}

	parent := filepath.Dir(logPath)
	if filepath.Base(parent) != "Build" {
		return ""
	}

	logsDir := filepath.Dir(parent)
	if filepath.Base(logsDir) != "Logs" {
		return ""
	}

	return filepath.Dir(logsDir)
}
