# syntax=docker/dockerfile:1
# Build from the repo root: docker build -f importer/Dockerfile .
# The cafeteria menu importer with its scheduler: Digdag runs menus.dig, which
# starts jobs.py once a minute (plans/menu-importer-deploy.md).

FROM ghcr.io/astral-sh/uv:0.12.23 AS uv
# Digdag 0.10 supports Java 8 or 11 (digdag-docs getting_started.md).
FROM eclipse-temurin:11.0.32.1_1-jre AS java

# --- Stage 1: install the locked runtime dependencies into /app/.venv ---
FROM python:3.14.7-slim AS build
COPY --from=uv /uv /usr/local/bin/uv
ENV UV_PYTHON_DOWNLOADS=never UV_LINK_MODE=copy UV_COMPILE_BYTECODE=1
WORKDIR /app
COPY importer/pyproject.toml importer/uv.lock ./
RUN uv sync --locked --no-dev --no-install-project

# --- Stage 2: Python venv, a Java runtime and Digdag, run as a normal user ---
FROM python:3.14.7-slim
RUN useradd --create-home --uid 10001 app
COPY --from=java /opt/java/openjdk /opt/java/openjdk
# GitHub publishes no checksum for this release; the hash pins the jar
# downloaded on 2026-11-23 so a changed file fails the build.
ADD --chmod=644 --checksum=sha256:4d1337d777bd334c2348e07ebc1795283aa50a2fd3b7d720cba5ee33b5486aa8 \
    https://github.com/treasure-data/digdag/releases/download/v0.10.5.1/digdag-0.10.5.1.jar /opt/digdag.jar
COPY --from=build /app/.venv /app/.venv
COPY importer/jobs.py importer/sources.py importer/menu_listing.py importer/menus.dig /app/
# Digdag keeps its working files in the project directory.
RUN chown app /app
ENV PATH=/app/.venv/bin:/opt/java/openjdk/bin:$PATH PYTHONUNBUFFERED=1 TZ=Asia/Tokyo
USER app
WORKDIR /app
# A small, fixed heap and the serial collector keep Digdag within the memory
# limit in deploy/compose.yaml; the schedule's state is in memory only, as the
# job table in PostgreSQL decides what is due. The in-memory database keeps
# every past run, and Digdag has no setting to drop them, so a full heap exits
# the JVM; compose restarts it and the next minute carries on.
CMD ["java", "-Xmx160m", "-Xss512k", "-XX:+UseSerialGC", "-XX:MaxMetaspaceSize=96m", "-XX:+ExitOnOutOfMemoryError", \
     "-jar", "/opt/digdag.jar", "scheduler", "--project", "/app", "--max-task-threads", "1"]
