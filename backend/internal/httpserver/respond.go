package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// maxBodyBytes is the request body limit from docs/api.md 1.1.
const maxBodyBytes = 64 << 10

// fieldError is one entry of error.details (docs/api.md 1.6).
type fieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

type errorBody struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Details []fieldError `json:"details,omitempty"`
}

func abortError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": errorBody{Code: code, Message: message}})
}

func abortValidation(c *gin.Context, details []fieldError) {
	c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{"error": errorBody{
		Code: "validation_failed", Message: "Request validation failed.", Details: details,
	}})
}

func abortRateLimited(c *gin.Context, retryAfter time.Duration) {
	c.Header("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
	abortError(c, http.StatusTooManyRequests, "rate_limited", "Too many requests.")
}

// abortQuotaExceeded answers a message over the daily quota of the meal
// assistant; Retry-After points at the next day.
func abortQuotaExceeded(c *gin.Context, retryAfter time.Duration) {
	c.Header("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
	abortError(c, http.StatusTooManyRequests, "quota_exceeded", "Daily quota exceeded.")
}

func abortNotFound(c *gin.Context) {
	abortError(c, http.StatusNotFound, "not_found", "Resource not found.")
}

// abortInternal logs the cause and returns a generic 500. The response never
// carries internal details.
func abortInternal(c *gin.Context, logger *slog.Logger, err error) {
	logger.Error("request failed", slog.String("path", c.Request.URL.Path), slog.String("error", err.Error()))
	abortError(c, http.StatusInternalServerError, "internal_error", "Internal server error.")
}

// decodeJSON reads exactly one JSON object into dst, rejecting unknown fields,
// trailing data and bodies over maxBodyBytes. It writes the error response and
// returns false on failure.
func decodeJSON(c *gin.Context, dst any) bool {
	return decodeJSONLimit(c, dst, maxBodyBytes)
}

// decodeJSONLimit is decodeJSON with a body limit of its own, for the few
// requests that may be larger.
func decodeJSONLimit(c *gin.Context, dst any, limit int64) bool {
	body := http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()

	err := dec.Decode(dst)
	if err == nil {
		if dec.Decode(&struct{}{}) != io.EOF {
			err = errors.New("request body must contain a single JSON object")
		}
	}
	if err == nil {
		return true
	}

	var tooLarge *http.MaxBytesError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &tooLarge):
		abortError(c, http.StatusRequestEntityTooLarge, "payload_too_large", "Request body is too large.")
		return false
	case errors.Is(err, io.EOF):
		err = errors.New("request body is empty")
	case errors.As(err, &typeErr):
		err = fmt.Errorf("field %q has the wrong type", typeErr.Field)
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		err = fmt.Errorf("unknown field %s", strings.TrimPrefix(err.Error(), "json: unknown field "))
	}
	abortError(c, http.StatusBadRequest, "invalid_request", "Invalid request body: "+err.Error())
	return false
}

// validator collects field errors so a response can report all of them.
type validator struct {
	errs []fieldError
}

func (v *validator) add(field, reason string) {
	v.errs = append(v.errs, fieldError{Field: field, Reason: reason})
}

func (v *validator) failed() bool { return len(v.errs) > 0 }

// text validates a required string after trimming whitespace.
func (v *validator) text(field string, p *string, minLen, maxLen int) string {
	if p == nil {
		v.add(field, "required")
		return ""
	}
	s := strings.TrimSpace(*p)
	v.length(field, s, minLen, maxLen)
	return s
}

// optionalText validates a nullable string; blank becomes nil.
func (v *validator) optionalText(field string, p *string, maxLen int) *string {
	if p == nil {
		return nil
	}
	s := strings.TrimSpace(*p)
	if s == "" {
		return nil
	}
	v.length(field, s, 1, maxLen)
	return &s
}

func (v *validator) length(field, s string, minLen, maxLen int) {
	n := utf8.RuneCountInString(s)
	switch {
	case n == 0 && minLen > 0:
		v.add(field, "required")
	case n < minLen:
		v.add(field, "too_short")
	case n > maxLen:
		v.add(field, "too_long")
	}
}

// decimal validates a number with at most one decimal place in [lo, hi].
// required=false allows nil (meaning unknown).
func (v *validator) decimal(field string, p *float64, lo, hi float64, required bool) *float64 {
	if p == nil {
		if required {
			v.add(field, "required")
		}
		return nil
	}
	x := *p
	switch {
	case x < lo || x > hi:
		v.add(field, "out_of_range")
	case math.Abs(x*10-math.Round(x*10)) > 1e-9:
		v.add(field, "too_many_decimals")
	}
	// Normalise binary noise such as 172.50000000001.
	r := math.Round(x*10) / 10
	return &r
}

func (v *validator) oneOf(field string, p *string, allowed ...string) string {
	if p == nil {
		v.add(field, "required")
		return ""
	}
	for _, a := range allowed {
		if *p == a {
			return a
		}
	}
	v.add(field, "invalid_value")
	return ""
}

// parseID reads a positive integer path parameter; anything else is a 404.
func parseID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		abortNotFound(c)
		return 0, false
	}
	return id, true
}
