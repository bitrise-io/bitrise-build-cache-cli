package xcode_app

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// BridgeXCConfigName is the sibling xcconfig written next to a .xcodeproj by
// `xcode-app link`. Kept as a re-export of paths.XcodeAppBridgeXCConfigName so
// callers of the link package don't have to import both.
//
// Rationale: macOS 26 stopped propagating `launchctl setenv XCODE_XCCONFIG_FILE`
// to GUI-launched Xcode.app, so the user-global env override alone is not
// enough for GUI builds. Per-project injection via a base-configuration
// xcconfig is Apple-documented and stable. We write the bridge; the user
// selects it as the base configuration once, and future GUI builds pick up
// the include chain.
const BridgeXCConfigName = paths.XcodeAppBridgeXCConfigName

// schemeGroup is the workspace FileRef / Group `location` scheme identifying
// entries whose path is resolved relative to the containing workspace or group.
const schemeGroup = "group"

// LinkParams targets a single .xcodeproj or .xcworkspace.
type LinkParams struct {
	// ProjectPath is either <name>.xcodeproj or <name>.xcworkspace (absolute or relative).
	ProjectPath string

	// OverrideXCConfigPath is the absolute path of the user-global override
	// xcconfig (e.g. ~/.bitrise-xcelerate/xcode-app.xcconfig). The bridge file
	// `#include`s this path. Must be absolute — Xcode's xcconfig loader does
	// NOT expand `~` in include paths.
	OverrideXCConfigPath string
}

type LinkResult struct {
	// BridgeFiles are the absolute paths of every bridge xcconfig we wrote.
	// One per referenced .xcodeproj (workspace fans out to multiple).
	BridgeFiles []string

	// AlreadyLinked lists bridge files whose content already matched what we
	// would have written — the link operation was a no-op for them.
	AlreadyLinked []string
}

type UnlinkResult struct {
	// RemovedBridgeFiles are absolute paths we deleted.
	RemovedBridgeFiles []string

	// MissingBridgeFiles are absolute paths that were already absent — nothing
	// to remove. Not an error; report so the CLI can inform the user.
	MissingBridgeFiles []string
}

// Link writes a bridge xcconfig next to each referenced .xcodeproj so the user
// can point Xcode's base-configuration at it. Idempotent: rewriting a bridge
// with identical content reports it under AlreadyLinked and skips the write.
func Link(osProxy osProxyForLink, p LinkParams) (LinkResult, error) {
	if err := validateOverrideXCConfigPath(p.OverrideXCConfigPath); err != nil {
		return LinkResult{}, err
	}

	projectDirs, err := ResolveProjectDirs(osProxy, p.ProjectPath)
	if err != nil {
		return LinkResult{}, err
	}

	body := renderBridge(p.OverrideXCConfigPath)

	var result LinkResult
	for _, dir := range projectDirs {
		bridgePath := filepath.Join(dir, BridgeXCConfigName)

		existing, found, err := osProxy.ReadFileIfExists(bridgePath)
		if err != nil {
			return result, fmt.Errorf("read %s: %w", bridgePath, err)
		}

		if found && existing == body {
			result.AlreadyLinked = append(result.AlreadyLinked, bridgePath)

			continue
		}

		if err := osProxy.WriteFile(bridgePath, []byte(body), 0o644); err != nil { //nolint:gosec // xcconfig is read by Xcode
			return result, fmt.Errorf("write %s: %w", bridgePath, err)
		}

		result.BridgeFiles = append(result.BridgeFiles, bridgePath)
	}

	return result, nil
}

// Unlink deletes every bridge xcconfig next to each referenced .xcodeproj.
// A missing bridge is not an error — it's reported under MissingBridgeFiles.
func Unlink(osProxy osProxyForLink, p LinkParams) (UnlinkResult, error) {
	projectDirs, err := ResolveProjectDirs(osProxy, p.ProjectPath)
	if err != nil {
		return UnlinkResult{}, err
	}

	var result UnlinkResult
	for _, dir := range projectDirs {
		bridgePath := filepath.Join(dir, BridgeXCConfigName)

		if err := osProxy.Remove(bridgePath); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				result.MissingBridgeFiles = append(result.MissingBridgeFiles, bridgePath)

				continue
			}

			return result, fmt.Errorf("remove %s: %w", bridgePath, err)
		}

		result.RemovedBridgeFiles = append(result.RemovedBridgeFiles, bridgePath)
	}

	return result, nil
}

