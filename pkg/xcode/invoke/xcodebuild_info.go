package invoke

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

//go:generate moq -stub -out xcodebuild_info_mock_test.go -pkg invoke . xcodebuildInfoProvider

// xcodebuildInfoProvider surfaces the candidate lists a picker needs. Split
// per-field so the default Prompt can chain scheme → configuration →
// destination without leaking xcodebuild subprocess wiring into the caller.
type xcodebuildInfoProvider interface {
	ListSchemes(ctx context.Context, workspace, project string) ([]string, error)
	ListConfigurations(ctx context.Context, workspace, project string) ([]string, error)
	ShowDestinations(ctx context.Context, workspace, project, scheme string) ([]string, error)
}

type execXcodebuildInfo struct{}

func (e execXcodebuildInfo) ListSchemes(ctx context.Context, workspace, project string) ([]string, error) {
	info, err := runXcodebuildList(ctx, workspace, project)
	if err != nil {
		return nil, err
	}

	switch {
	case info.Workspace != nil:
		return info.Workspace.Schemes, nil
	case info.Project != nil:
		return info.Project.Schemes, nil
	}

	return nil, nil
}

func (e execXcodebuildInfo) ListConfigurations(ctx context.Context, workspace, project string) ([]string, error) {
	info, err := runXcodebuildList(ctx, workspace, project)
	if err != nil {
		return nil, err
	}

	// Workspaces don't expose configurations directly. Fall back to the empty
	// slice — the caller drops to a free-text input, matching the pre-picker UX.
	if info.Project != nil {
		return info.Project.Configurations, nil
	}

	return nil, nil
}

func (e execXcodebuildInfo) ShowDestinations(ctx context.Context, workspace, project, scheme string) ([]string, error) {
	args := []string{"-showdestinations", "-scheme", scheme}

	switch {
	case workspace != "":
		args = append(args, "-workspace", workspace)
	case project != "":
		args = append(args, "-project", project)
	}

	cmd := exec.CommandContext(ctx, "xcodebuild", args...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("xcodebuild -showdestinations: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}

	return parseShowDestinations(string(out)), nil
}

// Private — parsing / subprocess helpers

// xcodebuildListOutput mirrors the top-level of `xcodebuild -list -json`.
// Either Workspace or Project is populated, never both.
type xcodebuildListOutput struct {
	Workspace *xcodebuildWorkspaceInfo `json:"workspace,omitempty"`
	Project   *xcodebuildProjectInfo   `json:"project,omitempty"`
}

type xcodebuildWorkspaceInfo struct {
	Name    string   `json:"name"`
	Schemes []string `json:"schemes"`
}

type xcodebuildProjectInfo struct {
	Name           string   `json:"name"`
	Schemes        []string `json:"schemes"`
	Configurations []string `json:"configurations"`
	Targets        []string `json:"targets"`
}

func runXcodebuildList(ctx context.Context, workspace, project string) (xcodebuildListOutput, error) {
	args := []string{"-list", "-json"}

	switch {
	case workspace != "":
		args = append(args, "-workspace", workspace)
	case project != "":
		args = append(args, "-project", project)
	}

	cmd := exec.CommandContext(ctx, "xcodebuild", args...)

	out, err := cmd.Output()
	if err != nil {
		return xcodebuildListOutput{}, fmt.Errorf("xcodebuild -list -json: %w", err)
	}

	return parseXcodebuildList(out)
}

func parseXcodebuildList(raw []byte) (xcodebuildListOutput, error) {
	var info xcodebuildListOutput
	if err := json.Unmarshal(raw, &info); err != nil {
		return xcodebuildListOutput{}, fmt.Errorf("decode xcodebuild -list -json output: %w", err)
	}

	return info, nil
}

// Each destination line looks like `{ platform:iOS Simulator, id:..., OS:17.4, name:iPhone 15 }`.
// Value tokens may contain spaces; the separator is `, key:`. Regex keeps parsing permissive.
var destinationKeyValueRe = regexp.MustCompile(`([A-Za-z]+):([^,}]+)`)

func parseShowDestinations(output string) []string {
	seen := map[string]struct{}{}
	dests := []string{}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
			continue
		}

		inner := strings.TrimSuffix(strings.TrimPrefix(line, "{"), "}")

		fields := map[string]string{}

		for _, m := range destinationKeyValueRe.FindAllStringSubmatch(inner, -1) {
			key := strings.TrimSpace(m[1])
			val := strings.TrimSpace(m[2])
			fields[key] = val
		}

		dest := canonicalDestination(fields)
		if dest == "" {
			continue
		}

		if _, dup := seen[dest]; dup {
			continue
		}

		seen[dest] = struct{}{}
		dests = append(dests, dest)
	}

	sort.Strings(dests)

	return dests
}

func canonicalDestination(fields map[string]string) string {
	platform := fields["platform"]
	if platform == "" {
		return ""
	}

	name := fields["name"]

	// "Any iOS Device" / "Any Mac" entries only carry a platform + name; emit as
	// generic/platform=X for consistency with Xcode's own -destination shorthand.
	if strings.HasPrefix(strings.ToLower(name), "any ") {
		return "generic/platform=" + platform
	}

	if name == "" {
		return "generic/platform=" + platform
	}

	return "platform=" + platform + ",name=" + name
}
