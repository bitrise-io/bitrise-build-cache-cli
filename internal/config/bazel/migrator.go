package bazelconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
)

// SidecarMigrator implements toolconfig.Migrator for the bazel sidecar.
type SidecarMigrator struct{}

func (SidecarMigrator) Tool() toolconfig.Tool { return toolconfig.Bazel }

func (SidecarMigrator) Migrate(home string) error {
	s, ok, err := ReadSidecar(home)
	if err != nil {
		return fmt.Errorf("read bazel sidecar: %w", err)
	}

	if !ok {
		return nil
	}

	if err := dropStoredRepoURL(s.BazelrcPath); err != nil {
		return err
	}

	if err := WriteSidecar(home, s); err != nil {
		return fmt.Errorf("rewrite bazel sidecar: %w", err)
	}

	return nil
}

// dropStoredRepoURL removes the x-repository-url lines an older activation left
// in the generated block. Without this, machines keep attributing every Bazel
// project to one repo until someone re-runs `activate bazel`.
func dropStoredRepoURL(bazelrcPath string) error {
	if bazelrcPath == "" {
		return nil
	}

	body, err := os.ReadFile(bazelrcPath) //nolint:gosec // path comes from the sidecar the CLI wrote
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("read %s: %w", bazelrcPath, err)
	}

	updated, changed := withoutRepoURLHeaders(string(body))
	if !changed {
		return nil
	}

	if err := os.WriteFile(bazelrcPath, []byte(updated), 0o644); err != nil { //nolint:gosec // matches the activation's mode
		return fmt.Errorf("rewrite %s: %w", bazelrcPath, err)
	}

	return nil
}

// Lines outside the generated block are the user's own and stay untouched.
func withoutRepoURLHeaders(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	inBlock := false
	changed := false

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, bazelBlockStart):
			inBlock = true
		case strings.HasPrefix(line, bazelBlockEnd):
			inBlock = false
		case inBlock && strings.Contains(line, "x-repository-url="):
			changed = true

			continue
		}

		kept = append(kept, line)
	}

	return strings.Join(kept, "\n"), changed
}
