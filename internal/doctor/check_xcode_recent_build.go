package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcactivitylog"
)

// xcodeRecentBuildWindow is wide enough to catch a typical build-think-build
// cadence while narrow enough that stale logs from yesterday don't drown the
// signal. Only the newest match in the window is reported.
const xcodeRecentBuildWindow = 1 * time.Hour

func (d *Doctor) xcodeRecentBuildCheck() Check {
	return Check{
		Name: "xcode-recent-build",
		Diagnose: func(_ context.Context) Result {
			if !d.toolActivated(toolconfig.Xcelerate) {
				return Result{State: StateOK, Detail: "skipped (xcode not activated)"}
			}

			if runtime.GOOS != "darwin" {
				return Result{State: StateOK, Detail: "skipped (macOS only)"}
			}

			home, err := os.UserHomeDir()
			if err != nil {
				return Result{State: StateError, Detail: "resolve home dir: " + err.Error()}
			}

			return diagnoseXcodeRecentBuild(filepath.Join(home, "Library", "Developer", "Xcode", "DerivedData"), d.now())
		},
	}
}

func diagnoseXcodeRecentBuild(derivedDataRoot string, now time.Time) Result {
	cutoff := now.Add(-xcodeRecentBuildWindow)

	newest, project, err := findNewestRecentActivityLog(derivedDataRoot, cutoff)
	if err != nil {
		return Result{State: StateOK, Detail: "no recent Xcode build found (" + err.Error() + ")"}
	}
	if newest == "" {
		return Result{State: StateOK, Detail: "no recent Xcode build found"}
	}

	info, statErr := os.Stat(newest)
	if statErr != nil {
		return Result{State: StateOK, Detail: fmt.Sprintf("stat %s: %s", newest, statErr)}
	}

	metrics, err := xcactivitylog.ReadCompilationCacheMetrics(newest)
	if err != nil {
		return Result{State: StateOK, Detail: fmt.Sprintf("read xcactivitylog %s: %s", newest, err)}
	}

	switch metrics.Outcome {
	case xcactivitylog.OutcomeOK:
		pct := 0
		if metrics.Total > 0 {
			pct = int(100.0 * float64(metrics.Hits) / float64(metrics.Total))
		}

		return Result{
			State: StateOK,
			Detail: fmt.Sprintf("recent xcode build: %d/%d hits (%d%%) at %s, project %s",
				metrics.Hits, metrics.Total, pct, info.ModTime().UTC().Format(time.RFC3339), project),
		}
	case xcactivitylog.OutcomeEmpty:
		return Result{State: StateOK, Detail: fmt.Sprintf("recent xcode build at %s (project %s): activity log empty", info.ModTime().UTC().Format(time.RFC3339), project)}
	case xcactivitylog.OutcomeUnparsed:
		return Result{State: StateOK, Detail: fmt.Sprintf("recent xcode build at %s (project %s): no CompilationCacheMetrics line (regex drift?)", info.ModTime().UTC().Format(time.RFC3339), project)}
	case xcactivitylog.OutcomeFileMissing, xcactivitylog.OutcomeReadError:
		return Result{State: StateOK, Detail: fmt.Sprintf("recent xcode build at %s (project %s): activity log unreadable", info.ModTime().UTC().Format(time.RFC3339), project)}
	default:
		return Result{State: StateOK, Detail: fmt.Sprintf("recent xcode build at %s (project %s): unknown reader outcome", info.ModTime().UTC().Format(time.RFC3339), project)}
	}
}

// findNewestRecentActivityLog walks ~/Library/Developer/Xcode/DerivedData/*/Logs/Build/
// for `.xcactivitylog` files modified after cutoff. Returns the newest path and
// the enclosing project dir name (e.g. "MyApp-abc123def"); empty string + nil
// error means no match.
func findNewestRecentActivityLog(derivedDataRoot string, cutoff time.Time) (string, string, error) {
	entries, err := os.ReadDir(derivedDataRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf("DerivedData missing at %s", derivedDataRoot)
		}

		return "", "", fmt.Errorf("read %s: %w", derivedDataRoot, err)
	}

	var (
		newestPath string
		newestMod  time.Time
		project    string
	)

	for _, projDir := range entries {
		if !projDir.IsDir() {
			continue
		}

		buildLogsDir := filepath.Join(derivedDataRoot, projDir.Name(), "Logs", "Build")
		logs, err := os.ReadDir(buildLogsDir)
		if err != nil {
			continue
		}

		for _, log := range logs {
			if log.IsDir() || !strings.HasSuffix(log.Name(), ".xcactivitylog") {
				continue
			}

			info, err := log.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				continue
			}
			if info.ModTime().After(newestMod) {
				newestMod = info.ModTime()
				newestPath = filepath.Join(buildLogsDir, log.Name())
				project = projDir.Name()
			}
		}
	}

	return newestPath, project, nil
}
