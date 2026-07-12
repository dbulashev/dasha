.PHONY: run-backend run-frontend run-mcp

run-backend:	## Run backend (serves on :8000)
	cd backend && go run cmd/main.go

run-frontend:	## Run frontend dev server (proxies /api to localhost:8000)
	cd frontend && npm run dev

run-mcp:	## Run MCP server over HTTP (:8765, against backend on :8000); stdio: go run ./cmd/dasha-mcp
	cd backend && go run ./cmd/dasha-mcp --dasha-url http://localhost:8000 --http :8765
