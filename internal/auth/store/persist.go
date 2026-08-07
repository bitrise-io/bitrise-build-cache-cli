package store

import (
	"fmt"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

// SetUsername writes name into the store that already holds credentials so a
// username-only edit can't strand an empty-token entry in the wrong backend.
// Empty name clears the override. Returns the store written to.
func SetUsername(isCI bool, name string) (auth.Origin, error) {
	target, existing := storeHoldingCreds(isCI)
	existing.Username = strings.TrimSpace(name)
	if err := target.Save(existing); err != nil {
		return existing.Origin(target.Backend()), fmt.Errorf("save display name to %s: %w", target.Backend().String(), err)
	}

	return existing.Origin(target.Backend()), nil
}

func storeHoldingCreds(isCI bool) (Store, auth.TokenSet) {
	for _, s := range []Store{NewKeychain(), NewFile()} {
		creds, err := s.Load()
		if err == nil && (strings.TrimSpace(creds.AuthToken) != "" || strings.TrimSpace(creds.WorkspaceID) != "") {
			return s, creds
		}
	}

	target := SelectAuto(isCI)
	creds, _ := target.Load()

	return target, creds
}
