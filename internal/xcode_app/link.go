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
	LinkBlockStart = "// [start] bitrise-build-cache xcode link"
	LinkBlockEnd   = "// [end] bitrise-build-cache xcode link"
)

// LinkParams targets a single .xcodeproj or .xcworkspace. OverrideXCConfigPath
// must be absolute — Xcode does not expand `~` in xcconfig include paths.
type LinkParams struct {
	ProjectPath          string
	OverrideXCConfigPath string
}

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
	groups := parseGroups(content)
	parentOf := buildGroupParentMap(groups)

	ctx := linkContext{
		projDir:         projDir,
		fileRefs:        fileRefs,
		groups:          groups,
		parentOf:        parentOf,
		siblingPath:     filepath.Join(projDir, paths.XcodeAppSiblingXCConfigFileName),
		siblingFileRef:  findExistingSiblingFileRef(fileRefs),
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

type linkContext struct {
	projDir         string
	fileRefs        map[string]pbxFileRef
	groups          map[string]pbxGroup
	parentOf        map[string]string
	siblingPath     string
	siblingFileRef  string
	touchedXCConfig map[string]struct{}
	updatedPbx      string
	projectPath     string
}

func (c *linkContext) resolveXCConfigForConfig(cfg pbxBuildConfig) (string, error) {
	if cfg.BaseConfigRefID != "" {
		if ref, ok := c.fileRefs[cfg.BaseConfigRefID]; ok {
			return resolveFileRefPath(c.projDir, ref, c.groups, c.parentOf), nil
		}

		// Dangling pointer: a previous activate (or a hand-edit) left a
		// baseConfigurationReference whose target PBXFileReference is gone.
		// Strip the stale line so the fall-through re-attaches a fresh ref
		// to the live sibling instead of stacking a duplicate alongside it.
		c.updatedPbx = detachBaseConfigReference(c.updatedPbx, cfg.ID)
	}

	if c.siblingFileRef == "" {
		id, err := c.mintSiblingFileRefID()
		if err != nil {
			return "", err
		}
		c.siblingFileRef = id
		updated, err := insertSiblingFileReference(c.updatedPbx, c.siblingFileRef, paths.XcodeAppSiblingXCConfigFileName)
		if err != nil {
			return "", err
		}
		c.updatedPbx = updated
		c.fileRefs[id] = pbxFileRef{ID: id, Path: paths.XcodeAppSiblingXCConfigFileName}
	}

	updated, err := attachBaseConfigReference(c.updatedPbx, cfg.ID, c.siblingFileRef, paths.XcodeAppSiblingXCConfigFileName)
	if err != nil {
		return "", err
	}
	c.updatedPbx = updated

	return c.siblingPath, nil
}

func findExistingSiblingFileRef(fileRefs map[string]pbxFileRef) string {
	for id, ref := range fileRefs {
		if ref.Path == paths.XcodeAppSiblingXCConfigFileName {
			return id
		}
	}

	return ""
}

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
	groups := parseGroups(content)
	ctx := unlinkContext{
		projDir:       projDir,
		fileRefs:      parseFileReferences(content),
		groups:        groups,
		parentOf:      buildGroupParentMap(groups),
		siblingPath:   filepath.Join(projDir, paths.XcodeAppSiblingXCConfigFileName),
		updatedPbx:    content,
		siblingRefIDs: map[string]struct{}{},
		visited:       map[string]struct{}{},
		warnedBase:    map[string]struct{}{},
	}

	for _, cfg := range parseBuildConfigurations(content) {
		if err := ctx.processConfig(osProxy, cfg, result); err != nil {
			return err
		}
	}

	for refID := range ctx.siblingRefIDs {
		ctx.updatedPbx = removeFileReferenceEntry(ctx.updatedPbx, refID)
	}

	if ctx.updatedPbx != content {
		if err := osProxy.WriteFile(pbxPath, []byte(ctx.updatedPbx), 0o644); err != nil { //nolint:gosec // Xcode must read the pbxproj
			return fmt.Errorf("write %s: %w", pbxPath, err)
		}
	}

	return nil
}

