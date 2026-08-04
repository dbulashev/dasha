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

exit "$FAILED"
