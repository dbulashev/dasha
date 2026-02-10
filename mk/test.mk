## test targets

POSTGRES_VERSION ?= 17

.PHONY: test-unit
test-unit: ## Run unit tests (no containers required)
	cd backend && go test ./internal/...

.PHONY: test-integration
test-integration: ## Run integration tests (requires docker). Use POSTGRES_VERSION=14..18
	cd backend && POSTGRES_VERSION=$(POSTGRES_VERSION) go test -tags=integration -v -timeout=5m -coverprofile=coverage-integration.out -coverpkg=./internal/repository/,./internal/query/ ./internal/repository/
	@cd backend && go tool cover -func=coverage-integration.out | tail -1

.PHONY: test-all
test-all: test-unit test-integration ## Run all tests

.PHONY: test-coverage
test-coverage: ## Run all tests with combined coverage report
	cd backend && go test -coverprofile=coverage-unit.out ./internal/...
	cd backend && POSTGRES_VERSION=$(POSTGRES_VERSION) go test -tags=integration -v -timeout=5m -coverprofile=coverage-integration.out -coverpkg=./internal/repository/,./internal/query/ ./internal/repository/
	@echo "=== Unit test coverage ==="
	@cd backend && go tool cover -func=coverage-unit.out | tail -1
	@echo "=== Integration test coverage ==="
	@cd backend && go tool cover -func=coverage-integration.out | tail -1
