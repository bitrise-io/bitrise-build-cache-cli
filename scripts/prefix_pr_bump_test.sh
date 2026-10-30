#!/usr/bin/env bash
# Tests scripts/prefix_pr_bump.sh against local bare repos with stubbed gh/curl (no network).
set -u
SCRIPT="${1:-$(cd "$(dirname "$0")" && pwd)/prefix_pr_bump.sh}"
PIN=systems/instance-management/nomad_startup_script_prefix_build_cache.sh
T="$(mktemp -d)"; trap 'rm -rf "$T"' EXIT; mkdir -p "$T/bin"
SRC="$T/pin.sh"
cat > "$SRC" <<'PINFILE'
BITRISE_BUILD_CACHE_CLI_VERSION="1.0.0"
BITRISE_BUILD_CACHE_CLI_LINUX_AMD64_SHASUM="0000"
BITRISE_BUILD_CACHE_CLI_DARWIN_ARM64_SHASUM="1111"
echo "install and activate"
PINFILE
mkseed() { # $1=dir $2=with-pin(1/0)
  git init -q -b staging "$1"; (cd "$1"; git config user.email t@t; git config user.name t; mkdir -p "$(dirname $PIN)"
  if [ "$2" = 1 ]; then cp "$SRC" "$PIN"; else echo x > other.txt; fi
  git add -A; git commit -qm seed; git branch production)
}
mkdir -p "$T/seed1" "$T/seed0"; mkseed "$T/seed1" 1; mkseed "$T/seed0" 0
git clone -q --bare "$T/seed1" "$T/remote.git"
cat > "$T/bin/curl" <<'C'
#!/bin/sh
while [ $# -gt 0 ]; do case "$1" in -o) out="$2"; shift 2;; -*) shift;; *) shift;; esac; done
printf 'aaaa1111  bitrise-build-cache_9.9.9_linux_amd64.tar.gz\nbbbb2222  bitrise-build-cache_9.9.9_darwin_arm64.tar.gz\n' > "$out"
C
cat > "$T/bin/gh" <<'G'
#!/bin/sh
echo "gh $*" >> "$GH_LOG"
[ "$1 $2" = "pr checks" ] && [ -n "${CHECKS_FAIL:-}" ] && [ "${3:-}" != "" ] && case "$*" in *--watch*) exit 1;; esac
exit 0
G
# the script targets GNU sed (-i -E); shim it on macOS
cat > "$T/bin/sed" <<'S'
#!/bin/sh
if [ "$1" = "-i" ] && [ "$2" = "-E" ] && [ "$(uname)" = Darwin ]; then shift 2; exec /usr/bin/sed -E -i '' "$@"; fi
exec /usr/bin/sed "$@"
S
printf '#!/bin/sh\nshift; exec "$@"\n' > "$T/bin/timeout"
chmod +x "$T/bin/"*
fail=0; check() { if eval "$2"; then echo "  ok   $1"; else echo "  FAIL $1"; fail=1; fi; }
run() { env PATH="$T/bin:$PATH" GH_LOG="$T/gh.log" BITRISE_GIT_TAG=v9.9.9 IPS_BOT_TOKEN=tok IPS_GIT_URL="$T/remote.git" "$@" bash "$SCRIPT" 2>&1; }
show() { git --git-dir="$T/remote.git" show "$1:$PIN"; }

