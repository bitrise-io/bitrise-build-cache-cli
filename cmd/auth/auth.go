package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform"
	xceleratconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// nolint:gochecknoglobals
var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage Bitrise Build Cache credentials stored in the OS keychain",
	Long: fmt.Sprintf("Manage Bitrise Build Cache credentials stored in the OS keychain (macOS Keychain, Linux secret-service). "+
		"Stored credentials are used when %s / %s (or %s on Bitrise CI) are not set — env vars take precedence "+
		"so you can override the stored credentials for a single run.",
		configcommon.EnvAuthToken, configcommon.EnvWorkspaceID, configcommon.EnvJWT),
	SilenceUsage: true,
}

// nolint:gochecknoglobals
var (
	setToken       string
	setWorkspaceID string
	setUsername    string
	setStorage     string
)

// nolint:gochecknoglobals
var authSetCmd = &cobra.Command{
	Use:          "set",
	Short:        "Store Bitrise Build Cache credentials in the OS keychain",
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		setToken = strings.TrimSpace(setToken)
		setWorkspaceID = strings.TrimSpace(setWorkspaceID)
		setUsername = strings.TrimSpace(setUsername)

		switch {
		case setToken == "" && setWorkspaceID == "":
			return errors.New("--token and --workspace-id are required and must not be empty")
		case setToken == "":
			return errors.New("--token is required and must not be empty")
		case setWorkspaceID == "":
			return errors.New("--workspace-id is required and must not be empty")
		}

		target, err := store.Select(utils.AllEnvs(), setStorage)
		if err != nil {
			return err //nolint:wrapcheck // already user-facing
		}
		existing, err := target.Load()
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("load existing credentials: %w", err)
		}
		existing.AuthToken = setToken
		existing.WorkspaceID = setWorkspaceID
		existing.Username = setUsername
		if err := store.SaveExclusive(target, existing); err != nil {
			return fmt.Errorf("save credentials: %w", err)
		}

		switch target.Kind() {
		case store.KindKeychain:
			logger.TInfof("✅ Credentials saved to the OS keychain")
		case store.KindFile:
			logger.TInfof("✅ Credentials saved to the multiplatform config file (%s)", displayHomePath(utils.DefaultOsProxy{}, multiplatformconfig.FilePath(utils.DefaultOsProxy{})))
			if configcommon.DetectCIProvider(utils.AllEnvs()) != "" && setStorage == "" {
				logger.Infof("(CI detected — keychain skipped because fastlane setup_ci swaps the default keychain and would drop the entry.)")
			}
		}
		if setUsername != "" {
			logger.TInfof("Display name for local invocations set to %q.", setUsername)
		}

		switch scrubbed, err := scrubDiskCredentials(target.Kind()); {
		case err != nil:
			logger.Warnf("Saved to %s, but could not strip plain-text credentials from disk: %v", target.Kind(), err)
			logger.Warnf("Run `bitrise-build-cache auth status` to audit remaining sources.")
		case len(scrubbed) > 0:
			scrubbedPaths := make([]string, len(scrubbed))
			for i, item := range scrubbed {
				scrubbedPaths[i] = item.path
			}

			logger.TInfof("Scrubbed plain-text credentials from %s", strings.Join(scrubbedPaths, ", "))
			for _, item := range scrubbed {
				if item.hint != "" {
					logger.Infof("  → %s", item.hint)
				}
			}
		}

		logger.Infof("You can now remove %s + %s from your shell rc files.", configcommon.EnvAuthToken, configcommon.EnvWorkspaceID)
		logger.Infof("If you have running Gradle daemons, stop them so the new token is picked up: `./gradlew --stop`.")

		return nil
	},
}

// scrubbedItem pairs a scrubbed file path with the reactivate command the user
// should run to regenerate a clean config (empty hint = no follow-up needed).
type scrubbedItem struct {
	path string
	hint string
}

func scrubDiskCredentials(target store.Kind) ([]scrubbedItem, error) {
	osProxy := utils.DefaultOsProxy{}

	scrubbers := []func(utils.OsProxy) (scrubbedItem, error){
		scrubXcelerate,
		scrubCcache,
		scrubGradleInitKts,
	}
	// Don't scrub the file we just wrote to (different field, same path — confusing on CI logs).
	if target != store.KindFile {
		scrubbers = append([]func(utils.OsProxy) (scrubbedItem, error){scrubMultiplatform}, scrubbers...)
	}

	var scrubbed []scrubbedItem

	for _, scrub := range scrubbers {
		item, err := scrub(osProxy)
		if err != nil {
			return scrubbed, err
		}
		if item.path != "" {
			scrubbed = append(scrubbed, item)
		}
	}

	return scrubbed, nil
}

