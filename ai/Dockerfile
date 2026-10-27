# Build from the repo root: docker build -f ai/Dockerfile .

FROM ghcr.io/astral-sh/uv:0.12.23 AS uv

# --- Stage 1: install the locked runtime dependencies into /app/.venv ---
FROM python:3.14.7-slim AS build
COPY --from=uv /uv /usr/local/bin/uv
# Use the image's Python, never download one; copy files instead of hard
# links so the venv can be copied to the next stage.
ENV UV_PYTHON_DOWNLOADS=never UV_LINK_MODE=copy UV_COMPILE_BYTECODE=1
WORKDIR /app
# Dependencies first, so this layer is cached until the lock file changes.
COPY ai/pyproject.toml ai/uv.lock ai/.python-version ./
RUN uv sync --locked --no-dev --no-install-project
COPY ai/src ./src
RUN uv sync --locked --no-dev --no-editable

# --- Stage 2: the venv only, no uv, no tests, run as a normal user ---
FROM python:3.14.7-slim
# A home directory, because compose mounts the AWS config into ~/.aws.
RUN useradd --create-home --uid 10001 app
COPY --from=build /app/.venv /app/.venv
ENV PATH=/app/.venv/bin:$PATH PYTHONUNBUFFERED=1
USER app
EXPOSE 8090
CMD ["uvicorn", "mogumogu_ai.app:app", "--host", "0.0.0.0", "--port", "8090", "--no-access-log"]
