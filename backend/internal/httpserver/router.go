// Package httpserver wires HTTP routes and middleware.
package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const readyTimeout = 2 * time.Second

// Pinger is the database check readiness needs; *sql.DB satisfies it.
type Pinger interface {
	PingContext(ctx context.Context) error
}

func init() {
	// Release mode stops Gin from printing non-JSON debug lines to stdout.
	gin.SetMode(gin.ReleaseMode)
}

// NewRouter returns the Gin engine with all routes registered.
func NewRouter(logger *slog.Logger, db Pinger) *gin.Engine {
	r := gin.New()
	r.Use(requestLogger(logger), gin.Recovery())

	health := r.Group("/api/health")
	health.GET("/live", live)
	health.GET("/ready", ready(logger, db))

	return r
}

func live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ready reports whether the database answers a ping within readyTimeout.
// Error details go to the log only, never to the response.
func ready(logger *slog.Logger, db Pinger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), readyTimeout)
		defer cancel()

		if err := db.PingContext(ctx); err != nil {
			logger.Warn("readiness check failed", slog.String("error", err.Error()))
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}

// requestLogger logs one line per request. Only the path is logged, not the
// query string, so query parameters never reach the logs.
func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info("request",
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", c.Writer.Status()),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
		)
	}
}
