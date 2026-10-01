package xcode_app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/stringmerge"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const (
	LinkBlockStart = "// [start] bitrise-build-cache xcode-app link"
	LinkBlockEnd   = "// [end] bitrise-build-cache xcode-app link"
)

// SiblingXCConfigName re-exports the on-disk filename from internal/paths so
// this package can keep using the short name without the import at every site.
const SiblingXCConfigName = paths.XcodeAppSiblingXCConfigFileName

// LinkParams targets a single .xcodeproj or .xcworkspace plus the override
// xcconfig to include. OverrideXCConfigPath must be absolute — Xcode does not
// expand `~` in xcconfig include paths.
type LinkParams struct {
	ProjectPath          string
	OverrideXCConfigPath string
}

// LinkResult / UnlinkResult are reported upwards for CLI output. Paths are
// absolute.
type LinkResult struct {
	ModifiedXCConfigs []string
	CreatedSiblings   []string
	SkippedProjects   []string
}

type UnlinkResult struct {
	ModifiedXCConfigs []string
	RemovedSiblings   []string
	WarnBaseRefs      []string
}

// Link wires each XCBuildConfiguration in the referenced project(s) to the
// override xcconfig: appends a `#include?` block to the existing base config,
// or creates a sibling .xcconfig and points baseConfigurationReference at it.
func Link(osProxy utils.OsProxy, p LinkParams) (LinkResult, error) {
	if err := validateOverridePath(p.OverrideXCConfigPath); err != nil {
		return LinkResult{}, err
	}

	projects, err := resolveProjectPaths(osProxy, p.ProjectPath)
	if err != nil {
		return LinkResult{}, err
	}

	var result LinkResult
	for _, proj := range projects {
		if err := linkOneProject(osProxy, proj, p.OverrideXCConfigPath, &result); err != nil {
			return result, err
		}
	}

	return result, nil
}

// Unlink reverts Link's edits: strips the marker block from each xcconfig
// referenced by a build configuration, and removes a sibling file if it has
// no non-marker content.
func Unlink(osProxy utils.OsProxy, p LinkParams) (UnlinkResult, error) {
	projects, err := resolveProjectPaths(osProxy, p.ProjectPath)
	if err != nil {
		return UnlinkResult{}, err
	}

	var result UnlinkResult
	for _, proj := range projects {
		if err := unlinkOneProject(osProxy, proj, &result); err != nil {
			return result, err
		}
	}

	return result, nil
}

// Private ---------------------------------------------------------------

func linkOneProject(osProxy utils.OsProxy, projectPath, overridePath string, result *LinkResult) error {
	pbxPath := filepath.Join(projectPath, "project.pbxproj")

	content, found, err := osProxy.ReadFileIfExists(pbxPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", pbxPath, err)
	}
	if !found {
		return fmt.Errorf("missing %s", pbxPath)
	}

	projDir := filepath.Dir(projectPath)
	fileRefs := parseFileReferences(content)
	configs := parseBuildConfigurations(content)

	ctx := linkContext{
		projDir:         projDir,
		fileRefs:        fileRefs,
		siblingPath:     filepath.Join(projDir, SiblingXCConfigName),
		touchedXCConfig: map[string]struct{}{},
		updatedPbx:      content,
		projectPath:     projectPath,
	}

	for _, cfg := range configs {
		xcconfigPath, err := ctx.resolveXCConfigForConfig(cfg)
		if err != nil {
			return err
		}
		if xcconfigPath == "" {
			continue
		}

		if _, done := ctx.touchedXCConfig[xcconfigPath]; done {
			continue
		}
		ctx.touchedXCConfig[xcconfigPath] = struct{}{}

		changed, err := appendIncludeMarker(osProxy, xcconfigPath, overridePath)
		if err != nil {
			return err
		}
		if !changed {
			continue
		}
		if xcconfigPath == ctx.siblingPath {
			result.CreatedSiblings = appendUnique(result.CreatedSiblings, xcconfigPath)
		} else {
			result.ModifiedXCConfigs = appendUnique(result.ModifiedXCConfigs, xcconfigPath)
		}
	}

	if ctx.updatedPbx != content {
		if err := osProxy.WriteFile(pbxPath, []byte(ctx.updatedPbx), 0o644); err != nil { //nolint:gosec // Xcode must read the pbxproj
			return fmt.Errorf("write %s: %w", pbxPath, err)
		}
	}

	return nil
}

// linkContext carries per-project state so resolveXCConfigForConfig stays flat.
type linkContext struct {
	projDir         string
	fileRefs        map[string]pbxFileRef
	siblingPath     string
	siblingFileRef  string
	touchedXCConfig map[string]struct{}
	updatedPbx      string
	projectPath     string
}

