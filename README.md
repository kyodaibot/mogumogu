# mogumogu

A calendar-centred meal planner. You log your schedule and what you eat on a calendar, and a meal assistant suggests your next meal based on your profile, your day and your recent meals.

**Live: https://mogumogu.teiyou416.ninja** (AWS Tokyo region). The interface is in Japanese and English.

The design documents are in Chinese: long-term goals and shared conventions in [docs/project-review.md](docs/project-review.md), the API contract in [docs/api.md](docs/api.md), operations in [docs/operations.md](docs/operations.md), and the scope of each phase in [plans/](plans/).

## What it does

- Sign up, log in and out; fill in a profile (body data, goal, allergens).
- Record schedule events and meals on a calendar. Each day shows a meal tray, so a missing meal stands out.
- Meal assistant (bottom right of the calendar): suggests three options for a meal, or answers questions about your own records. It runs on Claude (Amazon Bedrock) with a LangGraph agent that may query older records through the Go backend. Single-turn for now; multi-turn chat is phase 4b.
- Japanese and English interface. The first visit follows the browser language; the header switch remembers your choice. The assistant answers in the interface language.
- Data in PostgreSQL, isolated per user. Cookie sessions with CSRF protection and login rate limits.

## How it is built and run

| Part | Technology |
|---|---|
| Frontend | Vue 3, TypeScript, Vite |
| Backend | Go, Gin, GORM, golang-migrate |
| AI service | Python, uv, FastAPI, LangGraph, Amazon Bedrock (Claude Haiku) |
| Database | PostgreSQL 17 |
| Local | Docker Compose |
| Cloud | AWS: EC2 (Docker Compose, Caddy for HTTPS), RDS PostgreSQL, ECR, SSM; all in Terraform (`infra/`) |
| CI/CD | GitHub Actions: tests on every push and PR; a merge to `master` that passes every check is deployed automatically (OIDC, no stored keys) |

During development PostgreSQL runs in Docker Compose, while the Go backend, the AI service and the Vue frontend run on the host. The differences between the two ways of running, the security design, backup and restore, and cloud deployment are described in [docs/operations.md](docs/operations.md).

## Repository layout

```
compose.yaml            # PostgreSQL; the "app" profile adds migrate, backend, ai and web (Nginx)
.env.example            # Example environment (copy to .env)
Makefile                # Common commands; run `make` to list them
.dockerignore           # Keeps .env, dependencies and build output out of images
.github/workflows/      # CI and deployment (GitHub Actions)
backend/                # Go backend (Gin, GORM, log/slog)
  Dockerfile            # Multi-stage; runs as non-root on distroless
  cmd/server/           # Backend entry point
  cmd/migrate/          # Database migration command
  migrations/           # Numbered SQL migrations
  internal/auth/        # Password hashing, session JWTs, CSRF tokens, rate limits
  internal/config/      # Reading and checking the environment
  internal/database/    # Connection pool and migrations
  internal/httpserver/  # Routes, middleware and handlers
  internal/store/       # Data access (every query is filtered by user)
  internal/tz/          # Asia/Tokyo dates and instants
  internal/testdb/      # Throwaway databases for integration tests
frontend/               # Vue 3 + TypeScript + Vite + vue-router
  Dockerfile            # Builds the static files into an Nginx image
  nginx.conf            # Static files, SPA fallback, /api reverse proxy
  src/api/              # API client (CSRF, errors) and types
  src/i18n/             # Japanese and English text, language detection and switching
  src/pages/            # Login, profile, calendar
  src/components/       # Event and meal forms, the assistant chat panel, the language switch
  src/lib/              # Dates (Asia/Tokyo), meal types, error messages
ai/                     # The meal assistant's AI service (Python, uv, FastAPI, LangGraph)
  Dockerfile            # Multi-stage; runtime dependencies only, non-root
  src/mogumogu_ai/      # Agent loop, query tools, prompt, Bedrock calls
  tests/                # pytest (fake model only)
  evals/                # Checks against the real model (make ai-eval)
infra/                  # Terraform: bootstrap/ (state bucket), main/ (network, EC2, RDS, ECR, IAM, budget)
deploy/                 # Production compose file, Caddyfile and deploy.sh run on the server
scripts/                # put-secrets.sh (production secrets), seed-demo.mjs (demo data)
docs/  plans/           # Design documents and phase plans (Chinese)
backups/                # Output of make db-backup (not committed)
```

## Prerequisites

