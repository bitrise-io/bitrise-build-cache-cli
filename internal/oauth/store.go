package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Credentials is the on-disk OAuth credential file
// (~/.bitrise/build-cache/auth.json, 0600). WorkspaceID is stored because the
// cache is workspace-scoped while OAuth login is user-scoped.
type Credentials struct {
	PAT                string    `json:"pat,omitempty"`
	PATExpiry          time.Time `json:"pat_expiry,omitempty"`
	JWT                string    `json:"jwt,omitempty"`
	JWTExpiry          time.Time `json:"jwt_expiry,omitempty"`
	RefreshToken       string    `json:"refresh_token,omitempty"`
	RefreshTokenExpiry time.Time `json:"refresh_token_expiry,omitempty"`
	WorkspaceID        string    `json:"workspace_id,omitempty"`
}

// IsOAuthManaged reports whether the credential came from OAuth (has a refresh token).
func (c Credentials) IsOAuthManaged() bool {
	return c.RefreshToken != ""
}

// CredentialsPath returns the absolute path to the OAuth credential file.
func CredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate user home dir: %w", err)
	}

	return filepath.Join(home, ".bitrise", "build-cache", "auth.json"), nil
}

// Load reads the credential file. A missing file returns the zero Credentials
// so a not-logged-in user doesn't see an error.
func Load() (Credentials, error) {
	p, err := CredentialsPath()
	if err != nil {
		return Credentials{}, err
	}
	data, err := os.ReadFile(p) //nolint:gosec // p is derived from the user home dir, not user input
	if errors.Is(err, fs.ErrNotExist) {
		return Credentials{}, nil
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("read %s: %w", p, err)
	}
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return Credentials{}, fmt.Errorf("parse %s: %w", p, err)
	}

	return c, nil
}

// Save atomically writes c to disk with 0600 permissions, creating the parent
// directory (0700) if needed.
func Save(c Credentials) error {
	if c.PAT == "" {
		return errors.New("refusing to save credentials with empty PAT")
	}
	p, err := CredentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(&c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("install %s: %w", p, err)
	}

	return nil
}

// Clear removes the credential file. A non-existent file is not an error.
func Clear() error {
	p, err := CredentialsPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", p, err)
	}

	return nil
}
