package auth

// Backend is where a credential physically lives.
type Backend int

const (
	BackendNone Backend = iota
	BackendEnv
	BackendJWT
	BackendKeychain
	BackendFile
)

// Provenance is how the credential got there. It is independent of Backend: the
// config file holds both `auth login` credentials and the legacy analytics
// authConfig, and only provenance tells them apart.
type Provenance int

const (
	ProvenanceNone Provenance = iota
	// ProvenanceInjected is an env var or the CI JWT — supplied by the environment,
	// carrying no refresh token.
	ProvenanceInjected
	ProvenanceOAuthLogin
	ProvenanceManual
	// ProvenanceLegacy is the multiplatform config's authConfig key, kept for
	// analytics readers predating the credentials key.
	ProvenanceLegacy
)

// Origin is where a resolved credential came from.
type Origin struct {
	Backend    Backend
	Provenance Provenance
}

// StoreManaged reports whether the credential lives in a backend this CLI writes,
// which is what makes it refreshable. Test this, never a specific backend: both the
// keychain and the config file can hold an OAuth login. The legacy authConfig block
// is excluded — it predates the refresh token and has nothing to refresh with.
func (o Origin) StoreManaged() bool {
	if o.Provenance == ProvenanceLegacy {
		return false
	}

	return o.Backend == BackendKeychain || o.Backend == BackendFile
}

func (o Origin) Resolved() bool {
	return o.Backend != BackendNone
}

const labelNone = "none"

// Label is the prose name, for user-facing output.
func (o Origin) Label() string {
	switch o.Backend {
	case BackendEnv:
		return "environment variables"
	case BackendJWT:
		return "CI JWT (" + EnvJWT + ")"
	case BackendKeychain:
		if o.Provenance == ProvenanceOAuthLogin {
			return "OAuth login (keychain)"
		}

		return "OS keychain"
	case BackendFile:
		switch o.Provenance {
		case ProvenanceOAuthLogin:
			return "OAuth login (config file)"
		case ProvenanceLegacy:
			return "multiplatform config"
		case ProvenanceNone, ProvenanceInjected, ProvenanceManual:
		}

		return "config file (CI-safe)"
	case BackendNone:
		return labelNone
	}

	return labelNone
}

// ShortLabel is the compact machine-ish name, for diagnostics and --json.
func (o Origin) ShortLabel() string {
	switch o.Backend {
	case BackendEnv:
		return "env"
	case BackendJWT:
		return "jwt"
	case BackendKeychain:
		return "keychain"
	case BackendFile:
		if o.Provenance == ProvenanceLegacy {
			return "multiplatform-config"
		}

		return "config-file"
	case BackendNone:
		return labelNone
	}

	return "unknown"
}

// GradleToken renders the token the way the Gradle remote cache expects it. A JWT
// already embeds the workspace and is sent as-is; a PAT is prefixed. Needs both
// values, which is why it is a free function rather than a method.
func GradleToken(c Credential, o Origin) string {
	if o.Backend == BackendJWT || c.WorkspaceID == "" {
		return c.Token
	}

	return c.WorkspaceID + ":" + c.Token
}

func (b Backend) String() string {
	switch b {
	case BackendEnv:
		return "env"
	case BackendJWT:
		return "jwt"
	case BackendKeychain:
		return "keychain"
	case BackendFile:
		return "file"
	case BackendNone:
		return labelNone
	}

	return "unknown"
}