| Tool | Version | Used for |
|---|---|---|
| Docker (with Compose v2) | Docker Desktop or Docker Engine | PostgreSQL; running everything in containers; backups |
| Go | 1.26.2 or later | Backend, migrations |
| Node.js / npm | Node 24 (see `frontend/.nvmrc`) | Frontend |
| openssl | The system one | `make setup` generates secrets with it |
| uv | 0.12 or later | The AI service's Python environment (uv installs Python 3.14) |
| AWS CLI v2 | 2.32 or later (needs `aws login`) | Temporary credentials for Bedrock |
| pre-commit | 4.x | Runs gitleaks before each commit |
| gitleaks (optional) | Same as `rev` in `.pre-commit-config.yaml`, currently 8.30.1 | Scanning the whole tree or Git history by hand |

The first time the pre-commit gitleaks hook runs, it builds gitleaks with your local Go, so installing the gitleaks CLI separately is not required.

## First-time setup

1. Install pre-commit (once per clone):

   ```sh
   uv tool install pre-commit   # or: brew install pre-commit
   ```

2. Initialise:

   ```sh
   make setup
   ```

   - If there is no `.env` yet, it is created from `.env.example` with random `JWT_SECRET` and `CSRF_SECRET` values.
   - Runs `npm ci` for the frontend and `pre-commit install`.

3. Open `.env` and set `DB_PASSWORD` to a value of your own.

   - `.env` is ignored by Git. **Never commit it.**
   - The same `.env` is read by `docker compose` and by the shell (`source`), so **values must not contain spaces, quotes, `$` or `#`**. `DB_PASSWORD` also goes into the test database URL; letters, digits, `-` and `_` are safest.
   - If another PostgreSQL already uses port 5432, set `DB_PORT` to another port, such as 5433.
   - If you copy `.env.example` by hand instead of using make, replace both secrets with separate outputs of `openssl rand -base64 48`. The placeholders are deliberately too short, so the backend refuses to start with them.

4. The meal assistant calls Amazon Bedrock and needs temporary AWS credentials (no access keys):

   ```sh
   aws login --profile dev        # Log in in the browser; region ap-northeast-1. Credentials last up to about 12 hours.
   ```

   Then add `AWS_PROFILE=dev` (your profile name) to `.env`. When the credentials expire, the assistant reports that it is unavailable; run `aws login` again and restart the AI service. To work on the interface without the real model, set `MODEL_PROVIDER=fake` in `.env`: the AI service then returns fixed test replies.

## Running locally

### With make (recommended)

```sh
make dev
```

This starts the database, applies pending migrations, then runs the backend, the AI service and the frontend together. Open **http://localhost:5173**. On first use: **sign up → fill in the profile → calendar**. One Ctrl+C stops the three processes; the database keeps running.

