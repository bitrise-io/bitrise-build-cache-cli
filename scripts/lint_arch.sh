#!/usr/bin/env bash
#
# Enforces the auth package layering documented in docs/auth.md.
#
# Invariants 1 and 4 — the two *import* rules — are enforced by depguard in
# .golangci.yaml, which also surfaces them in editors. This script covers the two
# *symbol visibility* rules, which depguard cannot express.
#
# ADDING AN EXCEPTION: every entry below carries a reason, and the bar is "this
# code owns a layer beneath live for something live cannot serve" — auditing every
# source independently, writing a credential, or testing the keyring itself. It is
# not "resolving through live was awkward here". If the reason does not fit on one
# line, the code probably belongs behind live.

set -uo pipefail

fail=0

# escape_re quotes BRE metacharacters. Parentheses are literal in BRE — escaping
# them would open a group and make grep fail, which silently empties the hit list
# and turns the rule into a no-op.
escape_re() {
	printf '%s' "$1" | sed 's/[.[\*^$]/\\&/g'
}

report() {
	fail=1
	printf '\033[31mlint-arch: %s\033[0m\n' "$1"
	printf '%s\n' "$2" | sed 's/^/  /'
	printf '  → %s\n' "$3"
}

# ─────────────────────────────────────────────────────────────────────────────
# Invariant 2 — auth.TokenSet stays at or below L4.
#
# Consumers take auth.Credential. TokenSet carries the OAuth refresh machinery,
# and a package that never persists a login has no business holding one.
# ─────────────────────────────────────────────────────────────────────────────

tokenset_allowed=(
	'^internal/auth/'                   # L0-L4: the type's own tree
	'^internal/config/multiplatform/'   # L1: the file backend serialises it
	'^cmd/auth/'                        # auth login/set/clear write whole records; auth status audits them
	'^cmd/common/interactive/'          # the wizard completes a sign-in and stores the result
	'^internal/authprompt/'             # the doctor's auth fixer writes the credential the user typed
	'_test\.go$'                        # tests construct records to drive the layers below
)

# git ls-files, not `grep -r .`, which also descends into nested git worktrees and
# vendored copies where these prefixes do not match.
hits=$(git ls-files '*.go' | xargs grep -ln 'auth\.TokenSet\|authpkg\.TokenSet' 2>/dev/null || true)
for allow in "${tokenset_allowed[@]}"; do
	hits=$(printf '%s\n' "$hits" | grep -v "$allow" || true)
done
[ -n "$hits" ] && report \
	"auth.TokenSet must not appear above L4 — consumers take auth.Credential" \
	"$hits" \
	"see 'The two credential types' in docs/auth.md"

# ─────────────────────────────────────────────────────────────────────────────
# Invariant 3 — L5 consumers resolve through internal/auth/live.
#
# Reaching a backend directly reintroduces the second precedence order this
# layering exists to prevent.
#
# Known blind spot: this cannot see a credential read off a *config struct*
# rather than a store. pkg/common and pkg/reactnative did exactly that until
# ACI-5274, and the grep was silent about it. That one is on review.
# ─────────────────────────────────────────────────────────────────────────────

direct_backend='keychain\.New(\|keychain\.NewBackend(\|store\.NewKeychain(\|store\.NewFile(\|EnsureFreshFrom('

# Whole surfaces that legitimately own a backend.
backend_allowed_paths=(
	# path prefix                # why it may reach a backend directly
	'cmd/auth/'                  # auth status must audit every source independently — the one job live cannot do, since it has to report the keychain entry an exported env var is shadowing
	'cmd/common/interactive/'    # the sign-in flow picks its backend (--storage) and writes the completed login to it
	'internal/authprompt/'       # the doctor's auth fixer writes the credential the user just typed
)

# Single allowed calls, rather than excusing a whole package.
backend_allowed_calls=(
	# <path>:<symbol>                                  # why this one call is allowed
	'internal/doctor/doctor.go:keychain.NewBackend('   # wires the raw keyring for the keychain-smoke check, which writes a nonce under its own service/account to prove the keyring works. It never reads a credential, and going through live would test resolution instead of the thing being diagnosed.
)

hits=$(grep -rn "$direct_backend" \
	--include='*.go' cmd/ pkg/ internal/config/ internal/doctor/ internal/bazelcredhelper/ 2>/dev/null |
	grep -v '_test\.go:' || true)

for prefix in "${backend_allowed_paths[@]}"; do
	hits=$(printf '%s\n' "$hits" | grep -v "^${prefix}" || true)
done
for spec in "${backend_allowed_calls[@]}"; do
	path=${spec%%:*}
	symbol=${spec#*:}
	# Drop only lines that are both in that file and that exact call. grep exits 1
	# on "no lines left", which is fine; exit >1 is a broken pattern and must not
	# be swallowed, or the rule quietly stops checking anything.
	hits=$(printf '%s\n' "$hits" | grep -v "^${path}:[0-9]*:.*$(escape_re "$symbol")")
	[ $? -gt 1 ] && { echo "lint-arch: bad exclusion pattern for ${spec}" >&2; exit 2; }
done

[ -n "$hits" ] && report \
	"consumers must resolve through internal/auth/live, not keychain/store/oauth" \
	"$hits" \
	"see 'Adding to this' in docs/auth.md"

if [ "$fail" -ne 0 ]; then
	echo
	echo "See docs/auth.md for the layering these rules protect."
	exit 1
fi

echo "lint-arch: auth layering OK"
