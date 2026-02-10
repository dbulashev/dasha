MODERNIZE_CMD = go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@v0.18.1

.PHONY: lint-go
lint-go: ## Lint backend (revive + gosec)
	cd backend && REVIVE_FORCE_COLOR=1 revive -config revive.toml -formatter friendly -exclude ./gen/... ./...
	cd backend && gosec -quiet -exclude-dir=gen ./...

.PHONY: lint-vue
lint-vue: ## Lint frontend (eslint)
	cd frontend && npm run lint

.PHONY: modernize
modernize: modernize-fix ## Run gopls modernize check and fix

.PHONY: modernize-fix
modernize-fix: ## Run gopls modernize fix
	@echo "Running gopls modernize with -fix..."
	cd backend && $(MODERNIZE_CMD) -test -fix ./...

.PHONY: modernize-check
modernize-check: ## Run gopls modernize only check
	@echo "Checking if code needs modernization..."
	cd backend && $(MODERNIZE_CMD) -test ./...
