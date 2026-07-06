.PHONY: demo-lab demo-lab-down demo-lab-logs demo-lab-restart

# Base demo plus the metrics-backed overlay (VictoriaMetrics + pgSCV + pgbouncer).
DEMO_COMPOSE = docker compose -f demo/docker-compose.yaml -f demo/docker-compose.metrics.yaml

demo-lab: ## Start demo lab (builds from source, http://localhost:3000, VM at :8428)
	$(DEMO_COMPOSE) up --build -d
	@echo ""
	@echo "Demo lab started: http://localhost:3000"
	@echo "  VictoriaMetrics: http://localhost:8428/vmui"
	@echo "  Logs: make demo-lab-logs"
	@echo "  Stop: make demo-lab-down"

demo-lab-down: ## Stop demo lab and remove volumes
	$(DEMO_COMPOSE) down -v

demo-lab-logs: ## Show demo lab logs (follow)
	$(DEMO_COMPOSE) logs -f

demo-lab-restart: ## Rebuild and restart demo lab
	$(DEMO_COMPOSE) down -v
	$(DEMO_COMPOSE) up --build -d
