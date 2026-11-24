#!/usr/bin/env bash
# Called with the development DB configuration from make. Load the shared menu fixture.
set -euo pipefail
cd "$(dirname "$0")/../.."
umask 077
: "${ANTHROPIC_API_KEY:?Set ANTHROPIC_API_KEY in the project .env or shell for local evaluations}"
export PGHOST="$DB_HOST" PGUSER="$DB_USER" PGPORT="${DB_PORT:-5432}" PGPASSWORD="$DB_PASSWORD" PGSSLMODE="${DB_SSLMODE:-disable}"
export PGDATABASE="$DB_NAME"
work=$(mktemp -d "${TMPDIR:-/tmp}/mogumogu-quality.XXXXXXXX")
db="mogumogu_quality_$(openssl rand -hex 6)"
created=0
backend_pid=
ai_pid=
cleanup() {
    if [[ -n "$backend_pid" ]]; then kill "$backend_pid" 2>/dev/null || true; wait "$backend_pid" || true; fi
    if [[ -n "$ai_pid" ]]; then kill "$ai_pid" 2>/dev/null || true; wait "$ai_pid" || true; fi
    if [[ "$created" == 1 ]]; then dropdb --if-exists "$db"; fi
    rm -rf -- "$work"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
for port in 18180 18181 18190; do
    if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null; then echo "Evaluation port $port in use" >&2; exit 1; fi
done
createdb "$db"
created=1
(cd backend && DB_NAME="$db" go run ./cmd/migrate up)
# Migrations seed the cafeteria directory. Replace it with the captured directory
# in this newly created database, in the same transaction as the menu import.
export EVAL_MENU_FIXTURE="$PWD/ai/evals/cafeteria_menu.sql"
export EVAL_CONTEXT_TIME="2026-11-24T14:23:00+09:00"
psql -Xq --single-transaction --set ON_ERROR_STOP=1 -d "$db" \
    -c 'DELETE FROM public.cafeterias' -f "$EVAL_MENU_FIXTURE" > /dev/null
(cd backend && go build -tags cafeteria_eval -o "$work/server" ./cmd/server)
export EVAL_MENU_LOG="$work/menus.jsonl"
export EVAL_AI_LOG="$work/replies.jsonl"
touch "$EVAL_MENU_LOG" "$EVAL_AI_LOG"
export EVAL_ASSISTANT_MODEL="${EVAL_ASSISTANT_MODEL:-claude-haiku-4-5-20251001}"
export EVAL_JUDGE_MODEL="${EVAL_JUDGE_MODEL:-claude-sonnet-4-6}"
export JWT_SECRET="$(openssl rand -hex 32)" CSRF_SECRET="$(openssl rand -hex 32)"
DB_NAME="$db" APP_PORT=18180 INTERNAL_PORT=18181 AI_SERVICE_URL=http://127.0.0.1:18190 \
    ALLOWED_ORIGINS=http://127.0.0.1:18180 COOKIE_SECURE=false \
    "$work/server" >"$work/backend.log" 2>&1 &
backend_pid=$!
(cd ai && GO_INTERNAL_URL=http://127.0.0.1:18181 LANGFUSE_PUBLIC_KEY= LANGFUSE_SECRET_KEY= \
    SEARCH_PROVIDER=off exec uv run --locked uvicorn capture_app:app --app-dir evals --host 127.0.0.1 --port 18190 --no-access-log) >"$work/ai.log" 2>&1 &
ai_pid=$!
ready() {
    kill -0 "$ai_pid" "$backend_pid" 2>/dev/null &&
        curl -fsS -o /dev/null http://127.0.0.1:18180/api/health/ready 2>/dev/null &&
        curl -fsS -o /dev/null http://127.0.0.1:18190/healthz 2>/dev/null
}
for _ in {1..100}; do ready && break; sleep 0.3; done
if ! ready; then tail -n 12 "$work/backend.log" "$work/ai.log" >&2; exit 1; fi
export DEEPEVAL_TELEMETRY_OPT_OUT=YES
uv run --locked --script ai/evals/quality.py "$@"
