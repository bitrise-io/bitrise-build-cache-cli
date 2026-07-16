package xcode_app

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// BridgeXCConfigName is the sibling xcconfig written next to a .xcodeproj when
// `xcode-app link` falls back to sibling-bridge mode (no in-tree .xcconfig
// files exist to append into).
const BridgeXCConfigName = paths.XcodeAppBridgeXCConfigName

// schemeGroup identifies workspace FileRef / Group entries whose path is
// resolved relative to the containing workspace or group.
const schemeGroup = "group"

// trailerCommentLine is the exact comment we prepend to every in-place append;
// detectTrailer / stripTrailerFromContent match it byte-for-byte so re-running
// link only produces one trailer per xcconfig.
const trailerCommentLine = "// Bitrise Build Cache — auto-appended by `bitrise-build-cache xcode-app link`. Remove via `unlink`."

// trailerIncludeDirective is the include keyword we emit. `#include?` (optional
// include) keeps the append committable — teammates or CI without the CLI
// installed silently skip the include instead of failing the build with
// `Unable to open base configuration reference file`.
const trailerIncludeDirective = "#include?"

// LinkMode records which strategy `link` used for a given project. Persisted
// in the state file so `unlink` reverts the exact set of edits.
type LinkMode string

const (
	LinkModeInPlace LinkMode = "inplace"
	LinkModeSibling LinkMode = "sibling"
)

// LinkParams targets a single .xcodeproj or .xcworkspace.
type LinkParams struct {
	// ProjectPath is either <name>.xcodeproj or <name>.xcworkspace (absolute or relative).
	ProjectPath string

	// OverrideXCConfigPath is the absolute path of the user-global override
	// xcconfig (e.g. ~/.bitrise-xcelerate/xcode-app.xcconfig). Must be absolute
	// — Xcode's xcconfig loader does NOT expand `~` in include paths.
	OverrideXCConfigPath string

	// StateDir is the absolute dir that holds per-project link state JSON. Empty
	// = the caller has opted out of state persistence (in-memory tests). In
	// production, this points at Paths.LinkedProjectsDir().
	StateDir string
}

type LinkResult struct {
	// ModifiedXCConfigs are the absolute paths of project xcconfig files we
	// appended a trailer to (in-place mode).
	ModifiedXCConfigs []string

	// AlreadyLinked lists xcconfigs that already contained an up-to-date
	// trailer — no-op for them.
	AlreadyLinked []string

	// BridgeFiles are absolute paths of sibling bridge xcconfigs written when a
	// project has no in-tree .xcconfig files (fallback mode).
	BridgeFiles []string
}

type UnlinkResult struct {
	// ModifiedXCConfigs are absolute paths whose auto-appended trailer we stripped.
	ModifiedXCConfigs []string

	// RemovedBridgeFiles are absolute paths of sibling bridge xcconfigs deleted.
	RemovedBridgeFiles []string

	// MissingBridgeFiles are sibling bridge paths that were expected (per state)
	// but already absent — reported, not an error.
	MissingBridgeFiles []string

	// NoOp is true when nothing about the project needed changing.
	NoOp bool
}

// linkedProjectState is the on-disk record `unlink` reads to know exactly which
// xcconfigs to revert. Kept per-project (one file per .xcodeproj).
type linkedProjectState struct {
	ProjectPath       string    `json:"projectPath"`
	Mode              LinkMode  `json:"mode"`
	ModifiedXCConfigs []string  `json:"modifiedXCConfigs,omitempty"`
	BridgeFile        string    `json:"bridgeFile,omitempty"`
	LinkedAt          time.Time `json:"linkedAt"`
}