| Command | What it does |
|---|---|
| `make dev` | Database + migrations + backend + AI service + frontend |
| `make db` | Database only (in the background; returns once healthy) |
| `make backend` | Build and run the backend only (foreground, :8080) |
| `make frontend` | Frontend dev server only (foreground, :5173) |
| `make ai` | AI service only (foreground, 127.0.0.1:8090) |
| `make ai-eval` | Run the assistant's evaluation cases against the real model (a few cents per run) |
| `make migrate-up` | Apply all pending migrations |
| `make migrate-down` | Roll back the latest migration (one step) |
| `make migrate-version` | Show the current migration version |
| `make migrate-create name=add_xxx` | Create an empty pair of migration files |
| `make test` | Unit tests and builds (no database needed) |
| `make test-integration` | Backend tests against PostgreSQL (including integration tests) |
| `make test-e2e` | Browser test of the whole UI in headless Chrome (throwaway database) |
| `make setup` | First-time setup, see above |
| `make db-stop` / `make db-down` | Stop the database / remove its container (data is kept either way) |
| `make app-up` / `make app-down` / `make app-logs` | Start / stop / follow logs of the container setup, see below |
| `make db-backup` | Back up the database, see "Backup and restore" |
| `make push` / `make deploy` | Manual release to AWS; normally CI does this, see [docs/operations.md](docs/operations.md#6-云上部署aws) |

- `make` with no target lists every command.
- In `make dev`, if any process exits unexpectedly, the others are stopped and make fails.
- To work on one part only: run `make db` and `make migrate-up`, then `make backend` or `make frontend` in separate terminals.
- The backend does **not** migrate on start. Migrations are a separate step (`make migrate-up` or `make dev`).
- There is deliberately **no** make target that deletes data; run `docker compose down -v` yourself if you mean it.

### By hand

Use three terminals, each starting at the repository root.

**1. Database and migrations**

```sh
docker compose up -d
docker compose ps        # wait for STATUS (healthy)
(set -a; source .env; set +a; cd backend && go run ./cmd/migrate up)
```

**2. Backend** (listens on http://localhost:8080 by default)

```sh
cd backend
go build -o bin/server ./cmd/server
(set -a; source ../.env; set +a; ./bin/server)
```

- Running inside a subshell `( … )` keeps the `.env` variables out of your terminal.
- Logs are JSON on stdout and never contain passwords, tokens, cookies or request bodies. With a missing or invalid variable, the backend lists the problems (names only, never values) and exits.
- `(set -a; source ../.env; set +a; go run ./cmd/server)` also works during development. After Ctrl+C, `go run` itself prints `exit status 1`; that is `go run`, not a backend error.

**3. Frontend** (http://localhost:5173)

```sh
cd frontend
npm run dev
```

The dev server proxies `/api` to `API_PROXY_TARGET` (set in the root `.env`; default `http://localhost:8080`).

### In containers (Docker + Nginx)

After the first-time setup (this mode needs only Docker and `.env`):

```sh
make app-up     # builds the images and starts everything; returns once all services are healthy
```

Open **http://localhost:8000**. Start order: database → migrations (the `migrate` container runs and exits) → backend → Nginx; the AI service (`ai`) starts alongside.

- Only Nginx is published, on `127.0.0.1:8000` (change with `WEB_PORT` in `.env`). The backend and the AI service have no published ports and are reached only through Nginx. Nginx does not forward the backend's internal API (8081), which only the AI service uses.
- The AI service uses your `aws login` credentials: `~/.aws/config` is mounted read-only and `~/.aws/login` (refreshed tokens) writable. Credentials never go into an image.
- It shares the database container and its data with `make dev`; both can run at the same time.
- After changing code, run `make app-up` again to rebuild the images.
- `make app-logs` follows all container logs (Ctrl+C stops following, not the services).

## Stopping

- **Backend, frontend**: Ctrl+C in their terminals. The backend stops accepting requests, lets running ones finish (up to 10 seconds), closes the connection pool and exits.
- **Database**:

  | Command | Effect |
  |---|---|
  | `docker compose stop` | Stops the container; `docker compose start` resumes it |
  | `docker compose down` | Removes the container and network, **keeps the data** (named volume `pgdata`) |
  | `docker compose down -v` | Also removes the volume: **the data is gone for good** |

  A volume is not a backup.

- **Containers**: `make app-down` removes every container (**including the database**); the data stays in the volume. `make dev` starts the database again on its own; when running the backend alone, run `make db` first.

## Testing

```sh
make test               # go vet, go test, pytest, npm run build (with type checks), npm test (default zone + TZ=UTC)
make test-integration   # needs the database running; includes the integration tests below
make test-e2e           # browser end-to-end test, see below
```

- **Integration tests**: each test **creates its own database** `mogumogu_test_*` on the compose PostgreSQL, migrates it and drops it afterwards, so development data is never touched. Without `TEST_DATABASE_URL` (for example a plain `go test ./...`) they are skipped with a note. They cover user isolation, persistence, events across midnight, null versus 0 in nutrition, token revocation on logout, CSRF bound to the session, login rate limits, and the assistant's context, language and quota.
- **AI service tests** (pytest): only a scripted fake model, no AWS. They cover every path of the agent loop (step limit, forced finish, one validation retry, allergen check, at most 6 model calls), the query tools' date limits and error handling, and the reply language setting.
- **Evaluation** (`make ai-eval`, not part of `make test`): 7 cases against the real model, in Japanese and English (allergens, no records, more than 31 days of records, an off-topic request, no meal given, allergens typed in Japanese with an English interface, a Japanese question with an English interface). Each reply, its step count, tokens and time are printed, and the reply language is checked.
- **Frontend tests** (Vitest): the calendar grid (weeks start on Monday), Asia/Tokyo date arithmetic, date formats and assistant questions in both languages, language detection, matching Japanese and English dictionaries, and the API client reading the latest CSRF token for every write.
- **End-to-end test** (`make test-e2e`, in `e2e/`): headless Chrome with a Japanese browser language goes through the whole flow: sign-up, profile, calendar create/edit/delete, data after a reload, logout and login again. It also checks CSRF token rotation, the Origin header, and the assistant (preset questions give 3 cards, a new date adds a divider and keeps earlier messages, each message carries its own date and language, a reload starts a new conversation). Finally it switches to English and checks that the interface and the assistant follow, and that the choice survives a reload.
  - The test **creates and removes its own data**: a throwaway database `mogumogu_e2e_*` on the compose PostgreSQL, with the backend, the AI service (fake model, no AWS) and Vite on their own ports (18080 plus internal 18081, 18090, 15173). Whether it passes, fails or is interrupted with Ctrl+C, the processes are stopped and the database dropped. Development data is never read or changed, and a running `make dev` is not disturbed.
  - Needs Google Chrome or Chromium (found automatically on macOS; elsewhere set `CHROME=/path/to/chrome`), Node 24 or later and uv. No extra npm packages.

### CI/CD

On every push to `dev` or `master` and every PR into them, GitHub Actions runs 5 jobs: a secret scan of the full history, backend tests (with PostgreSQL, so the integration tests really run), frontend build and tests, AI service tests (fake model, no AWS credentials) and builds of the three images. A push to `master` that passes all five is then deployed by a sixth job, with temporary AWS credentials from OIDC. The configuration is in `.github/workflows/ci.yml`; see [docs/operations.md](docs/operations.md#4-cigithub-actions).

Workflow: branch from `dev` → merge back into `dev` → open a PR from `dev` to `master` → merge once every check passes; that merge is the release.

### Health checks

| Endpoint | Meaning |
|---|---|
| `GET /api/health/live` | Does not touch the database. `200 {"status":"ok"}` while the process runs |
| `GET /api/health/ready` | Pings the database within 2 seconds. `200 {"status":"ready"}` or `503 {"status":"unavailable"}` |

```sh
curl -i http://localhost:8080/api/health/ready
```

### Calling authenticated endpoints with curl

Writes (POST, PUT, DELETE) need an `Origin` header and the CSRF token (docs/api.md 2.4). For example:

```sh
B=http://localhost:5173 J=/tmp/mogumogu-cookies.txt
csrf() { awk '$6=="mogumogu_csrf"{v=$7} END{print v}' "$J"; }
curl -s -c "$J" -b "$J" "$B/api/auth/csrf"
curl -s -c "$J" -b "$J" -X POST "$B/api/auth/login" \
  -H 'Origin: http://localhost:5173' -H "X-CSRF-Token: $(csrf)" -H 'Content-Type: application/json' \
  -d '{"username":"your_name","password":"your password"}'
curl -s -b "$J" "$B/api/meal-records?from=2026-10-01&to=2026-10-31"
```

The server issues a new CSRF token after login, so read the cookie again for every request (as the `csrf` function does).

### Secret scanning

- On `git commit`, pre-commit runs gitleaks over the **staged changes** and blocks the commit if it finds something that looks like a secret. **Do not skip it with `--no-verify`**, and do not change the gitleaks configuration or add ignore rules to get past it; check a false positive with the team first.
- To check staged changes without committing: `pre-commit run gitleaks`. Note that `pre-commit run --all-files` also checks only what is staged; it does not scan the whole tree.
- A full scan needs the gitleaks CLI:

  ```sh
  gitleaks dir --redact .        # the working tree
  gitleaks git --redact .        # the full Git history
  ```

- A secret that reached Git history (including pushed commits) must be revoked and replaced at once. Deleting the file or rewriting history is not enough.
- If a local `JWT_SECRET` or `CSRF_SECRET` leaks (pasted into a chat, say), generate a new one with `openssl rand -base64 48`. All existing sessions end.

## Backup and restore

```sh
make db-backup   # writes backups/mogumogu-<time>.dump
```

- A backup holds personal data and password hashes. `backups/` is ignored by Git and the files are readable only by you. **Never commit or publish them.**
- For a backup that matches the data exactly, stop the app first.
- The restore drill (into a throwaway database, leaving the development database alone) and a real restore (overwrites data; by hand only) are in [docs/operations.md](docs/operations.md#5-备份与恢复).

## Troubleshooting

- **The backend says `JWT_SECRET must be at least 32 bytes`**: `.env` still has the placeholder; generate the secrets as in "First-time setup".
- **Sign-up or login shows a server error, and the backend log mentions `relation "users" does not exist`**: migrations have not run; run `make migrate-up`.
- **Writes return `403 csrf_failed`**: usually the `Origin` is not in `ALLOWED_ORIGINS`, for example when you open `127.0.0.1` but only `localhost` is allowed. Use http://localhost:5173, or add the address you use to `ALLOWED_ORIGINS`.
- **The meal assistant is suddenly unavailable**: the `aws login` credentials have probably expired. Check with `aws sts get-caller-identity --profile dev`, log in again and restart `make dev`.
- **`docker compose up` reports the port in use**: something else listens on 5432; change `DB_PORT` in `.env`.
- **When the backend stops, the Vite terminal shows `http proxy error … ECONNREFUSED`**: expected; the page says the server is temporarily unavailable.
- **`docker compose` reports `required variable … is missing a value`**: `.env` lacks that variable; add it as in `.env.example`.
- **In containers the backend fails with `JWT_SECRET must be at least 32 bytes`, although `.env` is fine**: a variable of the same name is exported in your shell, and compose prefers it. Use `make app-up`, which reads `.env` first. Other container issues are in [docs/operations.md](docs/operations.md#7-常见问题).
- **`npm ci` / `npm install` mentions `allow-scripts … fsevents`**: harmless. `fsevents` is an optional macOS dependency of Vite that ships prebuilt; skipping its install script affects nothing.