func scrubMultiplatform(osProxy utils.OsProxy) (scrubbedItem, error) {
	cfg, err := multiplatformconfig.ReadConfig(osProxy, utils.DefaultDecoderFactory{})
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return scrubbedItem{}, nil
	case err != nil:
		return scrubbedItem{}, fmt.Errorf("read multiplatform config: %w", err)
	}

	if cfg.AuthConfig.AuthToken == "" && cfg.AuthConfig.WorkspaceID == "" {
		return scrubbedItem{}, nil
	}

	cfg.AuthConfig = configcommon.CacheAuthConfig{}
	if err := cfg.Save(osProxy, utils.DefaultEncoderFactory{}); err != nil {
		return scrubbedItem{}, fmt.Errorf("save scrubbed multiplatform config: %w", err)
	}

	return scrubbedItem{path: displayHomePath(osProxy, multiplatformconfig.FilePath(osProxy))}, nil
}

func scrubXcelerate(osProxy utils.OsProxy) (scrubbedItem, error) {
	path, err := scrubRawConfigAuthToken(osProxy, xceleratconfig.ConfigFile(osProxy))
	if err != nil || path == "" {
		return scrubbedItem{}, err
	}

	return scrubbedItem{path: path, hint: "Run `bitrise-build-cache activate xcode` to regenerate a clean config"}, nil
}

func scrubCcache(osProxy utils.OsProxy) (scrubbedItem, error) {
	path, err := scrubRawConfigAuthToken(osProxy, ccacheconfig.ConfigFile(osProxy))
	if err != nil || path == "" {
		return scrubbedItem{}, err
	}

	return scrubbedItem{path: path, hint: "Run `bitrise-build-cache activate c++` to regenerate a clean config"}, nil
}

// Legacy files from older CLI versions still carry authConfig on disk; current activate path no longer writes it.
func scrubRawConfigAuthToken(osProxy utils.OsProxy, fullPath string) (string, error) {
	displayPath := displayHomePath(osProxy, fullPath)

	body, err := os.ReadFile(fullPath) //nolint:gosec // path composed via the typed paths helpers
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("read %s: %w", displayPath, err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", fmt.Errorf("decode %s: %w", displayPath, err)
	}

	ac, hasAuth := raw["authConfig"]
	if !hasAuth {
		return "", nil
	}

	var auth struct {
		AuthToken   string `json:"authToken"`
		WorkspaceID string `json:"workspaceID"`
	}
	if err := json.Unmarshal(ac, &auth); err != nil {
		return "", fmt.Errorf("decode authConfig in %s: %w", displayPath, err)
	}
	if auth.AuthToken == "" && auth.WorkspaceID == "" {
		return "", nil
	}

	delete(raw, "authConfig")

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return "", fmt.Errorf("re-encode %s: %w", displayPath, err)
	}

	if err := os.WriteFile(fullPath, append(out, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("write scrubbed %s: %w", displayPath, err)
	}

	return displayPath, nil
}

// Older templates and CI activates bake the token as a literal in init.kts; ValueSource activates leave nothing to scrub.
func scrubGradleInitKts(osProxy utils.OsProxy) (scrubbedItem, error) {
	home, err := osProxy.UserHomeDir()
	if err != nil {
		return scrubbedItem{}, fmt.Errorf("resolve home dir: %w", err)
	}

	fullPath := paths.FromHome(home).GradleInitScriptFile()

	body, err := os.ReadFile(fullPath) //nolint:gosec // path composed via the typed paths helpers
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return scrubbedItem{}, nil
	case err != nil:
		return scrubbedItem{}, fmt.Errorf("read gradle init.kts: %w", err)
	}

	scrubbed := authTokenLiteralRE.ReplaceAllStringFunc(string(body), func(match string) string {
		return quotedStringRE.ReplaceAllString(match, `""`)
	})

	if scrubbed == string(body) {
		return scrubbedItem{}, nil
	}

	if err := os.WriteFile(fullPath, []byte(scrubbed), 0o600); err != nil {
		return scrubbedItem{}, fmt.Errorf("write scrubbed gradle init.kts: %w", err)
	}

	return scrubbedItem{
		path: displayHomePath(osProxy, fullPath),
		hint: "Run `bitrise-build-cache activate gradle` to regenerate the init script with the ValueSource resolver",
	}, nil
}

