.PHONY: run-backend run-frontend

run-backend:	## Run backend (serves on :8000)
	cd backend && go run cmd/main.go

run-frontend:	## Run frontend dev server (proxies /api to localhost:8000)
	cd frontend && npm run dev
