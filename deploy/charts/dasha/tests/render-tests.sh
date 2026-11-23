#!/usr/bin/env bash
# Render tests for the chart: assert that `helm template` accepts valid value
# combinations and rejects invalid ones. Run from anywhere: tests/render-tests.sh
set -uo pipefail

CHART="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FAILED=0

# render_ok <name> <helm --set args...>
render_ok() {
  local name="$1"; shift
  local out
  if out=$(helm template test "$CHART" "$@" 2>&1); then
    echo "ok       $name"
  else
    echo "FAIL     $name — expected a successful render, helm said:"
    echo "$out" | sed 's/^/           /'
    FAILED=1
  fi
}

# render_fails <name> <expected substring> <helm --set args...>
render_fails() {
  local name="$1" want="$2"; shift 2
  local out
  if out=$(helm template test "$CHART" "$@" 2>&1); then
    echo "FAIL     $name — expected the render to be rejected, but it succeeded"
    FAILED=1
  elif [[ "$out" != *"$want"* ]]; then
    echo "FAIL     $name — rejected, but not for the expected reason; helm said:"
    echo "$out" | sed 's/^/           /'
    FAILED=1
  else
    echo "ok       $name"
  fi
}

GW_TLS=(
  --set gatewayAPI.enabled=true
  --set gatewayAPI.tls.enabled=true
  --set gatewayAPI.tls.redirect=true
  --set gatewayAPI.createGateway=false
  --set gatewayAPI.existingGateway.name=shared-gw
)

render_ok "existing gateway: distinct listeners for main and redirect route" \
  "${GW_TLS[@]}" \
  --set gatewayAPI.existingGateway.sectionName=https \
  --set gatewayAPI.existingGateway.redirectSectionName=http

render_fails "existing gateway: same listener for main and redirect route" \
  "are both \"https\"" \
  "${GW_TLS[@]}" \
  --set gatewayAPI.existingGateway.sectionName=https \
  --set gatewayAPI.existingGateway.redirectSectionName=https

render_fails "existing gateway: redirect listener without a main listener" \
  "existingGateway.sectionName is empty" \
  "${GW_TLS[@]}" \
  --set gatewayAPI.existingGateway.redirectSectionName=http

render_fails "existing gateway without a name" \
  "requires gatewayAPI.existingGateway.name" \
  --set gatewayAPI.enabled=true \
  --set gatewayAPI.createGateway=false

render_fails "ingress and gateway API at the same time" \
  "mutually exclusive" \
  --set ingress.enabled=true \
  --set gatewayAPI.enabled=true

render_ok "prefix: nested path" --set config.base_path=/tools/dasha/
render_ok "prefix: headless backend" --set config.base_path=/dasha --set frontend.enabled=false --set ingress.enabled=true
for prefix in "" / /apiary /authentication /mcp-tools /tools/api; do
  render_ok "prefix: ${prefix:-root}" --set-string "config.base_path=$prefix"
done
render_fails "prefix: relative path" "config.base_path" --set config.base_path=dasha
render_fails "prefix: traversal" "config.base_path" --set config.base_path=/tools/../dasha
render_fails "prefix: foreign origin" "config.base_path" --set config.base_path=//example.com
render_fails "prefix: invalid type" "config.base_path" --set config.base_path=false
for prefix in /api /api/ /auth /auth/ /mcp /mcp/ /api/auth /api/autosnapshot/clusters /auth/tools /mcp/tools; do
  render_fails "prefix: reserved first segment $prefix" "config.base_path" --set "config.base_path=$prefix"
done

out=$(helm template test "$CHART" --set config.base_path=/tools/dasha/ --set ingress.enabled=true)
for expected in 'base_path: /tools/dasha' 'value: "/tools/dasha"' 'path: "/tools/dasha/"' 'path: /tools/dasha'; do
  if ! grep -Fq "$expected" <<<"$out"; then
    echo "FAIL: chart prefix lost: $expected"
    FAILED=1
  fi
done


out=$(helm template test "$CHART")
if ! grep -A1 -F 'name: API_PROXY_READ_TIMEOUT' <<<"$out" | grep -Fq 'value: "30s"'; then
  echo "FAIL: default API_PROXY_READ_TIMEOUT is not 30s"
  FAILED=1
fi
if grep -Fq 'http_server' <<<"$out"; then
  echo "FAIL: http_server rendered without being set"
  FAILED=1
fi

out=$(helm template test "$CHART" --set frontend.proxyReadTimeout=150s \
  --set config.http_server.read_timeout=2m --set config.http_server.write_timeout=2m)
if ! grep -A1 -F 'name: API_PROXY_READ_TIMEOUT' <<<"$out" | grep -Fq 'value: "150s"'; then
  echo "FAIL: frontend.proxyReadTimeout not passed to the frontend"
  FAILED=1
fi
for expected in 'read_timeout: 2m' 'write_timeout: 2m'; do
  if ! grep -Fq "$expected" <<<"$out"; then
    echo "FAIL: http_server lost: $expected"
    FAILED=1
  fi
done

render_ok "proxyReadTimeout: bare seconds" --set frontend.proxyReadTimeout=90
render_ok "proxyReadTimeout: null falls back to 30s" --set frontend.proxyReadTimeout=null
render_fails "proxyReadTimeout: integer 0" "frontend.proxyReadTimeout" --set frontend.proxyReadTimeout=0
for value in 0s 1d '30 s' '30s;'; do
  render_fails "proxyReadTimeout: invalid $value" "frontend.proxyReadTimeout" --set-string "frontend.proxyReadTimeout=$value"
done

exit "$FAILED"
