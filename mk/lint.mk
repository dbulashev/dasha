MODERNIZE_CMD = go run golang.org/x/tools/go/analysis/passes/modernize/cmd/modernize@v0.52.0
# modernize has no exclude flag, so generated packages are filtered out of the package list.
MODERNIZE_PKGS = $$(go list ./... | grep -v '/gen/')

.PHONY: lint-go
lint-go: ## Lint backend (revive + gosec)
	cd backend && REVIVE_FORCE_COLOR=1 revive -config revive.toml -formatter friendly -exclude ./gen/... ./...
	cd backend && gosec -quiet -exclude-dir=gen ./...

# Not part of lint-go: the architecture rules are not enforced yet.
.PHONY: lint-arch
lint-arch: ## Check package dependencies against backend/.go-arch-lint.yml
	cd backend && go-arch-lint check

.PHONY: lint-vue
lint-vue: ## Lint frontend (eslint)
	cd frontend && npm run lint

.PHONY: modernize
modernize: modernize-fix ## Run gopls modernize check and fix

.PHONY: modernize-fix
modernize-fix: ## Run gopls modernize fix
	@echo "Running gopls modernize with -fix..."
	cd backend && $(MODERNIZE_CMD) -test -fix $(MODERNIZE_PKGS)

.PHONY: modernize-check
modernize-check: ## Run gopls modernize only check
	@echo "Checking if code needs modernization..."
	cd backend && $(MODERNIZE_CMD) -test $(MODERNIZE_PKGS)