// Link wires each referenced .xcodeproj to the override xcconfig. Prefers
// appending `#include? "<override>"` to existing in-tree .xcconfig files so
// the cache engages automatically after the next Xcode build; falls back to
// the legacy sibling-bridge behaviour only for projects with no xcconfigs.
func Link(osProxy osProxyForLink, p LinkParams) (LinkResult, error) {
	if err := validateOverrideXCConfigPath(p.OverrideXCConfigPath); err != nil {
		return LinkResult{}, err
	}

	projectPaths, err := resolveProjectPaths(osProxy, p.ProjectPath)
	if err != nil {
		return LinkResult{}, err
	}

	var result LinkResult
	for _, projPath := range projectPaths {
		perProject, err := linkOneProject(osProxy, p, projPath)
		if err != nil {
			return result, err
		}

		result.ModifiedXCConfigs = append(result.ModifiedXCConfigs, perProject.ModifiedXCConfigs...)
		result.AlreadyLinked = append(result.AlreadyLinked, perProject.AlreadyLinked...)
		result.BridgeFiles = append(result.BridgeFiles, perProject.BridgeFiles...)
	}

	return result, nil
}

func linkOneProject(osProxy osProxyForLink, p LinkParams, projectPath string) (LinkResult, error) {
	dir := filepath.Dir(projectPath)

	xcconfigs, err := findXCConfigs(dir)
	if err != nil {
		return LinkResult{}, fmt.Errorf("scan xcconfigs in %s: %w", dir, err)
	}

	// Prior sibling-bridge runs are stale under the new behaviour. Clean up so
	// the project doesn't carry both an in-place include AND the old sibling
	// (which could still be selected as a base config).
	staleBridge := filepath.Join(dir, BridgeXCConfigName)
	if _, found, ferr := osProxy.ReadFileIfExists(staleBridge); ferr == nil && found {
		_ = osProxy.Remove(staleBridge)
	}

	var result LinkResult
	switch {
	case len(xcconfigs) > 0:
		res, err := linkInPlace(osProxy, p, xcconfigs)
		if err != nil {
			return result, err
		}

		result = res
		linkedFiles := make([]string, 0, len(result.ModifiedXCConfigs)+len(result.AlreadyLinked))
		linkedFiles = append(linkedFiles, result.ModifiedXCConfigs...)
		linkedFiles = append(linkedFiles, result.AlreadyLinked...)
		if err := saveState(osProxy, p, linkedProjectState{
			ProjectPath:       projectPath,
			Mode:              LinkModeInPlace,
			ModifiedXCConfigs: linkedFiles,
			LinkedAt:          time.Now().UTC(),
		}); err != nil {
			return result, err
		}
	default:
		res, err := linkSibling(osProxy, p, dir)
		if err != nil {
			return result, err
		}

		result = res
		bridgeFile := ""
		if len(result.BridgeFiles) > 0 {
			bridgeFile = result.BridgeFiles[0]
		}
		if err := saveState(osProxy, p, linkedProjectState{
			ProjectPath: projectPath,
			Mode:        LinkModeSibling,
			BridgeFile:  bridgeFile,
			LinkedAt:    time.Now().UTC(),
		}); err != nil {
			return result, err
		}
	}

	return result, nil
}

// linkInPlace appends (or refreshes) the Bitrise trailer on each xcconfig. Rewrites
// the trailer whenever the override path drifts, so re-running `link` picks up a
// new override.
func linkInPlace(osProxy osProxyForLink, p LinkParams, xcconfigs []string) (LinkResult, error) {
	var result LinkResult
	for _, path := range xcconfigs {
		existing, found, err := osProxy.ReadFileIfExists(path)
		if err != nil {
			return result, fmt.Errorf("read %s: %w", path, err)
		}
		if !found {
			continue
		}

		mode, err := fileMode(osProxy, path)
		if err != nil {
			return result, err
		}

		hasTrailer, currentPath, currentDirective := detectTrailer(existing)
		switch {
		case hasTrailer && currentPath == p.OverrideXCConfigPath && currentDirective == trailerIncludeDirective:
			result.AlreadyLinked = append(result.AlreadyLinked, path)
		case hasTrailer:
			// Trailer is stale — either override path drifted or the directive
			// form is the legacy non-optional `#include`. Rewrite in both cases.
			stripped := stripTrailerFromContent(existing)
			if err := atomicWrite(osProxy, path, stripped+buildTrailer(p.OverrideXCConfigPath), mode); err != nil {
				return result, err
			}

			result.ModifiedXCConfigs = append(result.ModifiedXCConfigs, path)
		default:
			if err := atomicWrite(osProxy, path, appendTrailerToContent(existing, p.OverrideXCConfigPath), mode); err != nil {
				return result, err
			}

			result.ModifiedXCConfigs = append(result.ModifiedXCConfigs, path)
		}
	}

	return result, nil
}

