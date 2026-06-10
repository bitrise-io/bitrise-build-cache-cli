// Package xcodescheme installs / removes a Bitrise Build Cache pre-build
// scheme action inside an Xcode .xcscheme XML file.
//
// The injected action is a shell-script `<ExecutionAction>` under
// `<BuildAction>/<PreActions>` that runs `bitrise-build-cache doctor` before
// each Xcode build. Operations are idempotent: re-running install on a scheme
// that already has the action is a no-op.
//
// We deliberately use targeted regex / text manipulation instead of
// `encoding/xml`. Apple's xcscheme files use a specific indent style + ordering
// the Go XML encoder doesn't preserve; running everything through a marshal
// round-trip would produce a noisy git diff for customers.
package xcodescheme

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Marker is embedded as a comment line inside the injected script so re-runs
// detect a prior install without parsing the whole XML. Version-suffixed so
// future migrations can find + replace v1 with v2 cleanly.
const Marker = "bitrise-build-cache-prebuild-marker-v1"

// PreActionBlock is what we inject. The leading newline is intentional —
// it sits right after `<BuildAction ...>` and before `<BuildActionEntries>`.
//
// scriptText must escape XML chars. Our content uses only safe chars, so we
// embed it verbatim in CDATA-free form. (Apple's UI writes it the same way.)
const PreActionBlock = `      <PreActions>
         <ExecutionAction
            ActionType = "Xcode.IDEStandardExecutionActionsCore.ExecutionActionType.ShellScriptAction">
            <ActionContent
               title = "Bitrise Build Cache - doctor"
               scriptText = "# ` + Marker + `&#10;if ! command -v bitrise-build-cache &gt;/dev/null 2&gt;&amp;1; then&#10;  echo &quot;warning: bitrise-build-cache not installed; skipping health check&quot;&#10;  exit 0&#10;fi&#10;bitrise-build-cache doctor --no-update-check&#10;">
            </ActionContent>
         </ExecutionAction>
      </PreActions>
`

var (
	// reBuildActionOpen matches the BuildAction opening tag (single-line or
	// multi-line attribute form Apple emits). We append PreActions right after
	// the closing `>` of this tag.
	reBuildActionOpen = regexp.MustCompile(`(?s)(<BuildAction\b[^>]*>)`)

	// rePreActionsBlock matches any existing PreActions block whose body
	// contains our marker. Used for idempotency + uninstall.
	rePreActionsBlock = regexp.MustCompile(`(?s)\s*<PreActions>.*?` + regexp.QuoteMeta(Marker) + `.*?</PreActions>\n?`)
)

// Status describes what install / uninstall did.
type Status int

const (
	// StatusInstalled — the file was modified and our action is now present.
	StatusInstalled Status = iota
	// StatusAlreadyInstalled — file unchanged, our marker was already there.
	StatusAlreadyInstalled
	// StatusUninstalled — file was modified, our action removed.
	StatusUninstalled
	// StatusNotInstalled — no marker found; nothing to uninstall.
	StatusNotInstalled
)

// Install injects the pre-action into the xcscheme at path. Idempotent.
func Install(path string) (Status, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path comes from a CLI flag
	if err != nil {
		return 0, fmt.Errorf("read xcscheme: %w", err)
	}

	if rePreActionsBlock.Match(data) {
		return StatusAlreadyInstalled, nil
	}

	loc := reBuildActionOpen.FindIndex(data)
	if loc == nil {
		return 0, errors.New("xcscheme has no <BuildAction> element — is this a valid .xcscheme file?")
	}

	// Insert right after the closing `>` of <BuildAction ...>.
	insertAt := loc[1]
	if !strings.HasPrefix(string(data[insertAt:]), "\n") {
		// Defensive: Apple always writes a newline after the tag, but pad anyway.
		insertAt += 0
	}

	out := make([]byte, 0, len(data)+len(PreActionBlock)+1)
	out = append(out, data[:insertAt]...)
	out = append(out, '\n')
	out = append(out, []byte(PreActionBlock)...)
	// Reattach the remainder, skipping the leading newline (if any) so we don't
	// add a blank line in Apple's pre-existing layout.
	rest := data[insertAt:]
	if len(rest) > 0 && rest[0] == '\n' {
		rest = rest[1:]
	}
	out = append(out, rest...)

	if err := os.WriteFile(path, out, 0o644); err != nil { //nolint:gosec
		return 0, fmt.Errorf("write xcscheme: %w", err)
	}

	return StatusInstalled, nil
}

// Uninstall removes our injected pre-action. Idempotent.
func Uninstall(path string) (Status, error) {
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return 0, fmt.Errorf("read xcscheme: %w", err)
	}

	if !rePreActionsBlock.Match(data) {
		return StatusNotInstalled, nil
	}

	out := rePreActionsBlock.ReplaceAll(data, nil)

	if err := os.WriteFile(path, out, 0o644); err != nil { //nolint:gosec
		return 0, fmt.Errorf("write xcscheme: %w", err)
	}

	return StatusUninstalled, nil
}

// ResolveSchemePath returns the absolute path to a scheme XML file inside an
// .xcodeproj (or .xcworkspace) bundle. Looks under xcshareddata/xcschemes.
func ResolveSchemePath(projectOrWorkspacePath, scheme string) (string, error) {
	candidates := []string{
		projectOrWorkspacePath + "/xcshareddata/xcschemes/" + scheme + ".xcscheme",
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	return "", fmt.Errorf("scheme %q not found under %s/xcshareddata/xcschemes/", scheme, projectOrWorkspacePath)
}
