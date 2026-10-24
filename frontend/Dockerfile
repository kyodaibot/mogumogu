# Build from the repo root: docker build -f frontend/Dockerfile .

# --- Stage 1: build the static site ---
FROM node:24-alpine AS build
WORKDIR /app/frontend
# Install dependencies first so this layer is cached until the lockfile changes.
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# --- Stage 2: Nginx serves the files and proxies /api ---
FROM nginx:1.28-alpine
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/frontend/dist /usr/share/nginx/html