// linkSibling writes the legacy bitrise-build-cache-xcode.xcconfig next to
// the project. Kept as the fallback for xcconfig-free projects — the user
// still needs to select it as the base configuration in Xcode manually.
func linkSibling(osProxy osProxyForLink, p LinkParams, projectDir string) (LinkResult, error) {
	body := renderBridge(p.OverrideXCConfigPath)
	bridgePath := filepath.Join(projectDir, BridgeXCConfigName)

	existing, found, err := osProxy.ReadFileIfExists(bridgePath)
	if err != nil {
		return LinkResult{}, fmt.Errorf("read %s: %w", bridgePath, err)
	}
	if found && existing == body {
		return LinkResult{BridgeFiles: []string{bridgePath}, AlreadyLinked: []string{bridgePath}}, nil
	}

	if err := atomicWrite(osProxy, bridgePath, body, 0o644); err != nil {
		return LinkResult{}, err
	}

	return LinkResult{BridgeFiles: []string{bridgePath}}, nil
}

// Unlink reverts every edit `Link` made to the referenced project(s). Reads
// the per-project state file so we know the exact set of xcconfig files to
// touch even if the tree has shifted since link.
func Unlink(osProxy osProxyForLink, p LinkParams) (UnlinkResult, error) {
	projectPaths, err := resolveProjectPaths(osProxy, p.ProjectPath)
	if err != nil {
		return UnlinkResult{}, err
	}

	var result UnlinkResult
	anyWork := false
	for _, projPath := range projectPaths {
		perProject, err := unlinkOneProject(osProxy, p, projPath)
		if err != nil {
			return result, err
		}

		if len(perProject.ModifiedXCConfigs) > 0 || len(perProject.RemovedBridgeFiles) > 0 {
			anyWork = true
		}

		result.ModifiedXCConfigs = append(result.ModifiedXCConfigs, perProject.ModifiedXCConfigs...)
		result.RemovedBridgeFiles = append(result.RemovedBridgeFiles, perProject.RemovedBridgeFiles...)
		result.MissingBridgeFiles = append(result.MissingBridgeFiles, perProject.MissingBridgeFiles...)
	}

	result.NoOp = !anyWork

	return result, nil
}

func unlinkOneProject(osProxy osProxyForLink, p LinkParams, projectPath string) (UnlinkResult, error) {
	var result UnlinkResult

	state, hasState, err := loadState(osProxy, p, projectPath)
	if err != nil {
		return result, err
	}

	if hasState {
		if err := revertRecordedState(osProxy, p, projectPath, state, &result); err != nil {
			return result, err
		}
	}

	if err := defensiveSweep(osProxy, projectPath, hasState, state, &result); err != nil {
		return result, err
	}

	return result, nil
}

// revertRecordedState strips the trailer from every xcconfig the state file
// records, removes the sibling bridge if the state points at one, and clears
// the state file itself.
func revertRecordedState(osProxy osProxyForLink, p LinkParams, projectPath string, state linkedProjectState, result *UnlinkResult) error {
	for _, path := range state.ModifiedXCConfigs {
		stripped, err := stripTrailerFromFile(osProxy, path)
		if err != nil {
			return err
		}
		if stripped {
			result.ModifiedXCConfigs = append(result.ModifiedXCConfigs, path)
		}
	}

	if state.BridgeFile != "" {
		outcome := removeBridgeIfPresent(osProxy, state.BridgeFile)
		switch outcome {
		case bridgeRemoved:
			result.RemovedBridgeFiles = append(result.RemovedBridgeFiles, state.BridgeFile)
		case bridgeMissing:
			result.MissingBridgeFiles = append(result.MissingBridgeFiles, state.BridgeFile)
		case bridgeUnchanged:
			// Non-"not exist" error already surfaced by osProxy.Remove — swallow;
			// unlink is best-effort for cleanup.
		}
	}

	return removeStateFile(osProxy, p, projectPath)
}