func (c *linkContext) resolveXCConfigForConfig(cfg pbxBuildConfig) (string, error) {
	if cfg.BaseConfigRefID != "" {
		ref, ok := c.fileRefs[cfg.BaseConfigRefID]
		if !ok {
			return "", nil
		}

		return filepath.Join(c.projDir, ref.Path), nil
	}

	if c.siblingFileRef == "" {
		id, err := c.mintSiblingFileRefID()
		if err != nil {
			return "", err
		}
		c.siblingFileRef = id
		updated, err := insertSiblingFileReference(c.updatedPbx, c.siblingFileRef, SiblingXCConfigName)
		if err != nil {
			return "", err
		}
		c.updatedPbx = updated
		c.fileRefs[id] = pbxFileRef{ID: id, Path: SiblingXCConfigName}
	}

	updated, err := attachBaseConfigReference(c.updatedPbx, cfg.ID, c.siblingFileRef, SiblingXCConfigName)
	if err != nil {
		return "", err
	}
	c.updatedPbx = updated

	return c.siblingPath, nil
}

// mintSiblingFileRefID derives a sha256-based 24-char hex id from the project
// path and bumps a counter suffix on the rare chance it collides with an
// existing PBXFileReference id.
func (c *linkContext) mintSiblingFileRefID() (string, error) {
	base := stableFileRefID(c.projectPath)
	if _, clash := c.fileRefs[base]; !clash {
		return base, nil
	}

	const maxAttempts = 16
	for i := 1; i <= maxAttempts; i++ {
		id := stableFileRefID(fmt.Sprintf("%s#%d", c.projectPath, i))
		if _, clash := c.fileRefs[id]; !clash {
			return id, nil
		}
	}

	return "", fmt.Errorf("could not mint a unique PBXFileReference id for %s after %d attempts", c.projectPath, maxAttempts)
}

func unlinkOneProject(osProxy utils.OsProxy, projectPath string, result *UnlinkResult) error {
	pbxPath := filepath.Join(projectPath, "project.pbxproj")

	content, found, err := osProxy.ReadFileIfExists(pbxPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", pbxPath, err)
	}
	if !found {
		return nil
	}

	projDir := filepath.Dir(projectPath)
	fileRefs := parseFileReferences(content)
	configs := parseBuildConfigurations(content)
	siblingPath := filepath.Join(projDir, SiblingXCConfigName)

	visited := map[string]struct{}{}
	warnedBase := map[string]struct{}{}
	for _, cfg := range configs {
		if cfg.BaseConfigRefID == "" {
			continue
		}
		ref, ok := fileRefs[cfg.BaseConfigRefID]
		if !ok {
			continue
		}
		xcconfigPath := filepath.Join(projDir, ref.Path)
		if _, done := visited[xcconfigPath]; done {
			continue
		}
		visited[xcconfigPath] = struct{}{}

		stripped, changed, err := stripIncludeMarker(osProxy, xcconfigPath)
		if err != nil {
			return err
		}
		if changed {
			result.ModifiedXCConfigs = appendUnique(result.ModifiedXCConfigs, xcconfigPath)
		}

		if xcconfigPath == siblingPath && (stripped == "" || !hasNonMarkerContent(stripped)) {
			if err := osProxy.Remove(xcconfigPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("remove %s: %w", xcconfigPath, err)
			}
			result.RemovedSiblings = appendUnique(result.RemovedSiblings, xcconfigPath)

			continue
		}

		if _, warned := warnedBase[xcconfigPath]; !warned {
			warnedBase[xcconfigPath] = struct{}{}
			result.WarnBaseRefs = appendUnique(result.WarnBaseRefs, xcconfigPath)
		}
	}

	return nil
}

// appendIncludeMarker reads the xcconfig at path (treats missing as empty),
// drops the marker block carrying the single `#include?` directive, and writes
// it back. Returns whether the on-disk content changed.
func appendIncludeMarker(osProxy utils.OsProxy, path, overridePath string) (bool, error) {
	existing, _, err := osProxy.ReadFileIfExists(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	block := fmt.Sprintf(`#include? "%s"`, overridePath)
	updated := stringmerge.ChangeContentInBlock(existing, LinkBlockStart, LinkBlockEnd, block)
	if updated == existing {
		return false, nil
	}

	if err := osProxy.WriteFile(path, []byte(updated), 0o644); err != nil { //nolint:gosec // Xcode must read xcconfig
		return false, fmt.Errorf("write %s: %w", path, err)
	}

	return true, nil
}

// stripIncludeMarker reads the xcconfig and removes the marker block. Returns
// the resulting content and whether it changed.
func stripIncludeMarker(osProxy utils.OsProxy, path string) (string, bool, error) {
	existing, found, err := osProxy.ReadFileIfExists(path)
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", path, err)
	}
	if !found {
		return "", false, nil
	}

	updated := stringmerge.RemoveBlock(existing, LinkBlockStart, LinkBlockEnd)
	if updated == existing {
		return existing, false, nil
	}

	if err := osProxy.WriteFile(path, []byte(updated), 0o644); err != nil { //nolint:gosec // Xcode must read xcconfig
		return "", false, fmt.Errorf("write %s: %w", path, err)
	}

	return updated, true, nil
}

