// Package xcodebuildinfo shells out to `xcodebuild` to discover schemes,
// configurations, and destinations for the interactive picker.
package xcodebuildinfo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// Client runs xcodebuild from WorkDir (or the current directory when empty).
// Setting WorkDir to the project directory lets callers pass bare
// workspace/project filenames instead of absolute paths.
type Client struct {
	WorkDir string
}

// New returns a Client scoped to workDir.
func New(workDir string) *Client {
	return &Client{WorkDir: workDir}
}

func (c *Client) ListSchemesAndConfigurations(ctx context.Context, workspace, project string) ([]string, []string, error) {
	info, err := c.runXcodebuildList(ctx, workspace, project)
	if err != nil {
		return nil, nil, err
	}

	// Workspaces don't expose configurations directly — callers drop to a
	// free-text input, matching the pre-picker UX.
	switch {
	case info.Workspace != nil:
		return info.Workspace.Schemes, nil, nil
	case info.Project != nil:
		return info.Project.Schemes, info.Project.Configurations, nil
	}

	return nil, nil, nil
}

func (c *Client) ShowDestinations(ctx context.Context, workspace, project, scheme string) ([]string, error) {
	args := []string{"-showdestinations", "-scheme", scheme}

	switch {
	case workspace != "":
		args = append(args, "-workspace", workspace)
	case project != "":
		args = append(args, "-project", project)
	}

	cmd := exec.CommandContext(ctx, "xcodebuild", args...)
	cmd.Dir = c.WorkDir

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("xcodebuild -showdestinations: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}

	return parseShowDestinations(string(out)), nil
}

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

func (c *Client) runXcodebuildList(ctx context.Context, workspace, project string) (xcodebuildListOutput, error) {
	args := []string{"-list", "-json"}

	switch {
	case workspace != "":
		args = append(args, "-workspace", workspace)
	case project != "":
		args = append(args, "-project", project)
	}

	cmd := exec.CommandContext(ctx, "xcodebuild", args...)
	cmd.Dir = c.WorkDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		combined := strings.TrimSpace(stdout.String() + "\n" + stderr.String())

		return xcodebuildListOutput{}, fmt.Errorf("xcodebuild -list -json: %w (output: %s)", err, combined)
	}

	return parseXcodebuildList(stdout.Bytes())
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

	// Xcode emits two blocks: "Available destinations" (usable) and
	// "Ineligible destinations" (unavailable — e.g. uninstalled simulators).
	// Only the former belongs in the picker.
	inAvailable := false

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "Available destinations"):
			inAvailable = true

			continue
		case strings.HasPrefix(line, "Ineligible destinations"):
			inAvailable = false

			continue
		}

		if !inAvailable {
			continue
		}

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

	// "Any iOS Device" / "Any Mac" entries carry a platform + name; emit as
	// generic/platform=X for consistency with Xcode's own -destination shorthand.
	name := fields["name"]
	if name == "" || strings.HasPrefix(strings.ToLower(name), "any ") {
		return "generic/platform=" + platform
	}

	return "platform=" + platform + ",name=" + name
}
