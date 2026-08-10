# Deployment

[Русская версия](../ru/deployment.md) · [← README](../../README.md)

## Docker Compose

The simplest way to run Dasha with pre-built images:

```bash
cd deploy/compose
# Edit dasha.yaml with your cluster settings
docker compose up -d
# Open http://localhost:3000
```

## Docker Images

Multi-architecture images (`linux/amd64`, `linux/arm64`) are published to Docker Hub on every release:

| Image | Description |
|-------|-------------|
| `dbulashev/dasha-backend` | Go API server |
| `dbulashev/dasha-frontend` | Nginx + Vue SPA, proxies `/api/` to backend |
| `dbulashev/dasha-mcp` | MCP connector for AI assistants (stdio / HTTP) |

The frontend accepts `BACKEND_URL` environment variable (default: `backend:8000`).

## Helm Chart

The chart is published as an OCI artifact to GitHub Container Registry:

```bash
helm install dasha oci://ghcr.io/dbulashev/charts/dasha --version 0.1.5
```

### Minimal values (static clusters)

```yaml
config:
  clusters:
    - name: production
      username: monitoring_user
      password_from_env: PG_PASSWORD
      databases: [myapp]
      hosts: [pg-master.example.com]

secrets:
  existingSecret: my-pg-credentials  # must contain PG_PASSWORD key
```

### With ESO (External Secrets Operator)

```yaml
config:
  clusters:
    - name: production
      username: monitoring_user
      password_from_env: PG_PASSWORD
      databases: [myapp]
      hosts: [pg-master.example.com]

secrets:
  externalSecret:
    enabled: true
    refreshInterval: "1m"
    secretStoreRef:
      name: vault-backend
      kind: ClusterSecretStore
    data:
      - secretKey: PG_PASSWORD
        remoteRef:
          key: dasha/production
          property: password
```

### Extra environment variables (`extraEnv` / `extraEnvFrom`)

The `secrets.` block feeds a single Secret to the pod via `envFrom`, so variable names must equal that Secret's key names. When the Secret is owned by someone else — Dex, an operator, a neighbouring chart — its keys are named their way, and `envFrom` cannot rename them. `backend.extraEnv` names each variable explicitly and points at one key, which does the renaming:

```yaml
config:
  auth:
    mode: oidc
    oidc:
      issuer_url: "https://dex.example.com"
      client_id: dasha
      client_secret_from_env: DASHA_OIDC_SECRET

backend:
  extraEnv:
    - name: DASHA_OIDC_SECRET     # variable the config expects
      valueFrom:
        secretKeyRef:
          name: dasha-oidc-secret
          key: clientSecret       # key Dex actually created
```

`backend.extraEnvFrom` covers the other gap — pulling in sources beyond the chart's own env Secret (`secrets.existingSecret`, or the one `secrets.externalSecret` creates), which is limited to one. It imports a Secret/ConfigMap whole, with variable names equal to key names, so every key must already be a valid environment variable name — Kubernetes silently skips the ones that are not, so a `client-secret` key never arrives while `clientSecret` does:

```yaml
backend:
  extraEnvFrom:
    - secretRef:
        name: dasha-db-passwords
```

Both exist on `autosnapshot` as well; the daemon reads the same config, so it needs the same variables. On a name collision `extraEnv` wins over anything from `envFrom`, and within `envFrom` later sources win over earlier ones — the chart's own Secret is listed first, so `extraEnvFrom` overrides it.

### With Yandex MDB service discovery

```yaml
config:
  discovery:
    yandex_mdb_prod:
      type: yandex-mdb
      config:
        authorized_key: /secrets/prod/authorized_key.json
        folder_id: "b1g..."
        user: monitoring_user
        password_from_env: DISCOVERY_PROD_PASSWORD
        refresh_interval: 5
        clusters:
          - name: ".*"

secrets:
  externalSecret:
    enabled: true
    refreshInterval: "1m"
    secretStoreRef:
      name: vault-backend
      kind: ClusterSecretStore
    data:
      - secretKey: DISCOVERY_PROD_PASSWORD
        remoteRef:
          key: dasha/discovery
          property: password

cloudSAKeys:
  - name: prod
    mountPath: /secrets/prod
    externalSecret:
      enabled: true
      refreshInterval: "1m"
      secretStoreRef:
        name: vault-backend
        kind: ClusterSecretStore
      remoteRef:
        key: dasha/discovery
        property: sa_cloud_auth_key
```

### Ingress with TLS (cert-manager)

```yaml
ingress:
  enabled: true
  className: nginx
  domain: dasha.example.com
  tls:
    enabled: true
    certManager:
      enabled: true
      issuer: cluster-issuer
```