// hasNonMarkerContent returns true when stripped xcconfig content contains
// anything other than whitespace. Used to decide if we can delete a sibling we
// created.
func hasNonMarkerContent(content string) bool {
	return strings.TrimSpace(content) != ""
}

// pbxBuildConfig captures the fields parseBuildConfigurations needs: the
// XCBuildConfiguration object id, and the baseConfigurationReference it points
// at (empty when absent).
type pbxBuildConfig struct {
	ID              string
	BaseConfigRefID string
}

// pbxFileRef captures the resolved on-disk path for a PBXFileReference that
// Xcode treats as an xcconfig.
type pbxFileRef struct {
	ID   string
	Path string
}

// pbxObjectIDPattern matches both the legacy 12-char and modern 24-char
// pbxproj object ids, upper- or lowercase hex — both shapes Xcode has shipped.
const pbxObjectIDPattern = `(?:[0-9A-Fa-f]{24}|[0-9A-Fa-f]{12})`

// buildConfigBlockRe matches an XCBuildConfiguration object entry.
// The pbxproj text format is stable enough for a regex to be sound here: object
// ids are hex, each entry starts with `<ID> /* ... */ = {` and ends with `};`.
var buildConfigBlockRe = regexp.MustCompile(`(?m)^\s*(` + pbxObjectIDPattern + `)\s*/\*[^*]*\*/\s*=\s*\{\s*\n\s*isa\s*=\s*XCBuildConfiguration;([\s\S]*?)\n\s*\};`)

// baseConfigRefRe pulls the baseConfigurationReference out of the body of an
// XCBuildConfiguration block, if present.
var baseConfigRefRe = regexp.MustCompile(`baseConfigurationReference\s*=\s*(` + pbxObjectIDPattern + `)\s*/\*`)

// fileRefRe matches a PBXFileReference entry and captures the id and path.
// `lastKnownFileType = text.xcconfig` filters to xcconfigs only; we are not
// interested in sources, assets, or storyboards.
var fileRefRe = regexp.MustCompile(`(?m)^\s*(` + pbxObjectIDPattern + `)\s*/\*[^*]*\*/\s*=\s*\{isa\s*=\s*PBXFileReference;[^}]*lastKnownFileType\s*=\s*text\.xcconfig;[^}]*path\s*=\s*"?([^";]+)"?[^}]*\};`)

func parseBuildConfigurations(content string) []pbxBuildConfig {
	matches := buildConfigBlockRe.FindAllStringSubmatch(content, -1)
	out := make([]pbxBuildConfig, 0, len(matches))
	for _, m := range matches {
		cfg := pbxBuildConfig{ID: m[1]}
		if ref := baseConfigRefRe.FindStringSubmatch(m[2]); len(ref) == 2 {
			cfg.BaseConfigRefID = ref[1]
		}
		out = append(out, cfg)
	}

	return out
}

func parseFileReferences(content string) map[string]pbxFileRef {
	matches := fileRefRe.FindAllStringSubmatch(content, -1)
	out := make(map[string]pbxFileRef, len(matches))
	for _, m := range matches {
		out[m[1]] = pbxFileRef{ID: m[1], Path: m[2]}
	}

	return out
}

// insertSiblingFileReference adds a PBXFileReference entry for the sibling
// xcconfig and places it in the PBXFileReference section. If the section is
// missing (should not happen for well-formed pbxproj) returns an error.
func insertSiblingFileReference(content, refID, siblingName string) (string, error) {
	entry := fmt.Sprintf("\t\t%s /* %s */ = {isa = PBXFileReference; lastKnownFileType = text.xcconfig; path = \"%s\"; sourceTree = \"<group>\"; };\n",
		refID, siblingName, siblingName)

	marker := "/* Begin PBXFileReference section */\n"
	idx := strings.Index(content, marker)
	if idx < 0 {
		return "", errors.New("pbxproj has no PBXFileReference section")
	}

	insertAt := idx + len(marker)

	return content[:insertAt] + entry + content[insertAt:], nil
}

