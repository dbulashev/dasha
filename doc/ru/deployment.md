# Развёртывание

[English version](../en/deployment.md) · [← README](../../README.ru.md)

## Docker Compose

Самый простой способ запустить Dasha с готовыми образами:

```bash
cd deploy/compose
# Отредактируйте dasha.yaml под ваши кластеры
docker compose up -d
# Откройте http://localhost:3000
```

## Docker-образы

Мультиархитектурные образы (`linux/amd64`, `linux/arm64`) публикуются на Docker Hub при каждом релизе:

| Образ | Описание |
|-------|----------|
| `dbulashev/dasha-backend` | Go API-сервер |
| `dbulashev/dasha-frontend` | Nginx + Vue SPA, проксирует `/api/` на бэкенд |
| `dbulashev/dasha-mcp` | MCP-коннектор для AI-ассистентов (stdio / HTTP) |

Фронтенд принимает переменную окружения `BACKEND_URL` (по умолчанию: `backend:8000`).

## Helm Chart

Чарт публикуется как OCI-артефакт в GitHub Container Registry:

```bash
helm install dasha oci://ghcr.io/dbulashev/charts/dasha --version 0.1.5
```

### Минимальная конфигурация (статические кластеры)

```yaml
config:
  clusters:
    - name: production
      username: monitoring_user
      password_from_env: PG_PASSWORD
      databases: [myapp]
      hosts: [pg-master.example.com]

secrets:
  existingSecret: my-pg-credentials  # должен содержать ключ PG_PASSWORD
```

### С ESO (External Secrets Operator)

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

### Дополнительные переменные окружения (`extraEnv` / `extraEnvFrom`)

Блок `secrets.` подключает к поду ровно один Secret через `envFrom`, поэтому имена переменных обязаны совпадать с именами ключей в этом Secret. Если Secret создаёт кто-то другой — Dex, оператор, соседний чарт, — ключи в нём названы по-своему, а `envFrom` переименовывать не умеет. `backend.extraEnv` объявляет каждую переменную явно и ссылается на конкретный ключ, за счёт чего переименование и происходит:

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
    - name: DASHA_OIDC_SECRET     # переменная, которую ждёт конфиг
      valueFrom:
        secretKeyRef:
          name: dasha-oidc-secret
          key: clientSecret       # ключ, который реально создал Dex
```

`backend.extraEnvFrom` закрывает вторую проблему — подключение источников сверх единственного собственного Secret'а чарта (`secrets.existingSecret` либо созданного через `secrets.externalSecret`). Он импортирует Secret/ConfigMap целиком, имена переменных равны именам ключей, поэтому каждый ключ обязан быть валидным именем переменной окружения — остальные Kubernetes молча пропускает: ключ `client-secret` до пода не доедет, а `clientSecret` доедет:

```yaml
backend:
  extraEnvFrom:
    - secretRef:
        name: dasha-db-passwords
```

Оба параметра есть и у `autosnapshot` — демон читает тот же конфиг, значит ему нужны те же переменные. При совпадении имён `extraEnv` побеждает всё, что приходит через `envFrom`, а внутри `envFrom` побеждает источник, указанный позже: собственный Secret чарта идёт первым, поэтому `extraEnvFrom` его перекрывает.

### С сервис-дискавери Yandex MDB

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

### Ingress с TLS (cert-manager)

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

cert-manager создаст ресурс `Certificate` в namespace приложения.

### Gateway API с TLS (cert-manager)

Портативная альтернатива Ingress — работает с любой реализацией Gateway API (Istio, NGINX Gateway Fabric, Envoy Gateway, Cilium):

```yaml
gatewayAPI:
  enabled: true
  gatewayClassName: istio
  hostname: dasha.example.com
  # Если Gateway живёт в namespace контроллера (например, istio-system),
  # задайте gatewayNamespace — Certificate создаётся в том же namespace.
  # gatewayNamespace: istio-system
  tls:
    enabled: true
    certManager:
      enabled: true
      issuer: cluster-issuer
