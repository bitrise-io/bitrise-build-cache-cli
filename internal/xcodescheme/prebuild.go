// Package xcodescheme installs/removes a pre-build scheme action in an Xcode .xcscheme XML file.
// Targeted regex (not encoding/xml) — a marshal round-trip would change Apple's indent style and noise up the customer's git diff.
package xcodescheme

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Marker is embedded in the injected script so re-runs detect a prior install. Version-suffixed for future v1→v2 migration.
const Marker = "bitrise-build-cache-prebuild-marker-v1"

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
	reBuildActionOpen = regexp.MustCompile(`(?s)(<BuildAction\b[^>]*>)`)
	rePreActionsBlock = regexp.MustCompile(`(?s)\s*<PreActions>.*?` + regexp.QuoteMeta(Marker) + `.*?</PreActions>\n?`)
)

type Status int

const (
	StatusInstalled Status = iota
	StatusAlreadyInstalled
	StatusUninstalled
	StatusNotInstalled
)

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

	insertAt := loc[1]
	if !strings.HasPrefix(string(data[insertAt:]), "\n") {
		insertAt += 0
	}

	out := make([]byte, 0, len(data)+len(PreActionBlock)+1)
	out = append(out, data[:insertAt]...)
	out = append(out, '\n')
	out = append(out, []byte(PreActionBlock)...)
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
