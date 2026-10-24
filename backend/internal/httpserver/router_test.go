package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// fakePinger stands in for the database; ping decides the outcome.
type fakePinger struct {
	ping func(ctx context.Context) error
}

func (f fakePinger) PingContext(ctx context.Context) error { return f.ping(ctx) }

func pingOK() Pinger {
	return fakePinger{ping: func(context.Context) error { return nil }}
}

func get(t *testing.T, r http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func statusField(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	return body["status"]
}

func TestLiveDoesNotTouchDatabase(t *testing.T) {
	db := fakePinger{ping: func(context.Context) error {
		t.Error("live must not ping the database")
		return nil
	}}
	w := get(t, NewRouter(Deps{Logger: discardLogger(), DB: db}), "/api/health/live")

	if w.Code != http.StatusOK || statusField(t, w) != "ok" {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestReady(t *testing.T) {
	const secretDetail = "dial postgres://app:test-placeholder-pw@db:5432 refused"

	tests := []struct {
		name       string
		ping       func(ctx context.Context) error
		wantCode   int
		wantStatus string
	}{
		{
			name:       "database available",
			ping:       func(context.Context) error { return nil },
			wantCode:   http.StatusOK,
			wantStatus: "ready",
		},
		{
			name:       "database unavailable",
			ping:       func(context.Context) error { return errors.New(secretDetail) },
			wantCode:   http.StatusServiceUnavailable,
			wantStatus: "unavailable",
		},
		{
			name: "ping bounded by timeout",
			ping: func(ctx context.Context) error {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > readyTimeout {
					return errors.New("ping context has no deadline within readyTimeout")
				}
				// Behave like a database that never answers.
				<-ctx.Done()
				return ctx.Err()
			},
			wantCode:   http.StatusServiceUnavailable,
			wantStatus: "unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := get(t, NewRouter(Deps{Logger: discardLogger(), DB: fakePinger{ping: tt.ping}}), "/api/health/ready")

			if w.Code != tt.wantCode || statusField(t, w) != tt.wantStatus {
				t.Fatalf("got %d %s, want %d %q", w.Code, w.Body.String(), tt.wantCode, tt.wantStatus)
			}
			if strings.Contains(w.Body.String(), "postgres://") || strings.Contains(w.Body.String(), "refused") {
				t.Fatalf("response exposes internal error: %s", w.Body.String())
			}
		})
	}
}

func TestRequestLogger(t *testing.T) {
	var buf bytes.Buffer
	r := NewRouter(Deps{Logger: slog.New(slog.NewJSONHandler(&buf, nil)), DB: pingOK()})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/health/live?token=secret", nil))

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log line is not JSON: %v (%q)", err, buf.String())
	}
	if entry["method"] != "GET" || entry["path"] != "/api/health/live" || entry["status"] != float64(200) {
		t.Fatalf("unexpected log entry: %v", entry)
	}
	if _, ok := entry["duration_ms"]; !ok {
		t.Fatalf("log entry has no duration_ms: %v", entry)
	}
	if bytes.Contains(buf.Bytes(), []byte("secret")) {
		t.Fatal("query string leaked into the request log")
	}
}

func TestUnknownPathIsJSON404(t *testing.T) {
	w := get(t, NewRouter(Deps{Logger: discardLogger(), DB: pingOK()}), "/api/no-such-thing")

	var body struct {
		Error errorBody `json:"error"`
	}
	if w.Code != http.StatusNotFound || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Error.Code != "not_found" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
}

func TestClientIP(t *testing.T) {
	privateRanges := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	tests := []struct {
		name    string
		trusted []string
		peer    string
		headers map[string]string
		want    string
	}{
		{
			name:    "no trusted proxies ignores X-Forwarded-For",
			peer:    "172.18.0.5:40000",
			headers: map[string]string{"X-Forwarded-For": "203.0.113.7"},
			want:    "172.18.0.5",
		},
		{
			name:    "trusted proxy forwards the client address",
			trusted: privateRanges,
			peer:    "172.18.0.5:40000",
			headers: map[string]string{"X-Forwarded-For": "203.0.113.7"},
			want:    "203.0.113.7",
		},
		{
			name:    "untrusted peer cannot spoof X-Forwarded-For",
			trusted: privateRanges,
			peer:    "203.0.113.9:40000",
			headers: map[string]string{"X-Forwarded-For": "198.51.100.1"},
			want:    "203.0.113.9",
		},
		{
			name:    "X-Real-IP is never read",
			trusted: privateRanges,
			peer:    "172.18.0.5:40000",
			headers: map[string]string{"X-Real-IP": "198.51.100.1"},
			want:    "172.18.0.5",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRouter(Deps{Logger: discardLogger(), DB: pingOK(), TrustedProxies: tt.trusted})
			r.GET("/test/ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })

			req := httptest.NewRequest(http.MethodGet, "/test/ip", nil)
			req.RemoteAddr = tt.peer
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if got := w.Body.String(); got != tt.want {
				t.Fatalf("ClientIP = %q, want %q", got, tt.want)
			}
		})
	}
}
