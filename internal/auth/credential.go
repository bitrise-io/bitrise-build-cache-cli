// Package auth is the shared credential vocabulary. It is a leaf: it imports no
// other package in this module, so every layer above can speak it without a cycle.
// See docs/auth.md.
package auth

import "time"

// Credential is what a caller needs to make one authenticated call. Every field is
// always meaningful; the refresh machinery stays in TokenSet, below the boundary.
type Credential struct {
	Token       string
	WorkspaceID string
	Username    string
	Expiry      time.Time
}

// Expired reports whether a known expiry is in the past. A zero Expiry is unknown,
// not expired.
func (c Credential) Expired() bool {
	return !c.Expiry.IsZero() && time.Now().After(c.Expiry)
}

// TokenSet is the persisted record, identical for both backends. AuthToken +
// WorkspaceID are always present; the rest is set only by an OAuth login, where
// AuthToken is the minted PAT and the refresh token drives transparent refresh.
type TokenSet struct {
	AuthToken          string    `json:"auth_token"`
	WorkspaceID        string    `json:"workspace_id"`
	Username           string    `json:"username,omitempty"`
	PATExpiry          time.Time `json:"pat_expiry,omitempty"`
	JWT                string    `json:"jwt,omitempty"`
	JWTExpiry          time.Time `json:"jwt_expiry,omitempty"`
	RefreshToken       string    `json:"refresh_token,omitempty"`
	RefreshTokenExpiry time.Time `json:"refresh_token_expiry,omitempty"`
}

// IsOAuthManaged reports whether the record came from `auth login` rather than a
// manual `auth set`.
func (t TokenSet) IsOAuthManaged() bool {
	return t.RefreshToken != ""
}

// Credential narrows the record to the boundary. This is the only conversion
// between the two types; there is deliberately no reverse. Writes load the stored
// record, mutate the fields they own and save it back, so a write path cannot drop
// a refresh token, a JWT or a display name it wasn't thinking about.
func (t TokenSet) Credential() Credential {
	return Credential{
		Token:       t.AuthToken,
		WorkspaceID: t.WorkspaceID,
		Username:    t.Username,
		Expiry:      t.PATExpiry,
	}
}

// Origin pairs the backend the record was read from with the provenance the record
// itself implies.
func (t TokenSet) Origin(backend Backend) Origin {
	provenance := ProvenanceManual
	if t.IsOAuthManaged() {
		provenance = ProvenanceOAuthLogin
	}

	return Origin{Backend: backend, Provenance: provenance}
}

func (t TokenSet) Populated() bool {
	return t.AuthToken != "" && t.WorkspaceID != ""
}