// defensiveSweep catches leftovers from broken/interrupted prior runs: a stale
// sibling bridge next to the project (removed even without a state file), and
// (only when no state exists) any trailer we can find in the tree. Keeps unlink
// repeatable even if state was manually deleted.
func defensiveSweep(osProxy osProxyForLink, projectPath string, hasState bool, state linkedProjectState, result *UnlinkResult) error {
	dir := filepath.Dir(projectPath)
	bridge := filepath.Join(dir, BridgeXCConfigName)
	if !hasState || state.BridgeFile == "" {
		if outcome := removeBridgeIfPresent(osProxy, bridge); outcome == bridgeRemoved {
			result.RemovedBridgeFiles = append(result.RemovedBridgeFiles, bridge)
		}
	}

	if hasState {
		return nil
	}

	xcconfigs, err := findXCConfigs(dir)
	if err != nil {
		return fmt.Errorf("scan xcconfigs in %s: %w", dir, err)
	}
	for _, path := range xcconfigs {
		stripped, err := stripTrailerFromFile(osProxy, path)
		if err != nil {
			return err
		}
		if stripped {
			result.ModifiedXCConfigs = append(result.ModifiedXCConfigs, path)
		}
	}

	return nil
}

// stripTrailerFromFile is a no-op for a missing file and returns whether it
// changed anything so callers can report only actual reverts to the user.
func stripTrailerFromFile(osProxy osProxyForLink, path string) (bool, error) {
	existing, found, err := osProxy.ReadFileIfExists(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	if !found {
		return false, nil
	}

	stripped := stripTrailerFromContent(existing)
	if stripped == existing {
		return false, nil
	}

	mode, err := fileMode(osProxy, path)
	if err != nil {
		return false, err
	}

	if err := atomicWrite(osProxy, path, stripped, mode); err != nil {
		return false, err
	}

	return true, nil
}

// resolveProjectPaths returns the absolute .xcodeproj paths referenced by the
// input. For a single .xcodeproj that's just the input; for a .xcworkspace we
// fan out to the resolved FileRef entries.
func resolveProjectPaths(osProxy osProxyForLink, projectPath string) ([]string, error) {
	if projectPath == "" {
		return nil, errors.New("project path is empty")
	}

	abs, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute path for %s: %w", projectPath, err)
	}

	info, err := osProxy.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s: expected a .xcodeproj or .xcworkspace directory", abs)
	}

	switch strings.ToLower(filepath.Ext(abs)) {
	case ".xcodeproj":
		return []string{abs}, nil
	case ".xcworkspace":
		return resolveWorkspaceProjectPaths(osProxy, abs)
	default:
		return nil, fmt.Errorf("%s: unsupported extension — expected .xcodeproj or .xcworkspace", abs)
	}
}

// workspaceContents mirrors the minimal shape of contents.xcworkspacedata we
// care about. Xcode workspaces are XML with FileRef entries pointing at
// projects via a `group:` / `self:` / `container:` scheme.
type workspaceContents struct {
	XMLName  xml.Name           `xml:"Workspace"`
	FileRefs []workspaceFileRef `xml:"FileRef"`
	Groups   []workspaceGroup   `xml:"Group"`
}

type workspaceGroup struct {
	Location string             `xml:"location,attr"`
	FileRefs []workspaceFileRef `xml:"FileRef"`
	Groups   []workspaceGroup   `xml:"Group"`
}

type workspaceFileRef struct {
	Location string `xml:"location,attr"`
}

