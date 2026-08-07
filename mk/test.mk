## test targets

POSTGRES_VERSION ?= 17

.PHONY: test-unit
test-unit: ## Run unit tests (no containers required)
	cd backend && go test ./internal/...

.PHONY: test-integration
test-integration: ## Run integration tests (requires docker). Use POSTGRES_VERSION=14..18
	cd backend && POSTGRES_VERSION=$(POSTGRES_VERSION) go test -tags=integration -v -timeout=10m -coverprofile=coverage-integration.out -coverpkg=./internal/repository/,./internal/query/,./internal/storage/ ./internal/repository/ ./internal/storage/ ./internal/discovery/postgres/
	@cd backend && go tool cover -func=coverage-integration.out | tail -1

.PHONY: test-locales
test-locales: ## Check i18n locale files (key parity, duplicates, placeholders); warns only
	frontend/scripts/check-locales.py

.PHONY: test-helm
test-helm: ## Run Helm chart render tests (requires helm)
	deploy/charts/dasha/tests/render-tests.sh

.PHONY: test-all
test-all: test-unit test-locales test-integration ## Run all tests

.PHONY: test-coverage
test-coverage: ## Run all tests with combined coverage report
	cd backend && go test -coverprofile=coverage-unit.out ./internal/...
	cd backend && POSTGRES_VERSION=$(POSTGRES_VERSION) go test -tags=integration -v -timeout=10m -coverprofile=coverage-integration.out -coverpkg=./internal/repository/,./internal/query/,./internal/storage/ ./internal/repository/ ./internal/storage/ ./internal/discovery/postgres/
	@echo "=== Unit test coverage ==="
	@cd backend && go tool cover -func=coverage-unit.out | tail -1
	@echo "=== Integration test coverage ==="
	@cd backend && go tool cover -func=coverage-integration.out | tail -1
