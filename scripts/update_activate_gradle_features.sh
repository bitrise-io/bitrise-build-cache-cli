#!/bin/bash

set -ex

go get github.com/bitrise-io/bitrise-build-cache-cli/v2@${BITRISE_GIT_TAG}
go mod tidy
go mod vendor