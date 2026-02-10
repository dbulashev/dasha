#!/bin/sh
set -e

export BACKEND_URL="${BACKEND_URL:-backend:8000}"

envsubst '${BACKEND_URL}' < /etc/nginx/templates/nginx.conf.template > /etc/nginx/conf.d/default.conf

exec nginx -g 'daemon off;'
