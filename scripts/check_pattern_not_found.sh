#!/bin/bash
set -eo pipefail

# Usage: check_pattern_not_found.sh <file_path> <pattern1> <pattern2> [<pattern3> ...]
# Returns success if pattern is NOT found in file, fails if pattern IS found.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHECK_PATTERN_SH="$SCRIPT_DIR/check_pattern.sh"

if "$CHECK_PATTERN_SH" "$@"; then
  echo "Pattern found."
  exit 1
else
  echo "Pattern not found."
  exit 0
fi
