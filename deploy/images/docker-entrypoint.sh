#!/bin/sh
set -e

export BACKEND_URL="${BACKEND_URL:-backend:8000}"

# Preserve original X-Forwarded-Proto when proxied through ingress/gateway;
# fall back to $scheme when the header is absent (compose, port-forward).
cat > /etc/nginx/conf.d/00-proto-map.conf <<'EOF'
map $http_x_forwarded_proto $forwarded_proto {
    default $http_x_forwarded_proto;
    ""      $scheme;
}
EOF

# Defense-in-depth: redirect HTTP→HTTPS at the pod level if the chart signals TLS upstream.
# Only redirect requests that explicitly arrived via http through a proxy — requests without
# X-Forwarded-Proto (probes, port-forward) are left alone.
if [ "${FORCE_HTTPS_REDIRECT:-false}" = "true" ]; then
    export HTTPS_REDIRECT_BLOCK='if ($http_x_forwarded_proto = "http") { return 301 https://$host$request_uri; }'
else
    export HTTPS_REDIRECT_BLOCK=''
fi

# /mcp → dasha-mcp, only when the deployment wires an MCP endpoint. The directory
# is always (re)created empty so a disabled MCP leaves no stale location behind.
rm -rf /etc/nginx/conf.d/mcp
mkdir -p /etc/nginx/conf.d/mcp
if [ -n "${MCP_URL:-}" ]; then
    envsubst '${MCP_URL}' < /etc/nginx/templates/mcp-location.conf.template > /etc/nginx/conf.d/mcp/mcp.conf
fi

envsubst '${BACKEND_URL} ${HTTPS_REDIRECT_BLOCK}' < /etc/nginx/templates/nginx.conf.template > /etc/nginx/conf.d/default.conf

exec nginx -g 'daemon off;'
