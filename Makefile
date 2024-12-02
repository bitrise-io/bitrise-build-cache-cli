# Tool versions
GOLANGCI_LINT_VERSION = v1.60.0

.PHONY: lint
lint:					## Runs golangci-lint
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout 5m

.PHONY: govulncheck
govulncheck:				## Runs govulncheck
	go run golang.org/x/vuln/cmd/govulncheck@latest -test ./...

.PHONY: test-unit
test-unit:				## Runs all tests
	go test -tags unit ./...

protoc:
	protoc -I=./proto --go_out=./proto --go_opt=paths=source_relative --go-grpc_out=./proto --go-grpc_opt=paths=source_relative proto/kv_storage/kv_storage.proto
