#!/bin/sh
set -e

export BACKEND_URL="${BACKEND_URL:-backend:8000}"

# Validate the runtime URL prefix.
export DASHA_BASE_PATH="${DASHA_BASE_PATH:-}"
DASHA_BASE_PATH="${DASHA_BASE_PATH%/}"
case "$DASHA_BASE_PATH" in
    *[!A-Za-z0-9/._~-]*)
        echo 'Invalid DASHA_BASE_PATH' >&2
        exit 1
        ;;
esac
if [ -n "$DASHA_BASE_PATH" ]; then
    if ! printf '%s\n' "$DASHA_BASE_PATH" | grep -Eq '^(/[A-Za-z0-9._~-]+)+$' ||
       printf '%s\n' "$DASHA_BASE_PATH" | grep -Eq '(^|/)\.\.?(/|$)|^/(api|auth|mcp)(/|$)'; then
        echo 'Invalid DASHA_BASE_PATH' >&2
        exit 1
    fi
fi
# Validated: it lands in nginx.conf verbatim.
export API_PROXY_READ_TIMEOUT="${API_PROXY_READ_TIMEOUT:-30s}"
case "$API_PROXY_READ_TIMEOUT" in
    *[!0-9hms]*)
        echo 'Invalid API_PROXY_READ_TIMEOUT' >&2
        exit 1
        ;;
esac
if ! printf '%s\n' "$API_PROXY_READ_TIMEOUT" | grep -Eqx '[1-9][0-9]*(ms|s|m|h)?'; then
    echo 'Invalid API_PROXY_READ_TIMEOUT' >&2
    exit 1
fi
export BASE_PATH_REDIRECT_BLOCK=''
export OUTSIDE_BASE_PATH_BLOCK=''
export BASE_PATH_REGEX
BASE_PATH_REGEX=$(printf '%s' "$DASHA_BASE_PATH" | sed 's/\./\\./g')
if [ -n "$DASHA_BASE_PATH" ]; then
    BASE_PATH_REDIRECT_BLOCK="location = $DASHA_BASE_PATH { return 308 $DASHA_BASE_PATH/; }"
    OUTSIDE_BASE_PATH_BLOCK='location / { return 404; }'
fi

# Render the runtime HTML base and link the static assets.
site=/etc/nginx/conf.d/runtime
rm -rf "$site"
mkdir -p "$site$DASHA_BASE_PATH"
for file in /usr/share/nginx/html/*; do
    [ "$(basename "$file")" = index.html ] && continue
    ln -s "$file" "$site$DASHA_BASE_PATH/$(basename "$file")"
done
# Insert the base element before preload links.
sed -e '/<base href="\/">/d' -e "s|<head>|<head><base href=\"$DASHA_BASE_PATH/\">|" \
    /usr/share/nginx/html/index.html > "$site$DASHA_BASE_PATH/index.html"

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
# Preserve nginx variables for envsubst.
# shellcheck disable=SC2016,SC2089,SC2090
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
    # shellcheck disable=SC2016
    envsubst '${MCP_URL} ${DASHA_BASE_PATH} ${BASE_PATH_REGEX}' < /etc/nginx/templates/mcp-location.conf.template > /etc/nginx/conf.d/mcp/mcp.conf
fi

# shellcheck disable=SC2016
envsubst '${BACKEND_URL} ${API_PROXY_READ_TIMEOUT} ${HTTPS_REDIRECT_BLOCK} ${DASHA_BASE_PATH} ${BASE_PATH_REGEX} ${BASE_PATH_REDIRECT_BLOCK} ${OUTSIDE_BASE_PATH_BLOCK}' < /etc/nginx/templates/nginx.conf.template > /etc/nginx/conf.d/default.conf

exec nginx -g 'daemon off;'
