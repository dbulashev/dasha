.PHONY: demo-lab demo-lab-build demo-lab-logs demo-lab-full demo-lab-full-build demo-lab-full-logs demo-lab-down

# Lite: one PG18 cluster, no login. Full: three clusters, Keycloak, metrics
# (VictoriaMetrics + pgSCV + pgbouncer) and log search (VictoriaLogs + Fluent Bit).
DEMO_LITE_COMPOSE = docker compose -f demo/lite/docker-compose.yaml
DEMO_LITE_BUILD_COMPOSE = $(DEMO_LITE_COMPOSE) -f demo/lite/docker-compose.build.yaml
DEMO_COMPOSE = docker compose -f demo/docker-compose.yaml -f demo/docker-compose.metrics.yaml -f demo/docker-compose.logs.yaml
DEMO_BUILD_COMPOSE = $(DEMO_COMPOSE) -f demo/docker-compose.build.yaml

define demo_lite_banner
	@echo ""
	@echo "Demo lab (lite) started:"
	@echo "  Dasha:  http://localhost:3000  (no login, admin role)"
	@echo "  MCP:    http://localhost:8765  (no token)"
	@echo "          claude mcp add --transport http dasha-demo http://localhost:8765"
	@echo ""
	@echo "  Logs:   make demo-lab-logs"
	@echo "  Stop:   make demo-lab-down"
	@echo "  Full lab (3 clusters, Keycloak, metrics, logs): make demo-lab-full"
endef

define demo_full_banner
	@echo ""
	@echo "Demo lab (full) started:"
	@echo "  Dasha:            http://localhost:3000  (Keycloak: admin/admin, viewer/viewer)"
	@echo "  Keycloak:         http://localhost:8080"
	@echo "  VictoriaMetrics:  http://localhost:8428/vmui"
	@echo "  VictoriaLogs:     http://localhost:9428/select/vmui"
	@echo "  MCP:              http://localhost:8765  (header: Authorization: Bearer demo-api-token-for-testing)"
	@echo "                    claude mcp add --transport http dasha-demo http://localhost:8765 \\"
	@echo "                      --header \"Authorization: Bearer demo-api-token-for-testing\""
	@echo ""
	@echo "  Logs:   make demo-lab-full-logs"
	@echo "  Stop:   make demo-lab-down"
	@grep -qE '^[^#]*[[:space:]]keycloak([[:space:]]|$$)' /etc/hosts 2>/dev/null || { \
		echo ""; \
		echo "  !! Login redirects the browser to http://keycloak:8080, which this machine cannot resolve yet."; \
		echo "     Add it once:  echo '127.0.0.1 keycloak' | sudo tee -a /etc/hosts"; \
		echo "     (Windows: C:\\Windows\\System32\\drivers\\etc\\hosts)"; \
	}
endef

demo-lab: ## Start lite demo lab from released images (DASHA_TAG, default latest; http://localhost:3000)
	@$(DEMO_COMPOSE) down
	$(DEMO_LITE_COMPOSE) up -d
	$(demo_lite_banner)

demo-lab-build: ## Start lite demo lab with Dasha built from source
	@$(DEMO_COMPOSE) down
	$(DEMO_LITE_BUILD_COMPOSE) up --build -d
	$(demo_lite_banner)

demo-lab-logs: ## Show lite demo lab logs (follow)
	$(DEMO_LITE_COMPOSE) logs -f

demo-lab-full: ## Start full demo lab from released images (Keycloak, 3 clusters, metrics, logs)
	@$(DEMO_LITE_COMPOSE) down
	$(DEMO_COMPOSE) up -d
	$(demo_full_banner)

demo-lab-full-build: ## Start full demo lab with Dasha built from source
	@$(DEMO_LITE_COMPOSE) down
	$(DEMO_BUILD_COMPOSE) up --build -d
	$(demo_full_banner)

demo-lab-full-logs: ## Show full demo lab logs (follow)
	$(DEMO_COMPOSE) logs -f

demo-lab-down: ## Stop both demo labs and remove volumes
	$(DEMO_LITE_BUILD_COMPOSE) down -v
	$(DEMO_BUILD_COMPOSE) down -v