func resolveWorkspaceProjectPaths(osProxy osProxyForLink, workspacePath string) ([]string, error) {
	contentsPath := filepath.Join(workspacePath, "contents.xcworkspacedata")

	raw, found, err := osProxy.ReadFileIfExists(contentsPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", contentsPath, err)
	}
	if !found {
		return nil, fmt.Errorf("missing %s — workspace appears malformed", contentsPath)
	}

	var ws workspaceContents
	if err := xml.Unmarshal([]byte(raw), &ws); err != nil {
		return nil, fmt.Errorf("parse %s: %w", contentsPath, err)
	}

	refs := collectFileRefs(ws.FileRefs, ws.Groups, "")

	seen := make(map[string]struct{}, len(refs))
	projects := make([]string, 0, len(refs))
	for _, ref := range refs {
		resolved, ok := resolveWorkspaceFileRef(workspacePath, ref.Location)
		if !ok {
			continue
		}

		if !strings.EqualFold(filepath.Ext(resolved), ".xcodeproj") {
			continue
		}

		if _, err := osProxy.Stat(resolved); err != nil {
			continue
		}

		if _, dup := seen[resolved]; dup {
			continue
		}
		seen[resolved] = struct{}{}
		projects = append(projects, resolved)
	}

	if len(projects) == 0 {
		return nil, fmt.Errorf("%s: no .xcodeproj FileRefs found in workspace", workspacePath)
	}

	return projects, nil
}

// collectFileRefs flattens the FileRef entries under Workspace and any nested
// Group nodes. Some tool-generated workspaces write workspace-relative paths
// directly on nested FileRefs rather than group-relative ones — emit both
// candidates and let the stat downstream keep whichever exists.
func collectFileRefs(refs []workspaceFileRef, groups []workspaceGroup, prefix string) []workspaceFileRef {
	out := make([]workspaceFileRef, 0, len(refs)*2)
	for _, r := range refs {
		if prefix == "" {
			out = append(out, r)

			continue
		}

		prefixed := workspaceFileRef{Location: joinGroupPrefix(prefix, r.Location)}
		out = append(out, prefixed)
		if prefixed.Location != r.Location {
			out = append(out, r)
		}
	}
	for _, g := range groups {
		out = append(out, collectFileRefs(g.FileRefs, g.Groups, joinGroupLocation(prefix, g.Location))...)
	}

	return out
}

func joinGroupLocation(parentPrefix, groupLocation string) string {
	scheme, rest, ok := splitScheme(groupLocation)
	if !ok || scheme != schemeGroup {
		return parentPrefix
	}

	if parentPrefix == "" {
		return rest
	}

	if rest == "" {
		return parentPrefix
	}

	return parentPrefix + "/" + rest
}

func joinGroupPrefix(prefix, location string) string {
	if prefix == "" {
		return location
	}
	scheme, rest, ok := splitScheme(location)
	if !ok || scheme != schemeGroup {
		return location
	}

	if rest == "" {
		return schemeGroup + ":" + prefix
	}

	return schemeGroup + ":" + prefix + "/" + rest
}

func resolveWorkspaceFileRef(workspacePath, location string) (string, bool) {
	scheme, rest, ok := splitScheme(location)
	if !ok {
		return "", false
	}

	switch scheme {
	case "self":
		return workspacePath, true
	case schemeGroup:
		return filepath.Join(filepath.Dir(workspacePath), rest), true
	case "container":
		return filepath.Join(filepath.Dir(workspacePath), rest), true
	case "absolute":
		return rest, true
	default:
		return "", false
	}
}

func splitScheme(location string) (string, string, bool) {
	idx := strings.Index(location, ":")
	if idx < 0 {
		return "", "", false
	}

	return location[:idx], location[idx+1:], true
}

// findXCConfigs walks `root` recursively for `.xcconfig` files, skipping build
// artefact dirs and the sibling bridge from prior link runs.
func findXCConfigs(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Missing intermediates or permission blips: skip that subtree, keep going.
			if errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipDir
			}

			return err
		}

		if d.IsDir() {
			base := d.Name()
			// Skip build artefacts, dep-manager caches, and Xcode package dirs
			// we never want to walk into. `.git` is included so we don't pull
			// in xcconfigs from unrelated worktrees.
			switch base {
			case ".build", "DerivedData", "Pods", "Carthage", ".git", "node_modules":
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.EqualFold(filepath.Ext(path), ".xcconfig") {
			return nil
		}

		if filepath.Base(path) == BridgeXCConfigName {
			return nil
		}

		out = append(out, path)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}

	return out, nil
}

