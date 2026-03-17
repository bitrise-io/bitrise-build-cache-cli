#!/usr/bin/env bash
set -exo pipefail

mkdir build
cd build
cmake -D CMAKE_BUILD_TYPE=Release ..
make
make install