type unlinkContext struct {
	projDir       string
	fileRefs      map[string]pbxFileRef
	groups        map[string]pbxGroup
	parentOf      map[string]string
	siblingPath   string
	updatedPbx    string
	siblingRefIDs map[string]struct{}
	visited       map[string]struct{}
	warnedBase    map[string]struct{}
}

func (c *unlinkContext) processConfig(osProxy utils.OsProxy, cfg pbxBuildConfig, result *UnlinkResult) error {
	if cfg.BaseConfigRefID == "" {
		return nil
	}
	ref, ok := c.fileRefs[cfg.BaseConfigRefID]
	if !ok {
		return nil
	}
	xcconfigPath := resolveFileRefPath(c.projDir, ref, c.groups, c.parentOf)
	isSibling := xcconfigPath == c.siblingPath

	if isSibling {
		// Link() minted the baseConfigurationReference along with the sibling;
		// the file-ref dies with the sibling, so the pbxproj line goes too.
		c.updatedPbx = detachBaseConfigReference(c.updatedPbx, cfg.ID)
		c.siblingRefIDs[cfg.BaseConfigRefID] = struct{}{}
	}

	if _, done := c.visited[xcconfigPath]; done {
		return nil
	}
	c.visited[xcconfigPath] = struct{}{}

	stripped, changed, err := stripIncludeMarker(osProxy, xcconfigPath)
	if err != nil {
		return err
	}
	if changed {
		result.ModifiedXCConfigs = appendUnique(result.ModifiedXCConfigs, xcconfigPath)
	}

	if isSibling && (stripped == "" || !hasNonMarkerContent(stripped)) {
		if err := osProxy.Remove(xcconfigPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", xcconfigPath, err)
		}
		result.RemovedSiblings = appendUnique(result.RemovedSiblings, xcconfigPath)

		return nil
	}

	if !isSibling {
		if _, warned := c.warnedBase[xcconfigPath]; !warned {
			c.warnedBase[xcconfigPath] = struct{}{}
			result.WarnBaseRefs = appendUnique(result.WarnBaseRefs, xcconfigPath)
		}
	}

	return nil
}

// detachBaseConfigReference removes the `baseConfigurationReference = ...;` line
// from the XCBuildConfiguration block identified by cfgID. No-op if the line is
// absent (user already removed it) or the block cannot be located.
func detachBaseConfigReference(content, cfgID string) string {
	blockRe := regexp.MustCompile(`(?m)^(\s*)` + regexp.QuoteMeta(cfgID) + `\s*/\*[^*]*\*/\s*=\s*\{\s*\n\s*isa\s*=\s*XCBuildConfiguration;([\s\S]*?)\n\s*\};`)
	loc := blockRe.FindStringSubmatchIndex(content)
	if loc == nil {
		return content
	}

	bodyStart, bodyEnd := loc[4], loc[5]
	body := content[bodyStart:bodyEnd]
	stripped := baseConfigLineRe.ReplaceAllString(body, "")
	if stripped == body {
		return content
	}

	return content[:bodyStart] + stripped + content[bodyEnd:]
}

// Horizontal-only leading whitespace: `\s` includes `\n`, which would eat the
// terminating newline of the preceding `isa = XCBuildConfiguration;` line and
// collapse the next line onto it.
var baseConfigLineRe = regexp.MustCompile(`(?m)^[ \t]*baseConfigurationReference\s*=\s*` + pbxObjectIDPattern + `\s*/\*[^*]*\*/\s*;[ \t]*\n`)

// removeFileReferenceEntry drops the single-line PBXFileReference entry whose
// object id is refID. The regex matches both the sibling-insertion shape and
// anything structurally similar; absent id → no-op.
func removeFileReferenceEntry(content, refID string) string {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(refID) + `\s*/\*[^*]*\*/\s*=\s*\{\s*isa\s*=\s*PBXFileReference;[^}]*\};\s*\n`)

	return re.ReplaceAllString(content, "")
}

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

