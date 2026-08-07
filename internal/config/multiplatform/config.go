package multiplatform

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const (
	configPath = ".bitrise/analytics/multiplatform"
	configFile = "config.json"

	ErrFmtOpenConfigFile   = "open multiplatform analytics config file (%s): %w"
	ErrFmtDecodeConfigFile = "decode multiplatform analytics config file (%s): %w"
	ErrFmtCreateConfigFile = "failed to create multiplatform analytics config file: %w"
	ErrFmtEncodeConfigFile = "failed to encode multiplatform analytics config file: %w"
	ErrFmtCreateFolder     = "failed to create %s folder: %w"
)

// LegacyAuthConfig is the pre-credentials on-disk shape. The field names are the
// wire format — analytics readers and older CLI versions parse them by name, so
// they must not be renamed even though the in-memory type is auth.Credential now.
type LegacyAuthConfig struct {
	AuthToken   string
	WorkspaceID string
	IsJWT       bool
}

// Populated reports whether the legacy block carries a usable credential.
func (l LegacyAuthConfig) Populated() bool {
	return l.AuthToken != "" && l.WorkspaceID != ""
}

// Credential narrows the legacy block to the boundary type.
func (l LegacyAuthConfig) Credential() auth.Credential {
	return auth.Credential{Token: l.AuthToken, WorkspaceID: l.WorkspaceID}
}

// Credentials is the CI-safe file backend for auth set/login; AuthConfig stays for backward compatibility with older analytics readers.
type Config struct {
	AuthConfig   LegacyAuthConfig `json:"authConfig"`
	Credentials  *auth.TokenSet   `json:"credentials,omitempty"`
	DebugLogging bool             `json:"debugLogging,omitempty"`
}

func dirPath(osProxy utils.OsProxy) string {
	if home, err := osProxy.UserHomeDir(); err == nil {
		return filepath.Join(home, configPath)
	}

	if wd, err := osProxy.Getwd(); err == nil {
		return filepath.Join(wd, configPath)
	}

	return filepath.Join(".", configPath)
}

// FilePath returns the absolute path of the multiplatform analytics config file.
func FilePath(osProxy utils.OsProxy) string {
	return filepath.Join(dirPath(osProxy), configFile)
}

// Atomic write with 0600 perms — file holds PATs and OAuth refresh tokens.
func (c Config) Save(osProxy utils.OsProxy, encoderFactory utils.EncoderFactory) error {
	dir := dirPath(osProxy)
	if err := osProxy.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf(ErrFmtCreateFolder, dir, err)
	}

	var buf bytes.Buffer
	enc := encoderFactory.Encoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(c); err != nil {
		return fmt.Errorf(ErrFmtEncodeConfigFile, err)
	}

	path := FilePath(osProxy)
	// Per-process: a shared temp name lets one writer truncate the bytes another is
	// about to rename into place.
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := osProxy.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf(ErrFmtCreateConfigFile, err)
	}
	if err := osProxy.Rename(tmp, path); err != nil {
		_ = osProxy.Remove(tmp)

		return fmt.Errorf("rename multiplatform config file: %w", err)
	}

	return nil
}

// Update applies mutate to the config on disk, read-modify-write. Config.Save is a
// full overwrite, so anything that means to change one field must go through here
// or it silently drops every field it did not set — including the credentials
// block that is the only credential store on a keychain-less host.
func Update(osProxy utils.OsProxy, encoderFactory utils.EncoderFactory, decoderFactory utils.DecoderFactory, mutate func(*Config)) error {
	cfg, err := ReadConfig(osProxy, decoderFactory)
	if err != nil && !isNotExist(err) {
		return err
	}

	mutate(&cfg)

	return cfg.Save(osProxy, encoderFactory)
}

// Mirrors creds into legacy AuthConfig so downstream reactnative/invocation readers keep working.
func SaveCredentials(osProxy utils.OsProxy, encoderFactory utils.EncoderFactory, decoderFactory utils.DecoderFactory, creds auth.TokenSet) error {
	cfg, err := ReadConfig(osProxy, decoderFactory)
	if err != nil && !isNotExist(err) {
		return err
	}

	c := creds
	cfg.Credentials = &c
	cfg.AuthConfig = LegacyAuthConfig{AuthToken: creds.AuthToken, WorkspaceID: creds.WorkspaceID}

	return cfg.Save(osProxy, encoderFactory)
}

func ReadCredentials(osProxy utils.OsProxy, decoderFactory utils.DecoderFactory) (auth.TokenSet, bool) {
	cfg, err := ReadConfig(osProxy, decoderFactory)
	if err != nil || cfg.Credentials == nil {
		return auth.TokenSet{}, false
	}

	return *cfg.Credentials, true
}

func ClearCredentials(osProxy utils.OsProxy, encoderFactory utils.EncoderFactory, decoderFactory utils.DecoderFactory) error {
	cfg, err := ReadConfig(osProxy, decoderFactory)
	if err != nil {
		if isNotExist(err) {
			return nil
		}

		return err
	}
	if cfg.Credentials == nil && cfg.AuthConfig.AuthToken == "" {
		return nil
	}
	cfg.Credentials = nil
	cfg.AuthConfig = LegacyAuthConfig{}

	return cfg.Save(osProxy, encoderFactory)
}

func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

// ReadConfig loads the config from disk.
func ReadConfig(osProxy utils.OsProxy, decoderFactory utils.DecoderFactory) (Config, error) {
	path := FilePath(osProxy)

	f, err := osProxy.OpenFile(path, 0, 0)
	if err != nil {
		return Config{}, fmt.Errorf(ErrFmtOpenConfigFile, path, err)
	}
	defer f.Close()

	dec := decoderFactory.Decoder(f)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf(ErrFmtDecodeConfigFile, path, err)
	}

	return cfg, nil
}
