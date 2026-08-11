# Authentication

[Русская версия](../ru/auth.md) · [← README](../../README.md)

Dasha supports three authentication modes configured in `dasha.yaml`:

**No authentication (default)**
```yaml
auth:
  mode: none
```

**Static API keys**
```yaml
auth:
  mode: token
  tokens:
    - name: monitoring
      token_from_env: DASHA_TOKEN_MONITORING
      role: viewer
    - name: admin-cli
      token_from_env: DASHA_TOKEN_ADMIN
      role: admin
```

Clients send the key via `X-API-Key` header.

**OpenID Connect (Keycloak, Google, etc.)**
```yaml
auth:
  mode: oidc
  oidc:
    issuer_url: "https://keycloak.example.com/realms/dasha"
    client_id: "dasha-app"
    client_secret_from_env: DASHA_OIDC_SECRET
    redirect_url: "https://dasha.example.com/auth/callback"
    role_claim: "realm_access.roles"
  cookie_secret_from_env: DASHA_COOKIE_SECRET  # 32+ chars for AES-256
  cookie_max_age: 86400
  tokens:  # API keys also work alongside OIDC
    - name: monitoring
      token_from_env: DASHA_TOKEN_MONITORING
      role: viewer
  rate_limit:
    requests_per_second: 10
    burst: 20
```

In any mode other than `none`, set `auth.require_https: true` in production: Dasha then rejects requests that arrive over plaintext HTTP (`X-Forwarded-Proto: https` from a TLS-terminating proxy counts as secure), and the backend logs a startup warning while the flag is off. It is left out of the examples above because it breaks a local `http://localhost` setup.

Roles are extracted from the OIDC ID token claims at the path specified by `role_claim`. Supported roles: `admin` (full access) and `viewer` (read-only GET requests). If no known role is found, `viewer` is assigned by default.

**Generating secrets**

```bash
# Cookie secret (32+ characters for AES-256 session encryption)
openssl rand -base64 32

# OIDC client secret (register this value in your OIDC provider)
openssl rand -base64 32
```

## Personal Access Tokens (optional)

A logged-in user can mint **personal access tokens (PATs)** — bearer secrets sent as the `X-API-Key` header — so non-browser clients (the `dasha-mcp` server, scripts) act with that user's identity and role (RBAC is preserved). Requires snapshot storage: tokens are stored hashed in `api_tokens`, so run `dasha migrate` first.

**Auth mode must be `oidc`.** Minting requires an individually-identifiable principal, so it is refused for a static config token (shared, carries no per-user identity — a leaked one could otherwise mint tokens that outlive its removal from the config) and for another PAT (anti-chaining). Who may mint is further gated by `auth.pat_min_role`: `admin` (default while the feature matures) or `viewer` (any signed-in user).

- **Mint from the UI**: user menu → gear (*Settings*) → *My tokens* → create (name, role ≤ your own, optional expiry). The full secret is shown **once**.
- **Use it from any client:**

  ```bash
  curl -H "X-API-Key: dasha_pat_…" http://localhost:8000/api/clusters
  ```

List your tokens with `GET /api/auth/tokens` (no secrets); revoke with `DELETE /api/auth/tokens/{id}` (effective immediately). The requested role cannot exceed yours (default `viewer`); `expires_in_days` is optional (0 / omitted = no expiry). Both listings accept `?include_revoked=true` — a revoked token is kept as an audit row but can never authenticate again.

**Lifetime limits.** A token's role is frozen when it is minted: authentication reads the role stored on the token, not the identity provider, so a token keeps working after its owner is demoted or leaves the company. Two limits bound that exposure without relying on someone remembering to revoke:

- **Admin tokens expire within 30 days** — including when `expires_in_days` is omitted (otherwise the cap would be bypassed by simply not asking for an expiry). An over-long request is clamped, not rejected; check `expires_at` in the response for the value applied.
- **Any token unused for 90 days is revoked automatically.** Idleness is measured from last use, falling back to creation for a token that was never used — so a token minted and forgotten does not live forever. The cutoff takes effect the moment it is crossed (checked at authentication), and a background sweep records `revoked_at` so the token shows as revoked rather than looking live while silently failing.

Neither limit replaces revocation: for a departure, revoke the tokens (*Settings* → *All tokens*), since an idle cutoff only expires what nobody is using.

**Administration (admin only).** An administrator sees and revokes every user's tokens, and browses who has access — *Settings* → *All tokens* / *Users*:

```bash
curl -H "Cookie: <oidc-session>" http://localhost:8000/api/auth/admin/tokens        # all owners' tokens
curl -H "Cookie: <oidc-session>" -X DELETE .../api/auth/admin/tokens/{id}           # revoke any of them
curl -H "Cookie: <oidc-session>" http://localhost:8000/api/auth/admin/users          # who signed in, and when
```

The user directory is populated by SSO sign-ins (`users` table, also created by `dasha migrate`): each principal gets a row on first login, with `last_login_at` refreshed on every login. Roles shown there come from the identity provider and are an audit trail, not an authorization source. Like minting, these endpoints require an **interactive OIDC admin session** — an admin-scoped PAT is refused, so a leaked token cannot enumerate or revoke the tokens that would replace it.