```

`Certificate` от cert-manager создаётся в namespace Gateway (`gatewayNamespace`, по умолчанию — release namespace). Cross-namespace ссылки на secret потребовали бы `ReferenceGrant`, который чарт не рендерит — поэтому Certificate и Gateway держим в одном namespace.

Рендеримые ресурсы (все условны от `gatewayAPI.enabled: true`):
- `Gateway` — только при `gatewayAPI.createGateway: true` (по умолчанию); HTTP-listener всегда, HTTPS-listener только при `gatewayAPI.tls.enabled: true`.
- `HTTPRoute` (основной) — привязан к HTTPS-listener при `tls.enabled`, иначе к HTTP-listener.
- `HTTPRoute` (редирект HTTP→HTTPS, filter `RequestRedirect`) — только при `gatewayAPI.tls.enabled && gatewayAPI.tls.redirect`, а для чужого Gateway — дополнительно при заданном `existingGateway.redirectSectionName`.
- `Certificate` (cert-manager) — только при `gatewayAPI.tls.certManager.enabled` и когда Gateway создаёт сам чарт.

`ingress.enabled` и `gatewayAPI.enabled` взаимоисключаются — `helm template` падает, если оба true.

### HTTPRoute к уже существующему Gateway

Если в кластере уже есть общий Gateway, который живёт вне этого чарта вместе со своими сертификатами, поставьте `createGateway: false` — чарт отрендерит только `HTTPRoute` и привяжет его к этому Gateway:

```yaml
gatewayAPI:
  enabled: true
  hostname: dasha.example.com
  createGateway: false
  existingGateway:
    name: shared-gateway
    namespace: istio-system
    # Listener для привязки — при tls.enabled указывайте HTTPS.
    sectionName: https
    # Задавайте, только если редирект HTTP→HTTPS должен рендерить чарт.
    # redirectSectionName: http
  tls:
    enabled: true
```

Важное:
- `existingGateway.name` обязателен — без него `helm template` падает.
- `gatewayClassName`, `allowedRoutes` и `tls.certManager` описывают Gateway и в этом режиме игнорируются: listener'ы, сертификат и политика привязки маршрутов — на стороне владельца Gateway. `tls.enabled` по-прежнему важен: он говорит чарту, что точка входа работает по HTTPS (`auth.require_https` в конфиге), и включает рендер редирект-маршрута.
- Пустой `sectionName` привязывает маршрут ко всем подходящим listener'ам этого Gateway, включая обычный HTTP, — и Dasha начинает отдаваться открытым текстом. При `tls.enabled: true` в конфиг попадает ещё и `auth.require_https`, так что через такой HTTP-listener вход не сработает вовсе. Указывайте HTTPS-listener явно.
- Привязку из другого namespace должен разрешать `allowedRoutes` самого Gateway (`from: All` или `Selector` под namespace релиза) — чарт это проверить не может, непривязанный маршрут виден как `Accepted=False` у `HTTPRoute`.
- Редирект-маршрут рендерится только если `existingGateway.redirectSectionName` указывает на HTTP-listener: без `sectionName` он привязался бы и к HTTPS-listener, зациклив редирект на себя. Ещё требуется заданный `existingGateway.sectionName`, причём другой listener — иначе оба маршрута окажутся на одном listener'е и основной выиграет у редиректа (или редирект зациклится на себя); на таких комбинациях `helm template` падает.

### Режим только API (без фронтенда)

```yaml
frontend:
  enabled: false

ingress:
  enabled: true
  domain: dasha-api.example.com
```

### Ключевые возможности чарта

- **Конфиг как ConfigMap** — `dasha.yaml` рендерится из values, пароли не хранятся в открытом виде
- **Пароли через env** — `password_from_env` + ESO или существующий Kubernetes Secret; `extraEnv` / `extraEnvFrom` для Secret'ов, которыми управляет не чарт
- **Ключи сервисных аккаунтов** — отдельный `authorized_key.json` для каждого фолдера через ESO или существующий Secret
- **Фронтенд опционален** — можно развернуть только бэкенд для доступа через API
- **Ingress / Gateway API** — одно правило `/` на фронтенд (который проксирует `/api/` и `/auth/` на бэкенд); авто-редирект HTTP→HTTPS при включённом TLS; поддержка cert-manager; взаимоисключающий `gatewayAPI.enabled` для K8s Gateway API (`gateway.networking.k8s.io/v1`)
- **Безопасность** — `podSecurityContext`, `securityContext`, отдельные настройки для фронтенда и бэкенда

