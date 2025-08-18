# Tool versions
GOLANGCI_LINT_VERSION = v2.4.0

.PHONY: lint
lint:					## Runs golangci-lint
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout 5m

.PHONY: lint-fix
lint-fix:					## Runs golangci-lint
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout 5m --fix

.PHONY: govulncheck
govulncheck:				## Runs govulncheck
	go run golang.org/x/vuln/cmd/govulncheck@latest -test ./...

.PHONY: test-unit
test-unit:				## Runs all tests
	go test -tags unit -race ./...

.PHONY: protoc
protoc:
	protoc -I=./proto --go_out=./proto --go_opt=paths=source_relative --go-grpc_out=./proto --go-grpc_opt=paths=source_relative proto/build/bazel/remote/execution/v2/remote_execution.proto
	protoc -I=./proto --go_out=./proto --go_opt=paths=source_relative --go-grpc_out=./proto --go-grpc_opt=paths=source_relative proto/kv_storage/kv_storage.proto
	protoc -I=./proto --go_out=./proto --go_opt=paths=source_relative --go-grpc_out=./proto --go-grpc_opt=paths=source_relative proto/llvm/cas/compilation_caching_cas.proto
	protoc -I=./proto --go_out=./proto --go_opt=paths=source_relative --go-grpc_out=./proto --go-grpc_opt=paths=source_relative proto/llvm/kv/compilation_caching_kv.proto
	protoc -I=./proto --go_out=./proto --go_opt=paths=source_relative --go-grpc_out=./proto --go-grpc_opt=paths=source_relative proto/llvm/session/session.proto

run-xcelerate-proxy:
	# set token if you run against staging or production
	# REMOTE_CACHE_TOKEN=
	# if you want to run the proxy against production, remove `BITRISE_BUILD_CACHE_ENDPOINT`, it will default to production

	BITRISE_IO="true" \
	BITRISE_XCELERATE_SOCKET_PATH=/tmp/xcelerate-proxy.sock \
	INVOCATION_ID=$(shell uuidgen | tr '[:upper:]' '[:lower:]') \
	BITRISE_BUILD_CACHE_WORKSPACE_ID=322a005426441b60 \
	BITRISE_APP_SLUG=fa115466-5993-4b2d-b00a-c82ab6a63fe5 \
	BITRISE_BUILD_SLUG=$(shell uuidgen | tr '[:upper:]' '[:lower:]') \
	BITRISE_STEP_EXECUTION_ID=$(shell uuidgen | tr '[:upper:]' '[:lower:]') \
	BITRISE_BUILD_CACHE_ENDPOINT=grpc://localhost:6666 \
	go run main.go xcelerate-proxy
