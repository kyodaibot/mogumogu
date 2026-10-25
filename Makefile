# Local development shortcuts. Run `make` to list targets.
# Every target is a thin wrapper around the commands documented in README.md.

SHELL := /bin/bash
.DEFAULT_GOAL := help

BACKEND_BIN := backend/bin/server
# Export the repo-root .env for the command that follows. Each recipe line runs
# in its own shell, so the variables never reach your terminal.
WITH_ENV := set -a; . ./.env; set +a;

.PHONY: help setup check-env db db-stop db-down migrate-up migrate-down migrate-version migrate-create \
	backend backend-build frontend dev test test-integration test-e2e app-up app-down app-logs db-backup

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "  setup             First-time setup: create .env (with generated secrets) if missing, npm ci, pre-commit install"
	@echo "  dev               Start database, apply migrations, then backend + frontend together (Ctrl+C stops both)"
	@echo "  db                Start PostgreSQL in the background and wait until healthy"
	@echo "  backend           Build and run the Go backend in the foreground (:8080)"
	@echo "  frontend          Run the Vite dev server in the foreground (http://localhost:5173)"
	@echo "  test              Unit tests and builds: go vet, go test, npm run build, npm test"
	@echo "  test-integration  Backend tests against PostgreSQL (needs 'make db'; uses throwaway databases)"
	@echo "  test-e2e          Browser test of the whole UI in headless Chrome (throwaway database, ports 18080/15173)"
	@echo "  migrate-up        Apply all pending migrations"
	@echo "  migrate-down      Roll back the most recent migration"
	@echo "  migrate-version   Show the current migration version"
	@echo "  migrate-create    Add an empty migration pair: make migrate-create name=add_something"
	@echo "  db-stop           Stop the database container (data kept)"
	@echo "  db-down           Remove the database container (data kept in the pgdata volume)"
	@echo "  db-backup         Dump the database to backups/mogumogu-<time>.dump (restore: docs/operations.md)"
	@echo ""
	@echo "  app-up            Build images and start everything in containers: http://localhost:8000"
	@echo "  app-down          Stop and remove all containers, including the database (data kept)"
	@echo "  app-logs          Follow the logs of all containers (Ctrl+C stops following)"
	@echo ""
	@echo "Deleting data is deliberately not a target: run 'docker compose down -v' yourself."

setup: frontend/node_modules
	@if [ -f .env ]; then \
		echo ".env already exists, leaving it unchanged"; \
	else \
		awk -v j="$$(openssl rand -base64 48)" -v c="$$(openssl rand -base64 48)" \
			'/^JWT_SECRET=/ {print "JWT_SECRET=" j; next} /^CSRF_SECRET=/ {print "CSRF_SECRET=" c; next} {print}' \
			.env.example > .env && \
		echo "Created .env with generated JWT_SECRET and CSRF_SECRET - edit DB_PASSWORD before starting"; \
	fi
	pre-commit install

check-env:
	@test -f .env || { echo "Missing .env - run 'make setup' (or: cp .env.example .env)"; exit 1; }

db: check-env
	docker compose up -d --wait

migrate-up: check-env
	@$(WITH_ENV) cd backend && go run ./cmd/migrate up

migrate-down: check-env
	@$(WITH_ENV) cd backend && go run ./cmd/migrate down

migrate-version: check-env
	@$(WITH_ENV) cd backend && go run ./cmd/migrate version

migrate-create:
	@test -n "$(name)" || { echo "usage: make migrate-create name=add_something"; exit 1; }
	cd backend && go run ./cmd/migrate create $(name)

db-stop:
	docker compose stop

db-down:
	docker compose down

backend-build:
	cd backend && go build -o bin/server ./cmd/server

backend: check-env backend-build
	@$(WITH_ENV) exec ./$(BACKEND_BIN)

frontend: frontend/node_modules
	cd frontend && npm run dev

# Backend and frontend run as background jobs of one shell. On Ctrl+C the
# terminal already delivers SIGINT to the backend (Go re-enables it), so the
# INT trap only stops the frontend. The backend must get a single signal:
# a second one makes it exit immediately instead of shutting down gracefully.
# If either process exits on its own, the other is stopped too.
# The database keeps running.
dev: db migrate-up backend-build frontend/node_modules
	@echo "Backend :8080, frontend http://localhost:5173 - Ctrl+C stops both (database keeps running)"
	@($(WITH_ENV) exec ./$(BACKEND_BIN)) & be=$$!; \
	(cd frontend && exec npm run dev) & fe=$$!; \
	stopping=; \
	trap 'stopping=1; kill -TERM $$fe 2>/dev/null' INT; \
	trap 'stopping=1; kill -TERM $$be $$fe 2>/dev/null' TERM; \
	while kill -0 $$be 2>/dev/null && kill -0 $$fe 2>/dev/null; do sleep 1; done; \
	[ -n "$$stopping" ] || kill -TERM $$be $$fe 2>/dev/null; \
	wait; \
	[ -n "$$stopping" ] || { echo "backend or frontend exited unexpectedly; stopped the other one"; exit 1; }

# Integration tests skip themselves here because TEST_DATABASE_URL is unset.
# Frontend tests run twice to prove dates do not depend on the machine's zone.
test: frontend/node_modules
	cd backend && go vet ./... && go test ./...
	cd frontend && npm run build && npm test && TZ=UTC npm test

# Each test creates and drops its own mogumogu_test_* database on the compose
# server, so development data is never touched.
test-integration: check-env db
	@$(WITH_ENV) cd backend && \
	TEST_DATABASE_URL="postgres://$$DB_USER:$$DB_PASSWORD@$$DB_HOST:$$DB_PORT/$$DB_NAME?sslmode=$$DB_SSLMODE" \
	go test -count=1 ./...

# Creates and drops its own mogumogu_e2e_* database; see e2e/run.sh.
test-e2e: check-env db backend-build frontend/node_modules
	./e2e/run.sh

# pg_dump runs inside the postgres container, so its version always matches
# the server and nothing but Docker is needed. The file is written under a
# temporary name first, so a failed dump never looks like a finished backup,
# and is readable only by you (it holds password hashes and personal data).
db-backup: check-env db
	@mkdir -p backups
	@umask 077; f="backups/mogumogu-$$(date +%Y%m%d-%H%M%S).dump"; \
	if docker compose exec -T postgres sh -c 'pg_dump -Fc -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"' > "$$f.partial"; then \
		mv "$$f.partial" "$$f" && echo "Backup written to $$f ($$(du -h "$$f" | awk '{print $$1}'))"; \
	else \
		rm -f "$$f.partial"; echo "Backup failed"; exit 1; \
	fi

# Container setup (compose profile `app`): database, migrations, backend and
# Nginx. It shares the postgres container and data with `make dev`.
# .env is sourced first because compose prefers variables already set in your
# shell over .env, and a JWT_SECRET exported for another project would win.
# --wait returns once every service is healthy or has exited successfully.
app-up: check-env
	@$(WITH_ENV) docker compose --profile app up -d --build --wait

# Stops the database too; the pgdata volume is kept.
app-down: check-env
	@$(WITH_ENV) docker compose --profile app down

app-logs: check-env
	@$(WITH_ENV) docker compose --profile app logs -f

frontend/node_modules: frontend/package-lock.json
	cd frontend && npm ci
	@touch $@
