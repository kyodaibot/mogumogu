# Build from the repo root: docker build -f backend/Dockerfile .

# --- Stage 1: compile both binaries ---
FROM golang:1.26.2-alpine AS build
WORKDIR /src
# Download modules first so this layer is cached until go.mod/go.sum change.
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# CGO off gives static binaries that run on distroless/static. Migrations and
# the time zone database are embedded, so no other files are needed.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

# --- Stage 2: only the binaries, no shell, no source ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /out/migrate /
USER nonroot:nonroot
EXPOSE 8080
# compose runs `/migrate up` from the same image for the migrate service.
CMD ["/server"]
