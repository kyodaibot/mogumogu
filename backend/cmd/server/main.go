package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"mogumogu/backend/internal/auth"
	"mogumogu/backend/internal/config"
	"mogumogu/backend/internal/database"
	"mogumogu/backend/internal/httpserver"
	"mogumogu/backend/internal/store"
)

const shutdownTimeout = 10 * time.Second

func main() {
	// `server healthcheck` is for container health checks: the distroless
	// image has no shell or curl.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("shutdown complete")
}

func run(logger *slog.Logger) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	gormDB, sqlDB, err := database.Open(cfg.DB)
	if err != nil {
		return err
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			logger.Error("closing database pool failed", slog.String("error", err.Error()))
			return
		}
		logger.Info("database pool closed")
	}()
	logger.Info("database pool configured", slog.Any("db", cfg.DB))
	logger.Info("auth configured", slog.Any("auth", cfg.Auth))
	logger.Info("client ip configured", slog.Any("trusted_proxies", cfg.TrustedProxies))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr: net.JoinHostPort("", strconv.Itoa(cfg.AppPort)),
		Handler: httpserver.NewRouter(httpserver.Deps{
			Logger:          logger,
			DB:              sqlDB,
			Store:           store.New(gormDB),
			Sessions:        auth.NewSessions(cfg.Auth.JWTSecret, cfg.Auth.JWTIssuer, cfg.Auth.JWTAudience, cfg.Auth.JWTTTL),
			CSRF:            auth.NewCSRF(cfg.Auth.CSRFSecret),
			LoginLimiter:    auth.NewLimiter(5, 15*time.Minute),
			RegisterLimiter: auth.NewLimiter(10, time.Hour),
			AllowedOrigins:  cfg.Auth.AllowedOrigins,
			CookieSecure:    cfg.Auth.CookieSecure,
			TrustedProxies:  cfg.TrustedProxies,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("http server starting", slog.Int("port", cfg.AppPort))
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	// Restore default signal handling so a second Ctrl+C exits immediately.
	stop()

	logger.Info("shutdown signal received, draining in-flight requests",
		slog.String("timeout", shutdownTimeout.String()))
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	logger.Info("http server stopped")
	return nil
}

// healthcheck asks the running server's liveness endpoint and returns the
// process exit code: 0 when it answers 200, 1 otherwise. It reads only
// APP_PORT, so it needs none of the other settings.
func healthcheck() int {
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = strconv.Itoa(config.DefaultAppPort)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", port) + "/api/health/live")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}
