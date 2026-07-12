.PHONY: generate

generate: ## generate http-api
	go run backend/cmd/generate-queries/main.go
	go generate ./backend/internal/enums/query.go
	oapi-codegen -config .oapi-codegen.yaml ./doc/swagger.yaml
	mkdir -p backend/gen/apiclient
	oapi-codegen -config .oapi-codegen.client.yaml ./doc/swagger.yaml
	npx orval
