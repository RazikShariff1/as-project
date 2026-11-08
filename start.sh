#!/bin/sh
# Runs the four services in one container. Only the gateway listens on Render's public $PORT; the others stay on
# localhost (8081-8084), which are the defaults the gateway and catalogue-svc already use.
# Each service runs from its own directory so GoFr finds its own configs/.env, and gets its own database credentials.
set -u

: "${PORT:=10000}"

# run <service> <env-prefix>: start the service with <PREFIX>_DB_* mapped onto the DB_* variables GoFr reads.
run() {
  svc="$1"; p="$2"
  (
    cd "/app/$svc" || exit 1
    eval "export DB_HOST=\${${p}_DB_HOST:-} DB_PORT=\${${p}_DB_PORT:-5432} DB_USER=\${${p}_DB_USER:-} \
DB_PASSWORD=\${${p}_DB_PASSWORD:-} DB_SSL_MODE=\${${p}_DB_SSL_MODE:-require}"
    # Optional: override the database name from configs/.env (a separate Neon project's database is called "neondb").
    eval "[ -z \"\${${p}_DB_NAME:-}\" ] || export DB_NAME=\"\${${p}_DB_NAME}\""
    # With in-process delivery there is no broker: stop configs/.env (PUBSUB_BACKEND=GOOGLE) from starting Pub/Sub.
    if [ "$svc" = catalogue-svc ] && [ "${SELLER_CHANGE_DELIVERY:-pubsub}" = direct ]; then export PUBSUB_BACKEND=""; fi
    [ -n "$DB_HOST" ] || { echo "$svc: ${p}_DB_HOST is not set" >&2; exit 1; }
    exec "./$svc"
  ) &
}

run account-svc   ACCOUNT
run inventory-svc INVENTORY
run catalogue-svc CATALOGUE
# order-svc is optional until its database is configured, so adding it never takes the other services down.
if [ -n "${ORDER_DB_HOST:-}" ]; then run order-svc ORDER; else echo "order-svc skipped: ORDER_DB_HOST is not set" >&2; fi

# Gateway: public port, no database.
(cd /app/gateway-svc && HTTP_PORT="$PORT" exec ./gateway-svc) &

# If any service exits, stop the container so Render restarts all of them together.
trap 'kill $(jobs -p) 2>/dev/null' TERM INT
wait -n
code=$?
echo "a service exited (code $code); stopping container" >&2
kill $(jobs -p) 2>/dev/null
exit "$code"
