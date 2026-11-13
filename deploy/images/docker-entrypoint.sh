#!/bin/sh
set -e

export BACKEND_URL="${BACKEND_URL:-backend:8000}"

# Префикс — только путь, не произвольный URL или фрагмент nginx-конфигурации.
export DASHA_BASE_PATH="${DASHA_BASE_PATH:-}"
DASHA_BASE_PATH="${DASHA_BASE_PATH%/}"
case "$DASHA_BASE_PATH" in
    *[!A-Za-z0-9/._~-]*)
        echo 'Недопустимый DASHA_BASE_PATH' >&2
        exit 1
        ;;
esac
if [ -n "$DASHA_BASE_PATH" ]; then
    if ! printf '%s\n' "$DASHA_BASE_PATH" | grep -Eq '^(/[A-Za-z0-9._~-]+)+$' ||
       printf '%s\n' "$DASHA_BASE_PATH" | grep -Eq '(^|/)\.\.?(/|$)'; then
        echo 'Недопустимый DASHA_BASE_PATH' >&2
        exit 1
    fi
fi
export BASE_PATH_REDIRECT_BLOCK=''
export OUTSIDE_BASE_PATH_BLOCK=''
export BASE_PATH_REGEX
BASE_PATH_REGEX=$(printf '%s' "$DASHA_BASE_PATH" | sed 's/\./\\./g')
if [ -n "$DASHA_BASE_PATH" ]; then
    BASE_PATH_REDIRECT_BLOCK="location = $DASHA_BASE_PATH { return 308 $DASHA_BASE_PATH/; }"
    OUTSIDE_BASE_PATH_BLOCK='location / { return 404; }'
fi

# Только HTML получает runtime-значение <base>; скомпилированные JS/CSS не меняются.
# conf.d уже доступен для записи в штатном Helm chart, включая read-only rootfs.
site=/etc/nginx/conf.d/runtime
rm -rf "$site"
mkdir -p "$site$DASHA_BASE_PATH"
for file in /usr/share/nginx/html/*; do
    [ "$(basename "$file")" = index.html ] && continue
    ln -s "$file" "$site$DASHA_BASE_PATH/$(basename "$file")"
done
# <base> должен предшествовать preload-ссылкам, добавленным Vite plugins.
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
# Это текст nginx, не shell-команда; кавычки и переменные сохраняются для envsubst.
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
envsubst '${BACKEND_URL} ${HTTPS_REDIRECT_BLOCK} ${DASHA_BASE_PATH} ${BASE_PATH_REGEX} ${BASE_PATH_REDIRECT_BLOCK} ${OUTSIDE_BASE_PATH_BLOCK}' < /etc/nginx/templates/nginx.conf.template > /etc/nginx/conf.d/default.conf

exec nginx -g 'daemon off;'