func hasNonMarkerContent(content string) bool {
	return strings.TrimSpace(content) != ""
}

type pbxBuildConfig struct {
	ID              string
	BaseConfigRefID string
}

type pbxFileRef struct {
	ID         string
	Path       string
	SourceTree string
}

type pbxGroup struct {
	ID       string
	Path     string
	Children []string
}

// pbxObjectIDPattern matches both legacy 12-char and modern 24-char pbxproj
// object ids, in any hex case.
const pbxObjectIDPattern = `(?:[0-9A-Fa-f]{24}|[0-9A-Fa-f]{12})`

var buildConfigBlockRe = regexp.MustCompile(`(?m)^\s*(` + pbxObjectIDPattern + `)\s*/\*[^*]*\*/\s*=\s*\{\s*\n\s*isa\s*=\s*XCBuildConfiguration;([\s\S]*?)\n\s*\};`)

var baseConfigRefRe = regexp.MustCompile(`baseConfigurationReference\s*=\s*(` + pbxObjectIDPattern + `)\s*/\*`)

var fileRefRe = regexp.MustCompile(`(?m)^\s*(` + pbxObjectIDPattern + `)\s*/\*[^*]*\*/\s*=\s*\{isa\s*=\s*PBXFileReference;[^}]*lastKnownFileType\s*=\s*text\.xcconfig;[^}]*\};`)

var (
	pbxPathAttrRe       = regexp.MustCompile(`path\s*=\s*(?:"([^"]+)"|([^;\s]+))\s*;`)
	pbxSourceTreeAttrRe = regexp.MustCompile(`sourceTree\s*=\s*(?:"([^"]*)"|([^;\s]+))\s*;`)
)

var groupBlockRe = regexp.MustCompile(`(?m)^\s*(` + pbxObjectIDPattern + `)\s*(?:/\*[^*]*\*/\s*)?=\s*\{\s*\n\s*isa\s*=\s*PBXGroup;([\s\S]*?)\n\s*\};`)

var groupChildrenRe = regexp.MustCompile(`children\s*=\s*\(([\s\S]*?)\)\s*;`)

var groupChildIDRe = regexp.MustCompile(`(` + pbxObjectIDPattern + `)`)

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
		id := m[1]
		body := m[0]
		ref := pbxFileRef{ID: id}
		if p := pbxPathAttrRe.FindStringSubmatch(body); len(p) == 3 {
			if p[1] != "" {
				ref.Path = p[1]
			} else {
				ref.Path = p[2]
			}
		}
		if st := pbxSourceTreeAttrRe.FindStringSubmatch(body); len(st) == 3 {
			if st[1] != "" {
				ref.SourceTree = st[1]
			} else {
				ref.SourceTree = st[2]
			}
		}
		out[id] = ref
	}

	return out
}

// parseGroups extracts every PBXGroup; an empty `Path` means the group is
// pathless and contributes nothing to its descendants' resolved path.
func parseGroups(content string) map[string]pbxGroup {
	matches := groupBlockRe.FindAllStringSubmatch(content, -1)
	out := make(map[string]pbxGroup, len(matches))
	for _, m := range matches {
		id := m[1]
		body := m[2]
		g := pbxGroup{ID: id}
		if p := pbxPathAttrRe.FindStringSubmatch(body); len(p) == 3 {
			if p[1] != "" {
				g.Path = p[1]
			} else {
				g.Path = p[2]
			}
		}
		if ch := groupChildrenRe.FindStringSubmatch(body); len(ch) == 2 {
			for _, idm := range groupChildIDRe.FindAllStringSubmatch(ch[1], -1) {
				g.Children = append(g.Children, idm[1])
			}
		}
		out[id] = g
	}

	return out
}

func buildGroupParentMap(groups map[string]pbxGroup) map[string]string {
	out := make(map[string]string, len(groups))
	for parentID, g := range groups {
		for _, childID := range g.Children {
			out[childID] = parentID
		}
	}

	return out
}

