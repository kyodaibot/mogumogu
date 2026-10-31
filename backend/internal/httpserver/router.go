// Package httpserver wires HTTP routes, middleware and handlers. The API
// contract is docs/api.md.
package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"mogumogu/backend/internal/auth"
	"mogumogu/backend/internal/store"
)

const readyTimeout = 2 * time.Second

// Pinger is the database check readiness needs; *sql.DB satisfies it.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// Deps are the router's collaborators. Health checks only need Logger and DB.
type Deps struct {
	Logger          *slog.Logger
	DB              Pinger
	Store           *store.Store
	Sessions        *auth.Sessions
	CSRF            *auth.CSRF
	LoginLimiter    *auth.Limiter
	RegisterLimiter *auth.Limiter
	// Messages to the meal assistant per user in a short window.
	AssistantLimiter *auth.Limiter
	AllowedOrigins   []string
	CookieSecure     bool
	// TrustedProxies are the peers whose X-Forwarded-For is believed (see
	// config.Config). Nil means ClientIP is always the TCP peer.
	TrustedProxies []string
	// The meal assistant: the AI service, the tokens its query tools use, and
	// the daily message quotas.
	Assistant       Replier
	AgentTokens     *auth.AgentTokens
	AssistantLimits DailyLimits
}

// DailyLimits are the meal assistant's daily message quotas: per user by
// account tier, and for the whole site.
type DailyLimits struct {
	Normal, Pro, Site int
}

func (l DailyLimits) forTier(t store.Tier) int {
	if t == store.TierPro {
		return l.Pro
	}
	return l.Normal
}

// InternalDeps are the internal router's collaborators.
type InternalDeps struct {
	Logger      *slog.Logger
	Store       *store.Store
	AgentTokens *auth.AgentTokens
}

type api struct {
	logger           *slog.Logger
	store            *store.Store
	sessions         *auth.Sessions
	csrf             *auth.CSRF
	loginLimiter     *auth.Limiter
	registerLimiter  *auth.Limiter
	assistantLimiter *auth.Limiter
	allowedOrigins   []string
	cookieSecure     bool

	assistant       Replier
	agentTokens     *auth.AgentTokens
	assistantLimits DailyLimits
}

func init() {
	// Release mode stops Gin from printing non-JSON debug lines to stdout.
	gin.SetMode(gin.ReleaseMode)
}

// NewRouter returns the Gin engine with all routes registered.
func NewRouter(d Deps) *gin.Engine {
	r := gin.New()
	// ClientIP keys the rate limits. It reads X-Forwarded-For only when the TCP
	// peer is a trusted proxy (Nginx, which overwrites the header with the real
	// client address); otherwise it is the TCP peer, so clients cannot spoof it.
	// X-Real-IP is never read.
	r.RemoteIPHeaders = []string{"X-Forwarded-For"}
	if err := r.SetTrustedProxies(d.TrustedProxies); err != nil {
		panic("invalid trusted proxies (config validates them): " + err.Error())
	}
	r.Use(requestLogger(d.Logger), gin.Recovery())
	// Unknown paths get the same JSON error as missing resources.
	r.NoRoute(abortNotFound)

	health := r.Group("/api/health")
	health.GET("/live", live)
	health.GET("/ready", ready(d.Logger, d.DB))

	a := &api{
		logger: d.Logger, store: d.Store, sessions: d.Sessions, csrf: d.CSRF,
		loginLimiter: d.LoginLimiter, registerLimiter: d.RegisterLimiter, assistantLimiter: d.AssistantLimiter,
		allowedOrigins: d.AllowedOrigins, cookieSecure: d.CookieSecure,
		assistant: d.Assistant, agentTokens: d.AgentTokens, assistantLimits: d.AssistantLimits,
	}
	app := r.Group("/api", a.resolveSession)

	public := app.Group("", a.csrfProtect)
	public.GET("/auth/csrf", a.getCSRF)
	public.POST("/auth/register", a.register)
	public.POST("/auth/login", a.login)

	// Authenticate first (401) so CSRF can then check the session binding (403).
	private := app.Group("", a.requireAuth, a.csrfProtect)
	private.POST("/auth/logout", a.logout)
	private.GET("/auth/me", a.me)

	private.GET("/profile", a.getProfile)
	private.PUT("/profile", a.putProfile)

	private.GET("/schedule-events", a.listEvents)
	private.POST("/schedule-events", a.createEvent)
	private.GET("/schedule-events/:id", a.getEvent)
	private.PUT("/schedule-events/:id", a.updateEvent)
	private.DELETE("/schedule-events/:id", a.deleteEvent)

	private.GET("/meal-records", a.listMeals)
	private.POST("/meal-records", a.createMeal)
	private.GET("/meal-records/:id", a.getMeal)
	private.PUT("/meal-records/:id", a.updateMeal)
	private.DELETE("/meal-records/:id", a.deleteMeal)

	private.POST("/assistant/messages", a.postAssistantMessage)
	private.POST("/assistant/proposals/:id/execute", a.executeProposal)

	return r
}

// NewInternalRouter serves the read-only API the AI service's query tools call
// (docs/api.md 9.3). It listens on its own port, which Nginx never forwards
// to and which is not published outside the container network.
func NewInternalRouter(d InternalDeps) *gin.Engine {
	r := gin.New()
	r.Use(requestLogger(d.Logger), gin.Recovery())
	r.NoRoute(abortNotFound)

	a := &api{logger: d.Logger, store: d.Store, agentTokens: d.AgentTokens}
	agent := r.Group("/internal/agent", a.agentAuth)
	agent.GET("/events", a.agentEvents)
	agent.GET("/meals", a.agentMeals)
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