// displayHomePath returns the absolute path with the user's home dir replaced by `~`.
func displayHomePath(osProxy utils.OsProxy, full string) string {
	home, err := osProxy.UserHomeDir()
	if err != nil {
		return full
	}

	rel, err := filepath.Rel(home, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return full
	}

	return "~/" + filepath.ToSlash(rel)
}

//nolint:gochecknoglobals
var (
	authTokenLiteralRE = regexp.MustCompile(`authToken(?:\s*=\s*|\.set\()"[^"]+"`)
	quotedStringRE     = regexp.MustCompile(`"[^"]+"`)
)

// nolint:gochecknoglobals
var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Bitrise Build Cache credentials discovered across all known sources",
	Long: fmt.Sprintf("Shows credentials found in the OS keychain, the multiplatform analytics config on disk, "+
		"and the %s / %s / %s env vars. Use this to audit where your credentials live and to migrate them to the OS keychain.",
		configcommon.EnvAuthToken, configcommon.EnvWorkspaceID, configcommon.EnvJWT),
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		targets, migrationSources := credSources(utils.AllEnvs())

		var targetPopulated bool
		for _, t := range targets {
			if renderSource(logger, t, t.probe()) {
				targetPopulated = true
			}
		}

		var foundElsewhere, suppressed bool
		for _, s := range migrationSources {
			audit := s.probe()
			if audit.state == sourceAbsent && !common.IsDebugLogMode {
				suppressed = true

				continue
			}
			if renderSource(logger, s, audit) {
				foundElsewhere = true
			}
		}

		if suppressed {
			logger.Infof("(absent sources hidden — re-run with --debug to see the full audit)")
		}

		logger.Println()
		renderUsername(logger, utils.AllEnvs())

		if !targetPopulated && foundElsewhere {
			logger.Println()
			logger.Infof("Credentials exist outside the managed backends — migrate with:")
			logger.Infof("  bitrise-build-cache auth set --token <token> --workspace-id <workspace-id>")
			logger.Infof("`auth set` picks keychain locally and file storage on CI (fastlane setup_ci wipes the keychain).")
		}

		return nil
	},
}

func renderUsername(logger log.Logger, envs map[string]string) {
	name, src := configcommon.ResolveUsername(envs)
	switch src {
	case configcommon.UsernameSourceEnv:
		logger.Infof("Local invocation display name: %s (source: %s env)", name, configcommon.EnvUsername)
	case configcommon.UsernameSourceKeychain:
		logger.Infof("Local invocation display name: %s (source: keychain)", name)
	case configcommon.UsernameSourceFile:
		logger.Infof("Local invocation display name: %s (source: config file)", name)
	case configcommon.UsernameSourceOS:
		logger.Infof("Local invocation display name: %s (source: OS username fallback)", name)
	case configcommon.UsernameSourceNone:
		logger.Infof("Local invocation display name: (none — set via `auth set --username <name>` or %s)", configcommon.EnvUsername)
	}
}

type credSourceState int

const (
	sourceAbsent credSourceState = iota
	sourcePartial
	sourceReadError
	sourcePopulated
	sourcePopulatedTokenOnly
)

type credAudit struct {
	state       credSourceState
	workspaceID string
	authToken   string
	username    string
	note        string
	err         error
}

type credSource struct {
	label    string
	location string
	probe    func() credAudit
}

func credSources(envs map[string]string) ([]credSource, []credSource) {
	osProxy := utils.DefaultOsProxy{}

	xcelerateConfigPath := xceleratconfig.ConfigFile(osProxy)
	ccacheConfigPath := ccacheconfig.ConfigFile(osProxy)
	multiplatformConfigPath := multiplatformconfig.FilePath(osProxy)

	targets := []credSource{
		{"OS keychain", "<system keychain>", probeKeychain},
		{"Config file (CI-safe)", displayHomePath(osProxy, multiplatformConfigPath), probeFileStore},
	}
	migrationSources := []credSource{
		{"Multiplatform config (legacy authConfig)", displayHomePath(osProxy, multiplatformConfigPath), probeMultiplatform},
		{"Xcelerate config", displayHomePath(osProxy, xcelerateConfigPath), probeRawConfig(xcelerateConfigPath)},
		{"Ccache config", displayHomePath(osProxy, ccacheConfigPath), probeRawConfig(ccacheConfigPath)},
		{fmt.Sprintf("Env vars (%s + %s)", configcommon.EnvAuthToken, configcommon.EnvWorkspaceID), "process env", func() credAudit { return probeEnvVars(envs) }},
		{fmt.Sprintf("CI JWT (%s)", configcommon.EnvJWT), "process env", func() credAudit { return probeJWT(envs) }},
	}

	return targets, migrationSources
}