// detectTrailer returns (found, path, directive). The trailer is the two-line
// block appended by linkInPlace: our exact comment line, then an include
// directive with a quoted path. We still detect the legacy `#include` form
// (no `?`) so re-running link upgrades it to `#include?` in place.
func detectTrailer(content string) (bool, string, string) {
	lines := strings.Split(content, "\n")
	for i := range len(lines) - 1 {
		if strings.TrimSpace(lines[i]) != trailerCommentLine {
			continue
		}

		next := strings.TrimSpace(lines[i+1])
		if path, directive, ok := parseIncludeLine(next); ok {
			return true, path, directive
		}
	}

	return false, "", ""
}

func parseIncludeLine(line string) (string, string, bool) {
	for _, prefix := range []string{trailerIncludeDirective + " ", "#include "} {
		if !strings.HasPrefix(line, prefix) {
			continue
		}

		rest := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		if len(rest) < 2 || rest[0] != '"' || rest[len(rest)-1] != '"' {
			return "", "", false
		}

		return rest[1 : len(rest)-1], strings.TrimSpace(prefix), true
	}

	return "", "", false
}

// stripTrailerFromContent removes the trailer block (blank line + comment +
// include) if present. Preserves everything else byte-for-byte so a subsequent
// unlink leaves the file identical to its pre-link state.
func stripTrailerFromContent(content string) string {
	lines := strings.Split(content, "\n")
	for i := range len(lines) - 1 {
		if strings.TrimSpace(lines[i]) != trailerCommentLine {
			continue
		}

		if _, _, ok := parseIncludeLine(strings.TrimSpace(lines[i+1])); !ok {
			continue
		}

		// Drop the trailer + any single preceding blank separator line we may
		// have added; keep everything up to that point intact.
		start := i
		if start > 0 && strings.TrimSpace(lines[start-1]) == "" {
			start--
		}
		end := i + 2

		return strings.Join(append(append([]string{}, lines[:start]...), lines[end:]...), "\n")
	}

	return content
}

// appendTrailerToContent glues the trailer onto existing xcconfig content,
// preceded by a blank separator line so the trailer stands apart visually.
func appendTrailerToContent(existing, overridePath string) string {
	trimmed := strings.TrimRight(existing, "\n")
	if trimmed == "" {
		return buildTrailer(overridePath)
	}

	return trimmed + "\n" + buildTrailer(overridePath)
}

func buildTrailer(overridePath string) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(trailerCommentLine)
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s \"%s\"\n", trailerIncludeDirective, overridePath)

	return b.String()
}

// atomicWrite writes to `path` via a temp file + rename so an interrupted run
// never leaves a half-written xcconfig on disk.
func atomicWrite(osProxy osProxyForLink, path, content string, mode fs.FileMode) error {
	tmp := path + ".bitrise-build-cache.tmp"
	if err := osProxy.WriteFile(tmp, []byte(content), mode); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}

	if err := osProxy.Rename(tmp, path); err != nil {
		_ = osProxy.Remove(tmp)

		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}

	return nil
}

// fileMode looks up the current permissions of `path` so atomicWrite preserves
// them. Falls back to 0o644 (Xcode-readable) when the file doesn't exist yet.
func fileMode(osProxy osProxyForLink, path string) (fs.FileMode, error) {
	info, err := osProxy.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0o644, nil
		}

		return 0, fmt.Errorf("stat %s: %w", path, err)
	}

	return info.Mode().Perm(), nil
}

type bridgeRemoveOutcome int

const (
	bridgeUnchanged bridgeRemoveOutcome = iota
	bridgeRemoved
	bridgeMissing
)

