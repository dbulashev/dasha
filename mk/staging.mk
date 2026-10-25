.PHONY: demo-lab-staging demo-lab-staging-prebuilt demo-lab-staging-trace demo-lab-staging-explain demo-lab-staging-psql demo-lab-staging-down demo-lab-staging-logs demo-lab-staging-info

# Staging stand for planner traces (demo/staging): PostgreSQL with the
# pathprobe core hooks and extension, independent of the demo lab.
STAGING_COMPOSE = docker compose -f demo/staging/docker-compose.yaml
STAGING_COMPOSE_IMAGES = $(STAGING_COMPOSE) -f demo/staging/docker-compose.images.yaml
# Tag of dbulashev/dasha-{backend,frontend} for demo-lab-staging-prebuilt
DASHA_REGISTRY ?= dbulashev
DASHA_TAG ?= 1.9.2-path-probe-test
STAGING_PSQL = $(STAGING_COMPOSE) exec -T pg-pathprobe psql -X -q -v ON_ERROR_STOP=1 -U demo -d demo -f /path-probe/trace-on.sql
STAGING_SQL = demo/staging/path-probe
STAGING_TRACES = demo/staging/traces
# ANALYZE=0: EXPLAIN without running the query (no actual rows in <name>.plan.json)
ANALYZE ?= 1

# $(call staging_probe,<query file>,<output name>): plan + text report to
# <name>.txt, JSON report to <name>.json, EXPLAIN (FORMAT JSON) to
# <name>.plan.json, with ANALYZE unless ANALYZE=0.  A trailing semicolon on
# the last line of the query file is dropped: pathprobe takes one statement as
# text.
define staging_probe
	@mkdir -p $(STAGING_TRACES)
	$(STAGING_PSQL) -v q="$$(sed -e '$$s/;[[:space:]]*$$//' $(1))" \
		-f /path-probe/probe.sql >$(STAGING_TRACES)/$(2).txt 2>&1
	$(STAGING_PSQL) -v q="$$(sed -e '$$s/;[[:space:]]*$$//' $(1))" -v json=1 \
		-f /path-probe/probe.sql >$(STAGING_TRACES)/$(2).json
	$(STAGING_PSQL) -v q="$$(sed -e '$$s/;[[:space:]]*$$//' $(1))" -v plan=1 \
		$(if $(filter 1,$(ANALYZE)),-v analyze=1) \
		-f /path-probe/probe.sql >$(STAGING_TRACES)/$(2).plan.json
	@echo "  $(STAGING_TRACES)/$(2).txt, $(STAGING_TRACES)/$(2).json, $(STAGING_TRACES)/$(2).plan.json"
endef

demo-lab-staging: ## Start the planner-trace staging (postgres-pathprobe at :5440, Dasha built from this checkout at :3001, no auth) and write the demo traces
	$(STAGING_COMPOSE) pull --ignore-buildable
	$(STAGING_COMPOSE) up -d --build --wait
	@$(MAKE) --no-print-directory demo-lab-staging-trace demo-lab-staging-info

demo-lab-staging-prebuilt: ## Same as demo-lab-staging, Dasha from published images: [DASHA_TAG=1.9.2-path-probe-test] [DASHA_REGISTRY=dbulashev]
	DASHA_REGISTRY=$(DASHA_REGISTRY) DASHA_TAG=$(DASHA_TAG) $(STAGING_COMPOSE_IMAGES) pull
	DASHA_REGISTRY=$(DASHA_REGISTRY) DASHA_TAG=$(DASHA_TAG) $(STAGING_COMPOSE_IMAGES) up -d --no-build --wait
	@$(MAKE) --no-print-directory demo-lab-staging-trace demo-lab-staging-info

demo-lab-staging-info:
	@echo ""
	@echo "Staging started:"
	@echo "  PostgreSQL (pathprobe): postgresql://demo:demo@localhost:5440/demo"
	@echo "  Dasha (no auth):        http://localhost:3001/plan-lifecycle (all interfaces; DASHA_BIND=127.0.0.1 for local only)"
	@echo "  Traces:                 $(STAGING_TRACES)/"
	@echo ""
	@echo "  Probe your own query: make demo-lab-staging-explain Q=path/to/query.sql"
	@echo "  Interactive psql:     make demo-lab-staging-psql, then \\i /path-probe/trace-on.sql"
	@echo "  Stop:                 make demo-lab-staging-down"

demo-lab-staging-trace: ## Rewrite the demo traces (trace-observed, trace-observed-join: .txt, .json, .plan.json)
	$(call staging_probe,$(STAGING_SQL)/query-index.sql,trace-observed)
	$(call staging_probe,$(STAGING_SQL)/query-join.sql,trace-observed-join)

demo-lab-staging-explain: ## Probe the planner for a query file (a bare SELECT, not EXPLAIN): Q=path/to/query.sql [ANALYZE=0]
	@test -n "$(Q)" || { echo "usage: make demo-lab-staging-explain Q=path/to/query.sql [ANALYZE=0]"; exit 2; }
	$(call staging_probe,$(Q),$(basename $(notdir $(Q))))

demo-lab-staging-psql: ## Open psql on the staging (enable full coverage with \i /path-probe/trace-on.sql)
	$(STAGING_COMPOSE) exec pg-pathprobe psql -U demo -d demo

demo-lab-staging-logs: ## Show staging logs (follow)
	$(STAGING_COMPOSE) logs -f

demo-lab-staging-down: ## Stop the staging and remove its volume
	$(STAGING_COMPOSE) down -v