func renderSource(logger log.Logger, s credSource, a credAudit) bool {
	if s.location != "" {
		logger.TInfof("%s (%s):", s.label, s.location)
	} else {
		logger.TInfof("%s:", s.label)
	}

	switch a.state {
	case sourceAbsent:
		if a.note != "" {
			logger.Infof("  %s", a.note)
		} else {
			logger.Infof("  not configured")
		}

		return false
	case sourcePartial:
		logger.Warnf("  partial: %s", a.note)

		return false
	case sourceReadError:
		logger.Errorf("  read failed: %v", a.err)

		return false
	case sourcePopulated:
		logger.Infof("  Workspace ID: %s", a.workspaceID)
		logger.Infof("  Auth token:   %s", maskToken(a.authToken))
		if a.username != "" {
			logger.Infof("  Display name: %s", a.username)
		}
		if a.note != "" {
			logger.Infof("  %s", a.note)
		}

		return true
	case sourcePopulatedTokenOnly:
		logger.Infof("  set (%s)", maskToken(a.authToken))

		return true
	}

	return false
}

func probeKeychain() credAudit {
	creds, err := keychain.New().Load()
	switch {
	case errors.Is(err, keychain.ErrNotFound):
		return credAudit{state: sourceAbsent}
	case err != nil:
		return credAudit{state: sourceReadError, err: err}
	}

	audit := credAudit{state: sourcePopulated, workspaceID: creds.WorkspaceID, authToken: creds.AuthToken, username: creds.Username}
	if desc := configcommon.DescribeKeychainCredentials(creds); desc.IsOAuthLogin {
		audit.note = desc.Detail()
	}

	return audit
}

// Reads mp.Credentials (new file backend), distinct from legacy authConfig in probeMultiplatform.
func probeFileStore() credAudit {
	creds, ok := multiplatformconfig.ReadCredentials(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	if !ok {
		return credAudit{state: sourceAbsent, note: "not present"}
	}
	if creds.AuthToken == "" {
		return credAudit{state: sourceAbsent, note: "credentials block present but empty"}
	}

	return credAudit{state: sourcePopulated, workspaceID: creds.WorkspaceID, authToken: creds.AuthToken, username: creds.Username}
}

func probeMultiplatform() credAudit {
	cfg, err := multiplatformconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return credAudit{state: sourceAbsent, note: "not present"}
	case err != nil:
		return credAudit{state: sourceReadError, err: err}
	case cfg.AuthConfig.AuthToken == "":
		return credAudit{state: sourceAbsent, note: "present but no credentials"}
	case cfg.Credentials != nil:
		return credAudit{state: sourceAbsent, note: "mirrors the file-store credentials above"}
	}

	return credAudit{state: sourcePopulated, workspaceID: cfg.AuthConfig.WorkspaceID, authToken: cfg.AuthConfig.AuthToken}
}

// probeRawConfig reads the file directly — the per-tool ReadConfig overlays multiplatform credentials, which would mask the actual on-disk content this audit needs to see.
func probeRawConfig(fullPath string) func() credAudit {
	return func() credAudit {
		body, err := os.ReadFile(fullPath) //nolint:gosec // path composed via the typed paths helpers
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return credAudit{state: sourceAbsent, note: "not present"}
		case err != nil:
			return credAudit{state: sourceReadError, err: err}
		}

		var raw struct {
			AuthConfig struct {
				AuthToken   string `json:"authToken"`
				WorkspaceID string `json:"workspaceID"`
			} `json:"authConfig"`
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			return credAudit{state: sourceReadError, err: fmt.Errorf("decode: %w", err)}
		}

		if raw.AuthConfig.AuthToken == "" {
			return credAudit{state: sourceAbsent, note: "present but no embedded credentials"}
		}

		return credAudit{state: sourcePopulated, workspaceID: raw.AuthConfig.WorkspaceID, authToken: raw.AuthConfig.AuthToken}
	}
}

