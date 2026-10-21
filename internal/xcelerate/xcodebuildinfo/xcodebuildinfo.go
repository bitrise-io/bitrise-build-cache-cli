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
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
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

// Destination is one parsed row of `xcodebuild -showdestinations` output,
// retaining enough metadata for the picker to render a friendly label and
// pre-select a sensible default.
type Destination struct {
	// Canonical is the exact `-destination` arg form; what the picker commits.
	Canonical string
	Platform  string
	Name      string
	OS        string
	ID        string
	Arch      string
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

func (c *Client) ShowDestinations(ctx context.Context, workspace, project, scheme string) ([]Destination, error) {
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

func parseShowDestinations(output string) []Destination {
	// Xcode may emit an "Ineligible destinations" block (uninstalled sims,
	// incompatible platforms). Everything else — whether prefaced by
	// "Available destinations" (older Xcode) or "Destinations compatible with
	// the "<scheme>" scheme:" (Xcode 15+) — is usable.
	inAvailable := true

	parsed := []Destination{}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "Ineligible destinations"):
			inAvailable = false

			continue
		case strings.HasPrefix(line, "Available destinations"),
			strings.HasPrefix(line, "Destinations compatible with"):
			inAvailable = true

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

		dest := destinationFromFields(fields)
		if dest.Canonical == "" {
			continue
		}

		parsed = append(parsed, dest)
	}

	return rankAndDedup(parsed)
}

func destinationFromFields(fields map[string]string) Destination {
	return Destination{
		Canonical: canonicalDestination(fields),
		Platform:  fields["platform"],
		Name:      fields["name"],
		OS:        fields["OS"],
		ID:        fields["id"],
		Arch:      fields["arch"],
	}
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

// DefaultDestination returns the entry the picker should pre-select. The
// ranked winner is the newest iPhone simulator on the highest-numbered iOS;
// absent any iOS Simulator entries we fall back to the first row Xcode
// printed.
func DefaultDestination(dests []Destination) (Destination, bool) {
	if len(dests) == 0 {
		return Destination{}, false
	}

	ranked := rankAndDedup(dests)

	return ranked[0], true
}

// rankAndDedup sorts destinations by picker preference (iOS Simulator > newest
// OS > newest iPhone) and collapses rows that share a canonical form, keeping
// the ranked winner.
func rankAndDedup(dests []Destination) []Destination {
	out := append([]Destination(nil), dests...)
	sort.SliceStable(out, func(i, j int) bool { return lessDestination(out[i], out[j]) })

	seen := map[string]struct{}{}
	kept := out[:0]

	for _, d := range out {
		if _, dup := seen[d.Canonical]; dup {
			continue
		}

		seen[d.Canonical] = struct{}{}
		kept = append(kept, d)
	}

	return kept
}

func lessDestination(a, b Destination) bool {
	aSim, bSim := a.Platform == "iOS Simulator", b.Platform == "iOS Simulator"
	if aSim != bSim {
		return aSim
	}

	if !aSim {
		return false
	}

	if c := compareOS(a.OS, b.OS); c != 0 {
		return c > 0
	}

	return compareIPhoneName(a.Name, b.Name) > 0
}

// compareOS returns >0 when a is newer than b; delegates to semver (which
// handles partial versions like `17.4`) and falls back to lexical compare
// for anything semver can't parse.
func compareOS(a, b string) int {
	av, bv := "v"+a, "v"+b

	switch {
	case semver.IsValid(av) && semver.IsValid(bv):
		return semver.Compare(av, bv)
	case semver.IsValid(av):
		return 1
	case semver.IsValid(bv):
		return -1
	}

	return strings.Compare(a, b)
}

var iPhoneNameRe = regexp.MustCompile(`^iPhone\s+(\d+)(?:\s+(.*))?$`)

// compareIPhoneName returns >0 when a is a "newer" iPhone than b: higher model
// number wins first, then suffix tier Pro Max > Pro > Plus > plain > SE. Rows
// that don't match the iPhone shape fall through to a lexical compare.
func compareIPhoneName(a, b string) int {
	aNum, aSuffix, aOk := parseIPhoneName(a)
	bNum, bSuffix, bOk := parseIPhoneName(b)

	switch {
	case aOk && bOk:
		if aNum != bNum {
			return aNum - bNum
		}

		return iPhoneSuffixTier(aSuffix) - iPhoneSuffixTier(bSuffix)
	case aOk:
		return 1
	case bOk:
		return -1
	}

	return strings.Compare(a, b)
}

func parseIPhoneName(name string) (int, string, bool) {
	m := iPhoneNameRe.FindStringSubmatch(name)
	if m == nil {
		return 0, "", false
	}

	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, "", false
	}

	return n, strings.TrimSpace(m[2]), true
}

func iPhoneSuffixTier(suffix string) int {
	switch strings.ToLower(suffix) {
	case "pro max":
		return 4
	case "pro":
		return 3
	case "plus":
		return 2
	case "":
		return 1
	case "se":
		return 0
	}

	return 1
}