// attachBaseConfigReference inserts baseConfigurationReference = <refID> into
// the XCBuildConfiguration body identified by cfgID. Idempotent — if the
// configuration already has a baseConfigurationReference pointing at refID the
// content is returned unchanged.
func attachBaseConfigReference(content, cfgID, refID, siblingName string) (string, error) {
	blockRe := regexp.MustCompile(`(?m)^(\s*)` + regexp.QuoteMeta(cfgID) + `(\s*/\*[^*]*\*/\s*=\s*\{\s*\n\s*isa\s*=\s*XCBuildConfiguration;\s*\n)`)
	loc := blockRe.FindStringSubmatchIndex(content)
	if loc == nil {
		return "", fmt.Errorf("could not locate XCBuildConfiguration %s", cfgID)
	}

	indent := content[loc[2]:loc[3]] + "\t"
	injection := fmt.Sprintf("%sbaseConfigurationReference = %s /* %s */;\n", indent, refID, siblingName)

	return content[:loc[1]] + injection + content[loc[1]:], nil
}

// stableFileRefID derives a deterministic uppercase-hex 24-char id by taking
// the first 24 chars of sha256(seed). Deterministic across runs so repeated
// Link invocations reuse the same PBXFileReference rather than stacking
// duplicates; `mintSiblingFileRefID` suffixes the seed on the rare chance of a
// collision with an existing id.
func stableFileRefID(seed string) string {
	sum := sha256.Sum256([]byte("bitrise-build-cache-link:" + seed))

	return strings.ToUpper(hex.EncodeToString(sum[:]))[:24]
}

// validateOverridePath mirrors the removed implementation's checks: xcconfig
// `#include?` requires an absolute path (Xcode does not expand `~`) and has
// no quote-escape, so reject either.
func validateOverridePath(p string) error {
	if strings.TrimSpace(p) == "" {
		return errors.New("override xcconfig path is empty")
	}
	if !filepath.IsAbs(p) {
		return fmt.Errorf("override xcconfig path must be absolute (got %s)", p)
	}
	if strings.ContainsRune(p, '"') {
		return fmt.Errorf("override xcconfig path contains a quote character: %s", p)
	}

	return nil
}

func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}

	return append(slice, s)
}

// Workspace resolution --------------------------------------------------

// resolveProjectPaths returns the absolute .xcodeproj paths from the input.
// For a .xcworkspace, walks contents.xcworkspacedata and returns each
// referenced .xcodeproj; FileRefs pointing at Package.swift are skipped (SPM
// packages are out of scope). Nested project-of-project refs are not followed.
func resolveProjectPaths(osProxy utils.OsProxy, projectPath string) ([]string, error) {
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
		return resolveWorkspace(osProxy, abs)
	default:
		return nil, fmt.Errorf("%s: unsupported extension — expected .xcodeproj or .xcworkspace", abs)
	}
}

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

func resolveWorkspace(osProxy utils.OsProxy, workspacePath string) ([]string, error) {
	contentsPath := filepath.Join(workspacePath, "contents.xcworkspacedata")

	raw, found, err := osProxy.ReadFileIfExists(contentsPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", contentsPath, err)
	}
	if !found {
		return nil, fmt.Errorf("missing %s", contentsPath)
	}

	var ws workspaceContents
	if err := xml.Unmarshal([]byte(raw), &ws); err != nil {
		return nil, fmt.Errorf("parse %s: %w", contentsPath, err)
	}

	refs := collectWorkspaceRefs(ws.FileRefs, ws.Groups)

	seen := map[string]struct{}{}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		resolved, ok := resolveWorkspaceRef(workspacePath, ref)
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
		out = append(out, resolved)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no .xcodeproj FileRefs found in workspace", workspacePath)
	}

	return out, nil
}

func collectWorkspaceRefs(refs []workspaceFileRef, groups []workspaceGroup) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Location)
	}
	for _, g := range groups {
		out = append(out, collectWorkspaceRefs(g.FileRefs, g.Groups)...)
	}

	return out
}

func resolveWorkspaceRef(workspacePath, location string) (string, bool) {
	idx := strings.Index(location, ":")
	if idx < 0 {
		return "", false
	}
	scheme, rest := location[:idx], location[idx+1:]
	switch scheme {
	case "self":
		return workspacePath, true
	case "group", "container":
		return filepath.Join(filepath.Dir(workspacePath), rest), true
	case "absolute":
		return rest, true
	default:
		return "", false
	}
}
