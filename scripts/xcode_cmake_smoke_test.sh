#!/bin/bash
set -euo pipefail

wrapper=${XCODEBUILD_WRAPPER:-$HOME/.bitrise-xcelerate/bin/xcodebuild}
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/xcode-cmake.XXXXXX")
trap 'rm -rf "$work_dir"' EXIT

cat > "$work_dir/CMakeLists.txt" <<'EOF'
cmake_minimum_required(VERSION 3.25)
project(CacheSmoke LANGUAGES C Swift)
add_executable(c-smoke main.c)
add_executable(swift-smoke main.swift)
EOF
cat > "$work_dir/main.c" <<'EOF'
#include <stdio.h>
int main(void) { puts("C cache smoke"); return 0; }
EOF
cat > "$work_dir/main.swift" <<'EOF'
print("Swift cache smoke")
EOF

cmake -G Xcode -S "$work_dir" -B "$work_dir/build" \
  -DCMAKE_MAKE_PROGRAM="$wrapper" \
  -DCMAKE_XCODE_ATTRIBUTE_SWIFT_VERSION=6.0 \
  -DCMAKE_XCODE_ATTRIBUTE_COMPILATION_CACHE_CAS_PATH:STRING=

"$wrapper" -project "$work_dir/build/CacheSmoke.xcodeproj" \
  -target ALL_BUILD -configuration Debug build

test "$("$work_dir/build/Debug/c-smoke")" = "C cache smoke"
test "$("$work_dir/build/Debug/swift-smoke")" = "Swift cache smoke"
printf 'CMake configuration and C/Swift target builds passed.\n'