echo "== bump staging then production"
out="$(run)"; 
check "PRs created for both bases in order" "[ \"\$(grep -c '^gh pr create' $T/gh.log)\" = 2 ] && grep '^gh pr create' $T/gh.log | head -1 | grep -q -- '--base staging' && grep '^gh pr create' $T/gh.log | tail -1 | grep -q -- '--base production'"
check "both PRs merged after checks" "[ \"\$(grep -c '^gh pr merge' $T/gh.log)\" = 2 ] && grep -q 'pr checks .*--watch' $T/gh.log"
check "pushed branches carry the bump" "for b in chore/bump-bitrise-build-cache-cli-9.9.9-staging chore/bump-bitrise-build-cache-cli-9.9.9-production; do git --git-dir=$T/remote.git show \$b:$PIN | grep -q 'CLI_VERSION=\"9.9.9\"' && git --git-dir=$T/remote.git show \$b:$PIN | grep -q 'LINUX_AMD64_SHASUM=\"aaaa1111\"' && git --git-dir=$T/remote.git show \$b:$PIN | grep -q 'DARWIN_ARM64_SHASUM=\"bbbb2222\"' || exit 1; done"
check "only the three pinned lines changed" "[ \"\$(git --git-dir=$T/remote.git diff --numstat staging chore/bump-bitrise-build-cache-cli-9.9.9-staging)\" = \"\$(printf '3\t3\t%s' $PIN)\" ]"

echo "== a base already at the tag is skipped"
rm -f "$T/gh.log"; (cd "$T/seed1" && :) ; git --git-dir="$T/remote.git" update-ref refs/heads/staging chore-dummy 2>/dev/null
git --git-dir="$T/remote.git" update-ref refs/heads/staging "$(git --git-dir="$T/remote.git" rev-parse chore/bump-bitrise-build-cache-cli-9.9.9-staging)"
git --git-dir="$T/remote.git" update-ref refs/heads/production "$(git --git-dir="$T/remote.git" rev-parse chore/bump-bitrise-build-cache-cli-9.9.9-production)"
out="$(run)"
check "no PR when both are at the tag" "[ ! -s $T/gh.log ] && echo \"\$out\" | grep -q 'already at 9.9.9'"

echo "== checks fail: PR is not merged"
git clone -q --bare "$T/seed1" "$T/remote2.git"; rm -f "$T/gh.log"
out="$(env PATH="$T/bin:$PATH" GH_LOG="$T/gh.log" CHECKS_FAIL=1 BITRISE_GIT_TAG=v9.9.9 IPS_BOT_TOKEN=tok IPS_GIT_URL="$T/remote2.git" bash "$SCRIPT" 2>&1)"; rc=$?
check "non-zero exit and no merge" "[ $rc -ne 0 ] && ! grep -q '^gh pr merge' $T/gh.log && [ \"\$(grep -c '^gh pr create' $T/gh.log)\" = 1 ]"

echo "== a branch without the pin file is skipped"
git clone -q --bare "$T/seed0" "$T/remote3.git"; rm -f "$T/gh.log"
out="$(env PATH="$T/bin:$PATH" GH_LOG="$T/gh.log" BITRISE_GIT_TAG=v9.9.9 IPS_BOT_TOKEN=tok IPS_GIT_URL="$T/remote3.git" bash "$SCRIPT" 2>&1)"; rc=$?
check "exit 0, no PR" "[ $rc -eq 0 ] && [ ! -s $T/gh.log ] && echo \"\$out\" | grep -q 'not rolled out'"

echo "== unexpected extra change in the pin file is refused"
git clone -q --bare "$T/seed1" "$T/remote4.git"; rm -f "$T/gh.log"
sed2="$T/bin/sed"; cat > "$sed2" <<'S'
#!/bin/sh
if [ "$1" = "-i" ] && [ "$2" = "-E" ]; then shift 2; if [ "$(uname)" = Darwin ]; then /usr/bin/sed -E -i '' "$@"; else /usr/bin/sed -E -i "$@"; fi; for last; do :; done; echo "# stray" >> "$last"; exit 0; fi
exec /usr/bin/sed "$@"
S
out="$(env PATH="$T/bin:$PATH" GH_LOG="$T/gh.log" BITRISE_GIT_TAG=v9.9.9 IPS_BOT_TOKEN=tok IPS_GIT_URL="$T/remote4.git" bash "$SCRIPT" 2>&1)"; rc=$?
check "refuses and opens no PR" "[ $rc -ne 0 ] && [ ! -s $T/gh.log ] && echo \"\$out\" | grep -q 'Unexpected diff'"
exit $fail
