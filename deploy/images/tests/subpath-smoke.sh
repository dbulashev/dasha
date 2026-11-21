#!/usr/bin/env bash
# Test one image with multiple runtime prefixes.
set -euo pipefail
image="${1:-dasha-frontend:ci}"
# Use fixture API keys.
viewer_token=local-test-viewer
admin_token=local-test-admin
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
names=()
cleanup() {
    for name in "${names[@]}"; do docker rm -f "$name" >/dev/null 2>&1 || true; done
}
trap cleanup EXIT

check() {
    local prefix="$1" name="dasha-subpath-$$-${#names[@]}" base html asset ready=false
    local -a backend_options=() backend_command=()
    shift
    names+=("$name")
    if [ -n "${DASHA_SUBPATH_BACKEND:-}" ]; then
        backend_options=(-v "$DASHA_SUBPATH_BACKEND:/test-backend:ro" --entrypoint /bin/sh)
        backend_command=(-c '/test-backend & exec /docker-entrypoint.sh')
    fi
    docker run -d --name "$name" "$@" "${backend_options[@]}" -p 127.0.0.1::3000 \
        -e BACKEND_URL=127.0.0.1:8000 -e DASHA_BASE_PATH="$prefix" "$image" "${backend_command[@]}" >/dev/null
    base="http://127.0.0.1:$(docker port "$name" 3000/tcp | sed 's/.*://')"
    prefix="${prefix%/}"
    for _ in $(seq 30); do
        if curl -fsS --max-time 2 "$base$prefix/" >/dev/null &&
           { [ -z "${DASHA_SUBPATH_BACKEND:-}" ] ||
             curl -fsS --max-time 2 -H "X-API-Key: $viewer_token" "$base$prefix/api/private" >/dev/null; }; then
            ready=true
            break
        fi
        sleep 1
    done
    if [ "$ready" != true ]; then
        echo "Timed out waiting for subpath smoke readiness: ${prefix:-/}" >&2
        docker logs "$name" >&2
        return 1
    fi
    html=$(curl -fsS "$base$prefix/settings")
    # Check that the base element precedes preload and asset links.
    [[ "$html" == *"<head><base href=\"$prefix/\">"* ]]
    asset=$(printf '%s' "$html" | sed -n 's/.*src="\.\/\(assets\/[^"]*\.js\)".*/\1/p')
    test -n "$asset"
    curl -fsS -o /dev/null "$base$prefix/$asset"
    curl -fsS -o /dev/null "$base$prefix/logo.png"
    if [ -n "$prefix" ]; then
        [[ "$(curl -sS -o /dev/null -w '%{http_code}' "$base/")" == 404 ]]
        [[ "$(curl -sS -D - -o /dev/null "$base$prefix" | tr -d '\r')" == *"Location: $prefix/"* ]]
    fi
    if [ -n "${DASHA_SUBPATH_BACKEND:-}" ]; then
        # Exercise nginx proxying and backend prefix stripping.
        local result
        result=$(curl -fsS -H "X-API-Key: $viewer_token" -H 'X-Forwarded-Proto: https' "$base$prefix/api/private?q=a%2Fb")
        [[ "$result" == *'"path":"/api/private"'* && "$result" == *'"query":"q=a%2Fb"'* && "$result" == *'"proto":"https"'* ]]
        [[ "$result" == *"\"uri\":\"$prefix/api/private?q=a%2Fb\""* ]]
        [[ "$(curl -sS -o /dev/null -w '%{http_code}' "$base$prefix/api/private")" == 401 ]]
        [[ "$(curl -sS -X POST -H "X-API-Key: $viewer_token" -o /dev/null -w '%{http_code}' "$base$prefix/api/private")" == 403 ]]
        result=$(curl -fsS -X POST -H "X-API-Key: $admin_token" -d 'profile-test' "$base$prefix/api/activity/profile?q=a%2Fb")
        [[ "$result" == *'"path":"/api/activity/profile"'* && "$result" == *'"body":"profile-test"'* && "$result" == *'"query":"q=a%2Fb"'* ]]
        [[ "$result" == *"\"uri\":\"$prefix/api/activity/profile?q=a%2Fb\""* ]]
        [[ "$(curl -sS -D - -o /dev/null "$base$prefix/auth/login?state=a%2Fb" | tr -d '\r')" == *"Location: $prefix/auth/callback?state=a%2Fb"* ]]
        for path in /api/private /api/auth/info; do
            result=$(docker exec "$name" curl -fsS -H "X-API-Key: $viewer_token" "http://127.0.0.1:8000$path")
            [[ "$result" == *"\"path\":\"$path\""* ]]
        done
        [[ "$(docker exec "$name" curl -sS -D - -o /dev/null 'http://127.0.0.1:8000/auth/login?state=a%2Fb' | tr -d '\r')" == *"Location: $prefix/auth/callback?state=a%2Fb"* ]]
    fi
    if [ "${DASHA_SUBPATH_BROWSER:-false}" = true ]; then
        (cd "$root/frontend" && DASHA_SUBPATH_URL="$base$prefix/" \
            npm run test:e2e -- e2e/subpath.spec.ts --project=chromium --workers=1)
    fi
    echo "OK: runtime prefix ${prefix:-/} $*"
    docker rm -f "$name" >/dev/null
}

check ""
check /dasha
check /apiary
check /authentication
check /mcp-tools
check /tools/api
check /.dasha
check /tools/dasha-1.2/ --user 12345:0 --read-only \
    -e MCP_URL=127.0.0.1:9000 \
    --tmpfs /etc/nginx/conf.d:gid=0,mode=0775 \
    --tmpfs /var/cache/nginx:gid=0,mode=0775 --tmpfs /tmp
for prefix in '//example.test' '/tools/../dasha' '/a//b' '/bad?query' $'/ok\nbad' \
    /api /api/ /auth /auth/ /mcp /mcp/ /api/auth /api/autosnapshot/clusters /auth/tools /mcp/tools; do
    name="dasha-invalid-$$-${#names[@]}"
    names+=("$name")
    docker run -d --name "$name" -e BACKEND_URL=127.0.0.1:8000 -e DASHA_BASE_PATH="$prefix" "$image" >/dev/null
    if ! status=$(timeout 10 docker wait "$name") || [ "$status" = 0 ]; then
        echo "Accepted an invalid prefix: $prefix" >&2
        exit 1
    fi
    docker logs "$name" 2>&1 | grep -Fq 'Invalid DASHA_BASE_PATH'
done
