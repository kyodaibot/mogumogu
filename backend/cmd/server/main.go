package main

import (
	"context"
	"errors"
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
