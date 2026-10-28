#!/usr/bin/env bash
# Browser end-to-end test against a throwaway database (`make test-e2e`).
#
#  1. creates mogumogu_e2e_<random> on the compose PostgreSQL and migrates it
#  2. starts the backend (:18080, internal :18081) and Vite (:15173) pointed at that database,
#     and the AI service (:18090) with the fake model (no AWS, no cost), all on
#     their own ports so a running `make dev` is not disturbed
#  3. runs e2e/browser.mjs in headless Chrome
#  4. always stops the servers and drops the database, even on failure or Ctrl+C
#
# Development data is never read or written. Requires the database container
# (`make db`), a built backend (backend/bin/server) and frontend/node_modules;
# the Makefile target takes care of all three.
set -euo pipefail

cd "$(dirname "$0")/.."
set -a
. ./.env
set +a

BACKEND_PORT=18080
BACKEND_INTERNAL_PORT=18081
AI_PORT=18090
FRONTEND_PORT=15173
BASE_URL="http://localhost:${FRONTEND_PORT}"
E2E_DB="mogumogu_e2e_$(openssl rand -hex 4)"
WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/mogumogu-e2e.XXXXXX")"
BACKEND_PID=""
FRONTEND_PID=""
AI_PID=""
DB_CREATED=""

# psql as the compose superuser, connected to the maintenance database.
admin_sql() {
	docker compose exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 -q -U "$POSTGRES_USER" -d postgres -c "$1"' _ "$1"
}

cleanup() {
	local status=$?
	trap - EXIT INT TERM
	# SIGTERM lets the backend shut down gracefully.
	[ -n "$FRONTEND_PID" ] && kill -TERM "$FRONTEND_PID" 2>/dev/null || true
	[ -n "$BACKEND_PID" ] && kill -TERM "$BACKEND_PID" 2>/dev/null || true
	[ -n "$AI_PID" ] && kill -TERM "$AI_PID" 2>/dev/null || true
	wait 2>/dev/null || true
	if [ -n "$DB_CREATED" ]; then
		admin_sql "DROP DATABASE IF EXISTS ${E2E_DB} WITH (FORCE)" && echo "e2e: dropped ${E2E_DB}"
	fi
	if [ "$status" -eq 130 ]; then
		echo "e2e: interrupted"
	elif [ "$status" -ne 0 ]; then
		echo "e2e: FAILED - last backend, AI service and frontend log lines:"
		tail -n 20 "$WORK_DIR/backend.log" "$WORK_DIR/ai.log" "$WORK_DIR/frontend.log" 2>/dev/null || true
	fi
	case "$WORK_DIR" in
	*/mogumogu-e2e.*) rm -rf -- "$WORK_DIR" ;;
	esac
	exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

for port in "$BACKEND_PORT" "$BACKEND_INTERNAL_PORT" "$AI_PORT" "$FRONTEND_PORT"; do
	if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
		echo "e2e: port $port is already in use; stop whatever is listening there" >&2
		exit 1
	fi
done

echo "e2e: creating ${E2E_DB}"
admin_sql "CREATE DATABASE ${E2E_DB}"
DB_CREATED=1
(cd backend && DB_NAME="$E2E_DB" go run ./cmd/migrate up >/dev/null)

DB_NAME="$E2E_DB" APP_PORT="$BACKEND_PORT" INTERNAL_PORT="$BACKEND_INTERNAL_PORT" ALLOWED_ORIGINS="$BASE_URL" \
	AI_SERVICE_URL="http://127.0.0.1:${AI_PORT}" \
	./backend/bin/server >"$WORK_DIR/backend.log" 2>&1 &
BACKEND_PID=$!

(cd ai && MODEL_PROVIDER=fake GO_INTERNAL_URL="http://127.0.0.1:${BACKEND_INTERNAL_PORT}" \
	exec uv run uvicorn mogumogu_ai.app:app --host 127.0.0.1 --port "$AI_PORT") >"$WORK_DIR/ai.log" 2>&1 &
AI_PID=$!

(cd frontend && API_PROXY_TARGET="http://localhost:${BACKEND_PORT}" \
	exec ./node_modules/.bin/vite --port "$FRONTEND_PORT" --strictPort) >"$WORK_DIR/frontend.log" 2>&1 &
FRONTEND_PID=$!

echo "e2e: waiting for backend :${BACKEND_PORT}, AI service :${AI_PORT} and frontend :${FRONTEND_PORT}"
ready() {
	curl -fs -o /dev/null "http://localhost:${BACKEND_PORT}/api/health/ready" &&
		curl -fs -o /dev/null "http://127.0.0.1:${AI_PORT}/healthz" &&
		curl -fs -o /dev/null "$BASE_URL/"
}
for _ in $(seq 1 120); do
	ready && break
	sleep 0.5
done
ready

E2E_BASE_URL="$BASE_URL" node e2e/browser.mjs "$WORK_DIR/chrome"