// resolveFileRefPath returns the absolute on-disk path of a PBXFileReference,
// honouring its sourceTree and (for `<group>`) the ancestral PBXGroup path chain.
func resolveFileRefPath(projDir string, ref pbxFileRef, groups map[string]pbxGroup, parentOf map[string]string) string {
	switch ref.SourceTree {
	case "<absolute>":
		return ref.Path
	case "SOURCE_ROOT", "":
		return filepath.Join(projDir, ref.Path)
	case "<group>":
		segments := []string{ref.Path}
		visited := map[string]struct{}{ref.ID: {}}
		for id := parentOf[ref.ID]; id != ""; id = parentOf[id] {
			if _, loop := visited[id]; loop {
				break
			}
			visited[id] = struct{}{}
			g, ok := groups[id]
			if !ok {
				break
			}
			if g.Path != "" {
				segments = append([]string{g.Path}, segments...)
			}
		}

		return filepath.Join(append([]string{projDir}, segments...)...)
	default:
		return filepath.Join(projDir, ref.Path)
	}
}

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

// stableFileRefID keeps the generated PBXFileReference id deterministic across
// runs so repeated Link invocations reuse it rather than stacking duplicates.
func stableFileRefID(seed string) string {
	sum := sha256.Sum256([]byte("bitrise-build-cache-link:" + seed))

	return strings.ToUpper(hex.EncodeToString(sum[:]))[:24]
}

// validateOverridePath rejects values the xcconfig `#include?` directive cannot
// handle: `~` is not expanded, and there is no quote-escape.
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

// resolveProjectPaths returns absolute .xcodeproj paths. For a .xcworkspace,
// walks contents.xcworkspacedata; Package.swift refs are out of scope and
// nested project-of-project refs are not followed.
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

	refs := collectWorkspaceRefs(ws.FileRefs, ws.Groups, "")

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

func collectWorkspaceRefs(refs []workspaceFileRef, groups []workspaceGroup, parentPrefix string) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, composeWorkspaceLocation(parentPrefix, r.Location))
	}
	for _, g := range groups {
		childPrefix := composeWorkspaceLocation(parentPrefix, g.Location)
		out = append(out, collectWorkspaceRefs(g.FileRefs, g.Groups, childPrefix)...)
	}

	return out
}

const (
	workspaceSchemeAbsolute  = "absolute"
	workspaceSchemeGroup     = "group"
	workspaceSchemeContainer = "container"
	workspaceSchemeSelf      = "self"
)

// composeWorkspaceLocation joins a parent group's location with a child
// FileRef's. Child `absolute:` wins; parent `absolute:` reroots a relative
// child; any other relative parent carries the child's scheme forward.
func composeWorkspaceLocation(parent, child string) string {
	childScheme, childRest := splitWorkspaceLocation(child)
	if childScheme == workspaceSchemeAbsolute || parent == "" {
		return child
	}
	parentScheme, parentRest := splitWorkspaceLocation(parent)
	if parentScheme == "" {
		return child
	}
	joined := filepath.Join(parentRest, childRest)
	if parentScheme == workspaceSchemeAbsolute {
		return workspaceSchemeAbsolute + ":" + joined
	}

	return childScheme + ":" + joined
}

func splitWorkspaceLocation(location string) (string, string) {
	idx := strings.Index(location, ":")
	if idx < 0 {
		return "", location
	}

	return location[:idx], location[idx+1:]
}

func resolveWorkspaceRef(workspacePath, location string) (string, bool) {
	scheme, rest := splitWorkspaceLocation(location)
	if scheme == "" {
		return "", false
	}
	switch scheme {
	case workspaceSchemeSelf:
		return workspacePath, true
	case workspaceSchemeGroup, workspaceSchemeContainer:
		return filepath.Join(filepath.Dir(workspacePath), rest), true
	case workspaceSchemeAbsolute:
		return rest, true
	default:
		return "", false
	}
}
