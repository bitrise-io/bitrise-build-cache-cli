# Tool versions
GOLANGCI_LINT_VERSION = v2.3.1

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
	go test -tags unit ./...

.PHONY: protoc
protoc:
	protoc -I=./proto --go_out=./proto --go_opt=paths=source_relative --go-grpc_out=./proto --go-grpc_opt=paths=source_relative proto/build/bazel/remote/execution/v2/remote_execution.proto
	protoc -I=./proto --go_out=./proto --go_opt=paths=source_relative --go-grpc_out=./proto --go-grpc_opt=paths=source_relative proto/kv_storage/kv_storage.proto
	protoc -I=./proto --go_out=./proto --go_opt=paths=source_relative --go-grpc_out=./proto --go-grpc_opt=paths=source_relative proto/llvm/cas/compilation_caching_cas.proto
	protoc -I=./proto --go_out=./proto --go_opt=paths=source_relative --go-grpc_out=./proto --go-grpc_opt=paths=source_relative proto/llvm/kv/compilation_caching_kv.proto
	protoc -I=./proto --go_out=./proto --go_opt=paths=source_relative --go-grpc_out=./proto --go-grpc_opt=paths=source_relative proto/llvm/session/session.proto
