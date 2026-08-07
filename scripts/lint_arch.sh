#!/usr/bin/env bash
# Enforces the auth package layering documented in docs/auth.md.
set -uo pipefail

MOD="github.com/bitrise-io/bitrise-build-cache-cli/v3"
fail=0

report() {
	fail=1
	printf '\033[31mlint-arch: %s\033[0m\n' "$1"
	printf '%s\n' "$2" | sed 's/^/  /'
}

# 1. internal/auth (L0) is a leaf.
hits=$(grep -rn "\"$MOD/" internal/auth/*.go 2>/dev/null || true)
[ -n "$hits" ] && report "internal/auth must import no internal package (it is the L0 leaf)" "$hits"

# 2. auth.TokenSet stays at or below L4.
hits=$(grep -rln 'auth\.TokenSet' --include='*.go' . 2>/dev/null |
	grep -v '^./internal/auth/' |
	grep -v '^./internal/config/multiplatform/' || true)
[ -n "$hits" ] && report "auth.TokenSet must not appear above L4 — consumers take auth.Credential" "$hits"

# 3. No auth package depends on config/common.
hits=$(grep -rn "\"$MOD/internal/config/common\"" internal/auth/ 2>/dev/null || true)
[ -n "$hits" ] && report "no package under internal/auth may import internal/config/common" "$hits"

# 4. L5 consumers resolve through live, not the layers underneath. login/logout/clear
#    legitimately talk to the store and to oauth directly.
hits=$(grep -rn 'keychain\.New(\|store\.NewKeychain(\|store\.NewFile(\|EnsureFreshFrom(' \
	--include='*.go' cmd/ pkg/ internal/config/ internal/doctor/ internal/bazelcredhelper/ 2>/dev/null |
	grep -v '_test\.go:' |
	grep -v '^cmd/auth/auth\.go:' |
	grep -v '^cmd/common/interactive/login\.go:' || true)
[ -n "$hits" ] && report "consumers must resolve through internal/auth/live, not keychain/store/oauth" "$hits"

if [ "$fail" -ne 0 ]; then
	echo
	echo "See docs/auth.md for the layering these rules protect."
	exit 1
fi

echo "lint-arch: auth layering OK"
