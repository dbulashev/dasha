.PHONY: deps-install

deps-install: ## install project dependencies
	go install github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen@v2.2.0
	go install github.com/mgechev/revive@latest
	go install github.com/securego/gosec/v2/cmd/gosec@latest
	npm install orval -D


deps:
	cd backend && go mod tidy
	cd backend && go mod download
