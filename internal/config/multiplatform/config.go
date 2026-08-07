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

// AnalyticsAuthConfig is the credential the analytics consumers read: the React
// Native post-run hook, the ccache invocation registry, and readers outside this
// repo. Actively written on every activation — it is not deprecated. It carries no
// refresh machinery, which is why it is a separate type from auth.TokenSet rather
// than a narrower view of it.
//
// The field names are the wire format; parsers match them by name, so they cannot
// be renamed to match auth.Credential.
type AnalyticsAuthConfig struct {
	AuthToken   string
	WorkspaceID string
	IsJWT       bool
}

func (l AnalyticsAuthConfig) Populated() bool {
	return l.AuthToken != "" && l.WorkspaceID != ""
}

func (l AnalyticsAuthConfig) Credential() auth.Credential {
	return auth.Credential{Token: l.AuthToken, WorkspaceID: l.WorkspaceID}
}

// Origin reports where the legacy block's credential came from. IsJWT is
// load-bearing: a JWT is sent as-is, a PAT is prefixed with the workspace.
func (l AnalyticsAuthConfig) Origin() auth.Origin {
	if l.IsJWT {
		return auth.Origin{Backend: auth.BackendJWT, Provenance: auth.ProvenanceInjected}
	}

	return auth.Origin{Backend: auth.BackendFile, Provenance: auth.ProvenanceStatic}
}

// Credentials is the CI-safe file backend for auth set/login; AuthConfig stays for backward compatibility with older analytics readers.
type Config struct {
	AuthConfig   AnalyticsAuthConfig `json:"authConfig"`
	Credentials  *auth.TokenSet      `json:"credentials,omitempty"`
	DebugLogging bool                `json:"debugLogging,omitempty"`
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

// Read-modify-write. Config.Save is a full overwrite, so changing one field without
// this drops every field it did not set — including the credentials block that is
// the only credential store on a keychain-less host.
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
	cfg.AuthConfig = AnalyticsAuthConfig{AuthToken: creds.AuthToken, WorkspaceID: creds.WorkspaceID}

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
	cfg.AuthConfig = AnalyticsAuthConfig{}

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
