.PHONY: demo-lab demo-lab-down demo-lab-logs demo-lab-restart

demo-lab: ## Start demo lab (builds from source, http://localhost:3000)
	docker compose -f demo/docker-compose.yaml up --build -d
	@echo ""
	@echo "Demo lab started: http://localhost:3000"
	@echo "  Logs: make demo-lab-logs"
	@echo "  Stop: make demo-lab-down"

demo-lab-down: ## Stop demo lab and remove volumes
	docker compose -f demo/docker-compose.yaml down -v

demo-lab-logs: ## Show demo lab logs (follow)
	docker compose -f demo/docker-compose.yaml logs -f

demo-lab-restart: ## Rebuild and restart demo lab
	docker compose -f demo/docker-compose.yaml down -v
	docker compose -f demo/docker-compose.yaml up --build -d