cert-manager will create a `Certificate` resource in the application namespace.

### Gateway API with TLS (cert-manager)

Portable alternative to Ingress — works with any Gateway API implementation (Istio, NGINX Gateway Fabric, Envoy Gateway, Cilium):

```yaml
gatewayAPI:
  enabled: true
  gatewayClassName: istio
  hostname: dasha.example.com
  # When the Gateway lives in a controller-specific namespace (e.g. istio-system),
  # set gatewayNamespace accordingly — Certificate is created in the same namespace.
  # gatewayNamespace: istio-system
  tls:
    enabled: true
    certManager:
      enabled: true
      issuer: cluster-issuer
```

The cert-manager `Certificate` is created in the Gateway's namespace (`gatewayNamespace`, defaults to the release namespace). Cross-namespace secret refs would require a `ReferenceGrant`, which the chart does not render — keeping Certificate and Gateway colocated avoids that.

Rendered resources (all conditional on `gatewayAPI.enabled: true`):
- `Gateway` — only when `gatewayAPI.createGateway: true` (default); HTTP listener always, HTTPS listener only when `gatewayAPI.tls.enabled: true`.
- `HTTPRoute` (main) — attached to the HTTPS listener when `tls.enabled`, otherwise to the HTTP listener.
- `HTTPRoute` (HTTP→HTTPS redirect, `RequestRedirect` filter) — only when `gatewayAPI.tls.enabled && gatewayAPI.tls.redirect`, and on an existing Gateway additionally when `existingGateway.redirectSectionName` is set.
- `Certificate` (cert-manager) — only when `gatewayAPI.tls.certManager.enabled` and the chart owns the Gateway.

`ingress.enabled` and `gatewayAPI.enabled` are mutually exclusive — `helm template` fails if both are true.

### HTTPRoute on an existing Gateway

When the cluster already has a shared Gateway, managed outside this chart together with its certificates, set `createGateway: false` — the chart then renders only the `HTTPRoute` and attaches it to that Gateway:

```yaml
gatewayAPI:
  enabled: true
  hostname: dasha.example.com
  createGateway: false
  existingGateway:
    name: shared-gateway
    namespace: istio-system
    # Listener to attach to — name the HTTPS one when tls.enabled.
    sectionName: https
    # Set only if you also want the chart to render the HTTP→HTTPS redirect route.
    # redirectSectionName: http
  tls:
    enabled: true
```

Notes:
- `existingGateway.name` is required; `helm template` fails without it.
- `gatewayClassName`, `allowedRoutes` and `tls.certManager` describe the Gateway and are ignored in this mode — its listeners, TLS certificate and route-attachment policy belong to its owner. `tls.enabled` still matters: it tells the chart the endpoint is HTTPS (`auth.require_https` in the rendered config) and gates the redirect route.
- Leaving `sectionName` empty attaches the route to every compatible listener of that Gateway — a plain HTTP one included, which then serves Dasha in cleartext. With `tls.enabled: true` the rendered config also gets `auth.require_https`, so sessions arriving over that HTTP listener cannot log in at all. Name the HTTPS listener explicitly.
- Attachment across namespaces has to be permitted by that Gateway's own `allowedRoutes` (`from: All` or a `Selector` matching the release namespace) — the chart cannot check this, an unattached route shows up as `Accepted=False` on the `HTTPRoute`.
- The redirect route is rendered only when `existingGateway.redirectSectionName` names the HTTP listener: without a `sectionName` it would attach to the HTTPS listener too and redirect it onto itself. It also requires `existingGateway.sectionName` to be set, and to name a different listener — otherwise both routes land on one listener and the main route wins over the redirect (or the redirect loops onto itself); `helm template` fails on those combinations.

### API-only mode (without frontend)

```yaml
frontend:
  enabled: false

ingress:
  enabled: true
  domain: dasha-api.example.com
```

### Key chart features

- **Config as ConfigMap** — `dasha.yaml` rendered from values, no passwords inline
- **Passwords via env** — `password_from_env` + ESO or existing Kubernetes Secret; `extraEnv` / `extraEnvFrom` for Secrets managed outside the chart
- **Cloud SA keys** — per-folder `authorized_key.json` via ESO or existing Secret
- **Frontend optional** — deploy backend only for API access
- **Ingress / Gateway API** — single `/` rule routes to frontend (which proxies `/api/` and `/auth/` to backend); auto HTTP→HTTPS redirect when TLS is enabled; cert-manager support; mutually exclusive `gatewayAPI.enabled` for K8s Gateway API (`gateway.networking.k8s.io/v1`)
- **Security** — `podSecurityContext`, `securityContext`, separate settings for frontend/backend