// removeBridgeIfPresent tries to delete bridgePath. Distinguishes "gone now"
// from "already gone" from "unspecified failure" so the caller can surface the
// right status to the user.
func removeBridgeIfPresent(osProxy osProxyForLink, bridgePath string) bridgeRemoveOutcome {
	err := osProxy.Remove(bridgePath)
	if err == nil {
		return bridgeRemoved
	}
	if errors.Is(err, fs.ErrNotExist) {
		return bridgeMissing
	}

	return bridgeUnchanged
}

// renderBridge produces the sibling-bridge body used by linkSibling. `#include?`
// keeps the file safe to commit even when the override is missing on teammates'
// machines.
func renderBridge(overrideXCConfigPath string) string {
	var b strings.Builder
	b.WriteString("// Bitrise Build Cache — Xcode.app per-project bridge\n")
	b.WriteString("// Written by `bitrise-build-cache xcode-app link`.\n")
	b.WriteString("// Removed by `bitrise-build-cache xcode-app unlink`.\n")
	b.WriteString("// Do not edit by hand.\n")
	fmt.Fprintf(&b, "#include? \"%s\"\n", overrideXCConfigPath)

	return b.String()
}

func validateOverrideXCConfigPath(p string) error {
	if strings.TrimSpace(p) == "" {
		return errors.New("override xcconfig path is empty")
	}
	if !filepath.IsAbs(p) {
		return fmt.Errorf("override xcconfig path must be absolute (got %s) — Xcode does not expand ~ in xcconfig #include", p)
	}
	if strings.ContainsRune(p, '"') {
		return fmt.Errorf("override xcconfig path contains a quote — cannot safely embed in a bridge #include")
	}

	return nil
}

// saveState writes the per-project link state. When StateDir is empty we skip
// persistence — used by unit tests that don't want to fan out to $HOME.
func saveState(osProxy osProxyForLink, p LinkParams, s linkedProjectState) error {
	if p.StateDir == "" {
		return nil
	}

	if err := osProxy.MkdirAll(p.StateDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", p.StateDir, err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode link state: %w", err)
	}

	statePath := stateFilePath(p.StateDir, s.ProjectPath)
	if err := atomicWrite(osProxy, statePath, string(data)+"\n", 0o600); err != nil {
		return fmt.Errorf("save link state %s: %w", statePath, err)
	}

	return nil
}

func loadState(osProxy osProxyForLink, p LinkParams, projectPath string) (linkedProjectState, bool, error) {
	if p.StateDir == "" {
		return linkedProjectState{}, false, nil
	}

	statePath := stateFilePath(p.StateDir, projectPath)
	raw, found, err := osProxy.ReadFileIfExists(statePath)
	if err != nil {
		return linkedProjectState{}, false, fmt.Errorf("read %s: %w", statePath, err)
	}
	if !found {
		return linkedProjectState{}, false, nil
	}

	var s linkedProjectState
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return linkedProjectState{}, false, fmt.Errorf("decode %s: %w", statePath, err)
	}

	return s, true, nil
}

func removeStateFile(osProxy osProxyForLink, p LinkParams, projectPath string) error {
	if p.StateDir == "" {
		return nil
	}

	statePath := stateFilePath(p.StateDir, projectPath)
	if err := osProxy.Remove(statePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", statePath, err)
	}

	return nil
}

// stateFilePath mirrors Paths.LinkedProjectStateFile — the 8-byte hex keeps
// two projects sharing a basename isolated.
func stateFilePath(stateDir, projectPath string) string {
	return filepath.Join(stateDir, paths.LinkedProjectStateFilename(projectPath))
}

// osProxyForLink is the subset of utils.OsProxy that link/unlink needs. Kept
// minimal so tests can hand in a small fake when they don't want the real fs.
type osProxyForLink interface {
	ReadFileIfExists(name string) (string, bool, error)
	WriteFile(name string, data []byte, mode fs.FileMode) error
	Rename(oldpath, newpath string) error
	Remove(name string) error
	MkdirAll(name string, mode fs.FileMode) error
	Stat(name string) (fs.FileInfo, error)
}