// ResolveProjectDirs returns the parent dirs of every .xcodeproj referenced by
// the supplied path. For a .xcodeproj, that's the single containing dir. For a
// .xcworkspace, we parse contents.xcworkspacedata and resolve each FileRef.
func ResolveProjectDirs(osProxy osProxyForLink, projectPath string) ([]string, error) {
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
		return []string{filepath.Dir(abs)}, nil
	case ".xcworkspace":
		return resolveWorkspaceProjectDirs(osProxy, abs)
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

func resolveWorkspaceProjectDirs(osProxy osProxyForLink, workspacePath string) ([]string, error) {
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
	dirs := make([]string, 0, len(refs))
	for _, ref := range refs {
		resolved, ok := resolveWorkspaceFileRef(workspacePath, ref.Location)
		if !ok {
			continue
		}

		// Only wire bridges for .xcodeproj entries. FileRefs can also point at
		// loose folders or non-project resources.
		if !strings.EqualFold(filepath.Ext(resolved), ".xcodeproj") {
			continue
		}

		// Skip silently if the resolved path is missing — hand-edited or
		// tool-generated workspaces can carry FileRefs whose path shape doesn't
		// exactly match what our resolver expects. Not-existing is not an error.
		if _, err := osProxy.Stat(resolved); err != nil {
			continue
		}

		dir := filepath.Dir(resolved)
		if _, dup := seen[dir]; dup {
			continue
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}

	if len(dirs) == 0 {
		return nil, fmt.Errorf("%s: no .xcodeproj FileRefs found in workspace", workspacePath)
	}

	return dirs, nil
}

// collectFileRefs flattens the FileRef entries under Workspace and any nested
// Group nodes. Xcode workspaces in the wild disagree on whether a FileRef's
// `group:<rest>` under a `<Group location="group:<prefix>">` is
// workspace-relative or group-relative — Apple's docs say group-relative, but
// some tool-generated workspaces write the full workspace-relative path
// directly on the FileRef. To handle both, when a prefix is active we emit
// two candidates per FileRef (prefixed + as-is); the stat check downstream
// keeps whichever exists on disk.
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

// joinGroupLocation combines a parent group prefix with a nested group's
// `location` attribute, yielding the prefix descendants should see. Non-group
// group locations (rare in practice) short-circuit — we only chain `group:`.
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

// joinGroupPrefix prepends the accumulated group prefix to a FileRef location.
// Only `group:` locations are relative to the enclosing group; other schemes
// (self, container, absolute) are unaffected.
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

// resolveWorkspaceFileRef expands a contents.xcworkspacedata `<scheme>:<rest>`
// location into an absolute filesystem path. Unknown schemes return ok=false
// and are skipped rather than failing the whole workspace — hand-edited or
// exotic workspaces can carry entries we shouldn't refuse to process.
func resolveWorkspaceFileRef(workspacePath, location string) (string, bool) {
	scheme, rest, ok := splitScheme(location)
	if !ok {
		return "", false
	}

	switch scheme {
	case "self":
		// self: refers to the workspace's container. Return the workspace path;
		// the ext-filter downstream drops it because it doesn't end in .xcodeproj.
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

// renderBridge produces the exact byte content of a bridge xcconfig. `#include?`
// (optional include) lets the bridge stay committed — teammates or CI runs
// without the CLI installed skip the override silently instead of failing the
// build with `Unable to open base configuration reference file`.
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

// osProxyForLink is the subset of utils.OsProxy that link/unlink needs.
type osProxyForLink interface {
	ReadFileIfExists(name string) (string, bool, error)
	WriteFile(name string, data []byte, mode fs.FileMode) error
	Remove(name string) error
	Stat(name string) (fs.FileInfo, error)
}
