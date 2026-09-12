## test targets

POSTGRES_VERSION ?= 18

# Integration suites by the container they need: PostgreSQL ones run per
# POSTGRES_VERSION, log sources bring their own store and run once.
PG_PKGS := ./internal/repository/ ./internal/storage/ ./internal/discovery/postgres/
PG_COVERPKG := ./internal/repository/,./internal/query/,./internal/storage/
LOGS_PKGS := ./internal/logs/source/opensearch/ ./internal/logs/source/victorialogs/
LOGS_COVERPKG := ./internal/logs/...

.PHONY: test-unit
test-unit: ## Run unit tests (no containers required)
	cd backend && go test ./internal/...

.PHONY: test-integration-pg
test-integration-pg: ## Run PostgreSQL integration tests (requires docker). Use POSTGRES_VERSION=14..18
	cd backend && POSTGRES_VERSION=$(POSTGRES_VERSION) go test -tags=integration -v -timeout=10m -coverprofile=coverage-integration.out -coverpkg=$(PG_COVERPKG) $(PG_PKGS)
	@cd backend && go tool cover -func=coverage-integration.out | tail -1

.PHONY: test-integration-logs
test-integration-logs: ## Run log source integration tests: OpenSearch, VictoriaLogs (requires docker)
	cd backend && go test -tags=integration -v -timeout=15m -coverprofile=coverage-integration-logs.out -coverpkg=$(LOGS_COVERPKG) $(LOGS_PKGS)
	@cd backend && go tool cover -func=coverage-integration-logs.out | tail -1

.PHONY: test-integration
test-integration: test-integration-pg test-integration-logs ## Run every integration suite (requires docker)

.PHONY: test-locales
test-locales: ## Check i18n locale files; fails on duplicates/placeholder drift, warns on missing translations
	frontend/scripts/check-locales.py

.PHONY: test-helm
test-helm: ## Run Helm chart render tests (requires helm)
	deploy/charts/dasha/tests/render-tests.sh

.PHONY: test-all
test-all: test-unit test-locales test-integration ## Run all tests

.PHONY: test-coverage
test-coverage: ## Run all tests with combined coverage report
	cd backend && go test -coverprofile=coverage-unit.out ./internal/...
	cd backend && POSTGRES_VERSION=$(POSTGRES_VERSION) go test -tags=integration -v -timeout=10m -coverprofile=coverage-integration.out -coverpkg=$(PG_COVERPKG) $(PG_PKGS)
	cd backend && go test -tags=integration -v -timeout=15m -coverprofile=coverage-integration-logs.out -coverpkg=$(LOGS_COVERPKG) $(LOGS_PKGS)
	@echo "=== Unit test coverage ==="
	@cd backend && go tool cover -func=coverage-unit.out | tail -1
	@echo "=== PostgreSQL integration coverage ==="
	@cd backend && go tool cover -func=coverage-integration.out | tail -1
	@echo "=== Log source integration coverage ==="
	@cd backend && go tool cover -func=coverage-integration-logs.out | tail -1
