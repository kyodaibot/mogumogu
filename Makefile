# Local development shortcuts. Run `make` to list targets.
# Every target is a thin wrapper around the commands documented in README.md.

SHELL := /bin/bash
.DEFAULT_GOAL := help

BACKEND_BIN := backend/bin/server
# Export the repo-root .env for the command that follows. Each recipe line runs
# in its own shell, so the variables never reach your terminal.
WITH_ENV := set -a; . ./.env; set +a;

.PHONY: help setup check-env db db-stop db-down backend backend-build frontend dev test

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "  setup      First-time setup: create .env if missing, npm ci, pre-commit install"
	@echo "  dev        Start database, then backend + frontend together (Ctrl+C stops both)"
	@echo "  db         Start PostgreSQL in the background and wait until healthy"
	@echo "  backend    Build and run the Go backend in the foreground (:8080)"
	@echo "  frontend   Run the Vite dev server in the foreground (http://localhost:5173)"
	@echo "  test       go vet, go test, npm run build"
	@echo "  db-stop    Stop the database container (data kept)"
	@echo "  db-down    Remove the database container (data kept in the pgdata volume)"
	@echo ""
	@echo "Deleting data is deliberately not a target: run 'docker compose down -v' yourself."

setup: frontend/node_modules
	@if [ -f .env ]; then \
		echo ".env already exists, leaving it unchanged"; \
	else \
		cp .env.example .env && echo "Created .env from .env.example - edit DB_PASSWORD before starting"; \
	fi
	pre-commit install

check-env:
	@test -f .env || { echo "Missing .env - run 'make setup' (or: cp .env.example .env)"; exit 1; }

db: check-env
	docker compose up -d --wait

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
dev: db backend-build frontend/node_modules
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

test: frontend/node_modules
	cd backend && go vet ./... && go test ./...
	cd frontend && npm run build

frontend/node_modules: frontend/package-lock.json
	cd frontend && npm ci
	@touch $@
