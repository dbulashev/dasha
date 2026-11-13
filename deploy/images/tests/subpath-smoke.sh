#!/usr/bin/env bash
# Один собранный образ проверяется с разными runtime-префиксами, без пересборки.
set -euo pipefail
image="${1:-dasha-frontend:ci}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
names=()
cleanup() {
    for name in "${names[@]}"; do docker rm -f "$name" >/dev/null 2>&1 || true; done
}
trap cleanup EXIT

check() {
    local prefix="$1" name="dasha-subpath-$$-${#names[@]}" base html asset
    shift
    names+=("$name")
    docker run -d --name "$name" "$@" -p 127.0.0.1::3000 \
        -e BACKEND_URL=127.0.0.1:8000 -e DASHA_BASE_PATH="$prefix" "$image" >/dev/null
    base="http://127.0.0.1:$(docker port "$name" 3000/tcp | sed 's/.*://')"
    prefix="${prefix%/}"
    for _ in $(seq 30); do
        if curl -fsS "$base$prefix/" >/dev/null; then break; fi
        sleep 1
    done
    html=$(curl -fsS "$base$prefix/settings")
    # base обязан идти до preload/font/script, иначе deep links запрашивают неверные assets.
    [[ "$html" == *"<head><base href=\"$prefix/\">"* ]]
    asset=$(printf '%s' "$html" | sed -n 's/.*src="\.\/\(assets\/[^"]*\.js\)".*/\1/p')
    test -n "$asset"
    curl -fsS -o /dev/null "$base$prefix/$asset"
    curl -fsS -o /dev/null "$base$prefix/logo.png"
    if [ -n "$prefix" ]; then
        [[ "$(curl -sS -o /dev/null -w '%{http_code}' "$base/")" == 404 ]]
        [[ "$(curl -sS -D - -o /dev/null "$base$prefix" | tr -d '\r')" == *"Location: $prefix/"* ]]
    fi
    if [ "${DASHA_SUBPATH_BROWSER:-false}" = true ]; then
        (cd "$root/frontend" && DASHA_SUBPATH_URL="$base$prefix/" \
            npm run test:e2e -- e2e/subpath.spec.ts --project=chromium --workers=1)
    fi
    echo "OK: runtime-префикс ${prefix:-/} $*"
    docker rm -f "$name" >/dev/null
}

check ""
check /dasha
check /.dasha
check /tools/dasha-1.2/ --user 12345:0 --read-only \
    -e MCP_URL=127.0.0.1:9000 \
    --tmpfs /etc/nginx/conf.d:gid=0,mode=0775 \
    --tmpfs /var/cache/nginx:gid=0,mode=0775 --tmpfs /tmp
for prefix in '//example.test' '/tools/../dasha' '/a//b' '/bad?query' $'/ok\nbad'; do
    name="dasha-invalid-$$-${#names[@]}"
    names+=("$name")
    docker run -d --name "$name" -e BACKEND_URL=127.0.0.1:8000 -e DASHA_BASE_PATH="$prefix" "$image" >/dev/null
    if ! status=$(timeout 10 docker wait "$name") || [ "$status" = 0 ]; then
        echo "Принят недопустимый префикс: $prefix" >&2
        exit 1
    fi
    docker logs "$name" 2>&1 | grep -Fq 'Недопустимый DASHA_BASE_PATH'
done