func probeEnvVars(envs map[string]string) credAudit {
	tok := envs[configcommon.EnvAuthToken]
	ws := envs[configcommon.EnvWorkspaceID]

	switch {
	case tok != "" && ws != "":
		return credAudit{state: sourcePopulated, workspaceID: ws, authToken: tok}
	case tok != "" || ws != "":
		return credAudit{state: sourcePartial, note: "only one of AUTH_TOKEN / WORKSPACE_ID is set"}
	}

	return credAudit{state: sourceAbsent, note: "not set"}
}

func probeJWT(envs map[string]string) credAudit {
	jwt := envs[configcommon.EnvJWT]
	if jwt == "" {
		return credAudit{state: sourceAbsent, note: "not set"}
	}

	return credAudit{state: sourcePopulatedTokenOnly, authToken: jwt}
}

// nolint:gochecknoglobals
var (
	clearStorage string
)

// nolint:gochecknoglobals
var authClearCmd = &cobra.Command{
	Use:          "clear",
	Short:        "Remove Bitrise Build Cache credentials from the OS keychain and the multiplatform config file",
	Long:         "By default clears the OS keychain + the multiplatform config file. Legacy per-tool copies (xcelerate/ccache/gradle-init) are scrubbed via `auth set`, not here. Use --storage=keychain|file to target one backend.",
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))

		var targets []store.Store
		switch clearStorage {
		case "", "auto":
			targets = []store.Store{store.NewKeychain(), store.NewFile()}
		case "keychain":
			targets = []store.Store{store.NewKeychain()}
		case "file":
			targets = []store.Store{store.NewFile()}
		default:
			return fmt.Errorf("unknown --storage %q (want keychain|file|auto)", clearStorage)
		}

		for _, t := range targets {
			if err := t.Clear(); err != nil {
				return fmt.Errorf("clear %s: %w", t.Kind(), err)
			}
			logger.TInfof("✅ Credentials removed from %s", t.Kind())
		}

		return nil
	},
}

// nolint:gochecknoglobals
var authTokenCmd = &cobra.Command{
	Use:           "token",
	Short:         "Resolve and print the Bitrise Build Cache auth token to stdout",
	Long:          "Resolves the auth token via the same precedence chain as the rest of the CLI (env vars → OS keychain → multiplatform analytics config) and prints it to stdout. Intended for build-time consumers (Gradle init script, future Bazel workspace_status_command) that need the resolved token without baking it into a config file. On failure exits non-zero with a short one-line message on stderr (no cobra Error: prefix) — callers framing the wrapper script own the wording.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, _, err := configcommon.ResolveAuthConfig(utils.AllEnvs())
		if err != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), err.Error())

			return fmt.Errorf("resolve auth config: %w", err)
		}

		if _, err := fmt.Fprintln(cmd.OutOrStdout(), cfg.TokenInGradleFormat()); err != nil {
			return fmt.Errorf("write auth token: %w", err)
		}

		return nil
	},
}

func maskToken(token string) string {
	const tailLen = 4
	if len(token) <= tailLen {
		return "(present, length too short to mask)"
	}

	return fmt.Sprintf("****%s", token[len(token)-tailLen:])
}

func init() {
	authSetCmd.Flags().StringVar(&setToken, "token", "", "Bitrise Build Cache auth token (required)")
	authSetCmd.Flags().StringVar(&setWorkspaceID, "workspace-id", "", "Bitrise workspace ID (required)")
	authSetCmd.Flags().StringVar(&setUsername, "username", "", fmt.Sprintf("Display name for local invocations (optional). Overrides the OS username. Env var %s takes precedence for a single run.", configcommon.EnvUsername))
	authSetCmd.Flags().StringVar(&setStorage, "storage", "", "Where to persist credentials: keychain (OS keychain) | file (multiplatform config on disk) | auto (default: CI→file, local→keychain). File storage is required on CI where fastlane setup_ci swaps the default keychain.")
	_ = authSetCmd.MarkFlagRequired("token")
	_ = authSetCmd.MarkFlagRequired("workspace-id")

	authClearCmd.Flags().StringVar(&clearStorage, "storage", "", "Which backend to clear: keychain | file | auto (default auto clears both).")

	authCmd.AddCommand(authSetCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authClearCmd)
	authCmd.AddCommand(authTokenCmd)
	authCmd.AddCommand(common.LoginCmd)
	authCmd.AddCommand(common.LogoutCmd)

	common.RootCmd.AddCommand(authCmd)
}
