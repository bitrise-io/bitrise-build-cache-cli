package doctor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

const (
	smokeServiceName = "bitrise-build-cache-doctor"
	smokeAccountName = "smoketest"
)

// newSmokeSecret is a per-run nonce so a stale entry from a previous failed Delete can't masquerade as a hit.
func newSmokeSecret() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "smoketest-fallback"
	}

	return "smoketest-" + hex.EncodeToString(b[:])
}

func (d *Doctor) keychainSmokeCheck() Check {
	return Check{
		Name: "keychain-smoke",
		Diagnose: func(_ context.Context) Result {
			secret := newSmokeSecret()

			if err := d.Keyring.Set(smokeServiceName, smokeAccountName, secret); err != nil {
				return Result{
					State:  StateError,
					Detail: "keychain Set failed: " + err.Error() + ". On Linux check that a secret-service backend (e.g. gnome-keyring, KeePassXC) is running.",
				}
			}

			got, err := d.Keyring.Get(smokeServiceName, smokeAccountName)
			if err != nil || got != secret {
				_ = d.Keyring.Delete(smokeServiceName, smokeAccountName)
				if err != nil {
					return Result{State: StateError, Detail: "keychain Get failed: " + err.Error()}
				}

				return Result{State: StateError, Detail: "keychain Get returned mismatched value (stale entry from a previous run with a failed Delete?)"}
			}

			if err := d.Keyring.Delete(smokeServiceName, smokeAccountName); err != nil {
				return Result{State: StateWarn, Detail: "keychain Delete failed: " + err.Error() + ". Set + Get worked; the test entry stays behind."}
			}

			return Result{State: StateOK, Detail: "Set/Get/Delete round-trip OK"}
		},
	}
}
