package collect

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/payload"
)

// BuildSettingsCollector plucks a known-good subset of build settings from
// `xcodebuild -showBuildSettings -json`.
type BuildSettingsCollector struct{}

func (BuildSettingsCollector) Name() string { return "buildsettings" }

type showBuildSettingsRow struct {
	Target        string            `json:"target"`
	BuildSettings map[string]string `json:"buildSettings"`
}

func (BuildSettingsCollector) Collect(ctx context.Context, in Input) (Output, error) {
	if in.XcodebuildInfo == nil || in.Scheme == "" {
		return Output{}, nil
	}

	raw, err := in.XcodebuildInfo.ShowBuildSettings(ctx, in.Scheme, in.Project)
	if err != nil {
		return Output{}, fmt.Errorf("showBuildSettings: %w", err)
	}

	snap, err := parseShowBuildSettings(raw)
	if err != nil {
		return Output{}, err
	}

	return Output{BuildSettings: snap}, nil
}

// parseShowBuildSettings reads the first row (the scheme's top-level target;
// subsequent rows are dependencies) and extracts the known-good field set.
func parseShowBuildSettings(raw []byte) (*payload.BuildSettingsSnapshot, error) {
	var rows []showBuildSettingsRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("decode showBuildSettings json: %w", err)
	}

	if len(rows) == 0 {
		return nil, nil //nolint:nilnil // nothing to report, not an error
	}

	bs := rows[0].BuildSettings

	return &payload.BuildSettingsSnapshot{
		SwiftOptimizationLevel:       pick(bs, "SWIFT_OPTIMIZATION_LEVEL"),
		SwiftCompilationMode:         pick(bs, "SWIFT_COMPILATION_MODE"),
		SwiftWholeModuleOptimization: pick(bs, "SWIFT_WHOLE_MODULE_OPTIMIZATION"),
		GCCOptimizationLevel:         pick(bs, "GCC_OPTIMIZATION_LEVEL"),
		EnableBitcode:                pick(bs, "ENABLE_BITCODE"),
		DebugInformationFormat:       pick(bs, "DEBUG_INFORMATION_FORMAT"),
		DeploymentTarget:             pickDeploymentTarget(bs),
		Archs:                        pick(bs, "ARCHS"),
		ValidArchs:                   pick(bs, "VALID_ARCHS"),
	}, nil
}

func pick(m map[string]string, key string) string {
	if v, ok := m[key]; ok {
		return v
	}

	return ""
}

// pickDeploymentTarget returns the first non-empty *_DEPLOYMENT_TARGET —
// the key varies by platform and the scheme does not publish a stable name.
func pickDeploymentTarget(m map[string]string) string {
	for _, k := range []string{
		"IPHONEOS_DEPLOYMENT_TARGET",
		"MACOSX_DEPLOYMENT_TARGET",
		"TVOS_DEPLOYMENT_TARGET",
		"WATCHOS_DEPLOYMENT_TARGET",
		"XROS_DEPLOYMENT_TARGET",
	} {
		if v := pick(m, k); v != "" {
			return v
		}
	}

	return ""
}
