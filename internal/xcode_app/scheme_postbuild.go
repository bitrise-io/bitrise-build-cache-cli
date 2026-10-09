package xcode_app

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// FlushPostActionIdentifier marks the scheme post-build action this CLI injects.
const FlushPostActionIdentifier = "io.bitrise.cas.flush"

const (
	flushPostActionTitle   = "Bitrise Build Cache: flush session"
	flushPostActionCommand = "xcelerate flush-session"
)

// DiscoverSchemes returns every .xcscheme file reachable from an .xcodeproj,
// covering both shared and per-user scheme locations.
func DiscoverSchemes(osProxy utils.OsProxy, projectPath string) ([]string, error) {
	var out []string

	shared := filepath.Join(projectPath, "xcshareddata", "xcschemes")
	schemes, err := listSchemesInDir(osProxy, shared)
	if err != nil {
		return nil, err
	}
	out = append(out, schemes...)

	userRoot := filepath.Join(projectPath, "xcuserdata")
	users, err := osProxy.ReadDir(userRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return out, nil
		}

		return nil, fmt.Errorf("read %s: %w", userRoot, err)
	}

	for _, u := range users {
		if !u.IsDir() {
			continue
		}
		dir := filepath.Join(userRoot, u.Name(), "xcschemes")
		schemes, err := listSchemesInDir(osProxy, dir)
		if err != nil {
			return nil, err
		}
		out = append(out, schemes...)
	}

	return out, nil
}

func listSchemesInDir(osProxy utils.OsProxy, dir string) ([]string, error) {
	entries, err := osProxy.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(e.Name()), ".xcscheme") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}

	return out, nil
}

// InjectFlushSessionPostAction adds a BuildAction/PostActions ExecutionAction
// to the given scheme, invoking the CLI's flush-session command. No-op if the
// action is already present (matched by its ActionID).
func InjectFlushSessionPostAction(osProxy utils.OsProxy, schemePath, cliBinaryPath string) (bool, error) {
	content, found, err := osProxy.ReadFileIfExists(schemePath)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", schemePath, err)
	}
	if !found {
		return false, nil
	}

	if hasFlushPostAction(content) {
		return false, nil
	}

	updated, err := injectIntoScheme(content, cliBinaryPath)
	if err != nil {
		return false, fmt.Errorf("inject into %s: %w", schemePath, err)
	}
	if updated == content {
		return false, nil
	}

	if err := atomicWrite(osProxy, schemePath, updated); err != nil {
		return false, err
	}

	return true, nil
}

// RemoveFlushSessionPostAction strips the previously-injected ExecutionAction.
// If removing the action leaves PostActions empty, the whole PostActions block
// is pruned so we don't leave a hollow element behind.
func RemoveFlushSessionPostAction(osProxy utils.OsProxy, schemePath string) (bool, error) {
	content, found, err := osProxy.ReadFileIfExists(schemePath)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", schemePath, err)
	}
	if !found {
		return false, nil
	}

	updated, changed := removeFromScheme(content)
	if !changed {
		return false, nil
	}

	if err := atomicWrite(osProxy, schemePath, updated); err != nil {
		return false, err
	}

	return true, nil
}

func atomicWrite(osProxy utils.OsProxy, path, content string) error {
	tmp := path + ".tmp"
	if err := osProxy.WriteFile(tmp, []byte(content), 0o644); err != nil { //nolint:gosec // Xcode must read scheme
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := osProxy.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}

	return nil
}

func hasFlushPostAction(content string) bool {
	return strings.Contains(content, `ActionID = "`+FlushPostActionIdentifier+`"`)
}

var buildActionOpenRe = regexp.MustCompile(`(?s)<BuildAction\b[^>]*>`)

func injectIntoScheme(content, cliBinaryPath string) (string, error) {
	openLoc := buildActionOpenRe.FindStringIndex(content)
	if openLoc == nil {
		return content, errors.New("scheme has no <BuildAction> element")
	}

	closeIdx := strings.Index(content[openLoc[1]:], "</BuildAction>")
	if closeIdx < 0 {
		return content, errors.New("scheme has no </BuildAction> close tag")
	}
	closeAbs := openLoc[1] + closeIdx

	inner := content[openLoc[1]:closeAbs]

	postOpenRe := regexp.MustCompile(`<PostActions>`)
	postCloseRe := regexp.MustCompile(`</PostActions>`)

	indent := leadingLineIndent(content, closeAbs)
	childIndent := indent + "   "
	action := renderExecutionAction(childIndent, cliBinaryPath)

	if postLoc := postCloseRe.FindStringIndex(inner); postLoc != nil {
		absClose := openLoc[1] + postLoc[0]
		insertion := action + "\n" + leadingLineIndent(content, absClose)

		return content[:absClose] + insertion + content[absClose:], nil
	}

	if postOpenRe.MatchString(inner) {
		return content, errors.New("scheme has <PostActions> without a close tag")
	}

	block := indent + "<PostActions>\n" + action + "\n" + indent + "</PostActions>\n" + leadingLineIndent(content, closeAbs)

	return content[:closeAbs] + block + content[closeAbs:], nil
}

var executionActionRe = regexp.MustCompile(`(?s)[ \t]*<ExecutionAction[^>]*>.*?</ExecutionAction>\n?`)

func removeFromScheme(content string) (string, bool) {
	locs := executionActionRe.FindAllStringIndex(content, -1)
	if len(locs) == 0 {
		return content, false
	}

	var out strings.Builder
	out.Grow(len(content))
	prev := 0
	removed := false
	for _, loc := range locs {
		block := content[loc[0]:loc[1]]
		if !strings.Contains(block, `ActionID = "`+FlushPostActionIdentifier+`"`) {
			continue
		}
		out.WriteString(content[prev:loc[0]])
		prev = loc[1]
		removed = true
	}
	if !removed {
		return content, false
	}
	out.WriteString(content[prev:])

	return pruneEmptyPostActions(out.String()), true
}

var emptyPostActionsRe = regexp.MustCompile(`(?s)[ \t]*<PostActions>[\s]*</PostActions>\n?`)

func pruneEmptyPostActions(content string) string {
	return emptyPostActionsRe.ReplaceAllString(content, "")
}

// leadingLineIndent returns the whitespace prefix of the line containing idx —
// used so inserted elements line up with the surrounding XML indentation.
func leadingLineIndent(content string, idx int) string {
	start := strings.LastIndexByte(content[:idx], '\n') + 1
	indent := content[start:idx]
	for i, r := range indent {
		if r != ' ' && r != '\t' {
			return indent[:i]
		}
	}

	return indent
}

func renderExecutionAction(indent, cliBinaryPath string) string {
	script := fmt.Sprintf("%s %s", shellEscape(cliBinaryPath), flushPostActionCommand)
	escaped := xmlAttrEscape(script)

	return indent + `<ExecutionAction` + "\n" +
		indent + `   ActionType = "Xcode.IDEStandardExecutionActionsCore.ExecutionActionType.ShellScriptAction"` + "\n" +
		indent + `   ActionID = "` + FlushPostActionIdentifier + `">` + "\n" +
		indent + `   <ActionContent` + "\n" +
		indent + `      title = "` + flushPostActionTitle + `"` + "\n" +
		indent + `      scriptText = "` + escaped + `">` + "\n" +
		indent + `   </ActionContent>` + "\n" +
		indent + `</ExecutionAction>`
}

func shellEscape(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\"'$`\\") {
		return s
	}

	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func xmlAttrEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	)

	return replacer.Replace(s)
}
