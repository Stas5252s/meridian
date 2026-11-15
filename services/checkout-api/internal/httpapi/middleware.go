package httpapi

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	mathrand "math/rand/v2"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/meridian-platform/meridian/services/checkout-api/internal/store"
)

type contextKey int

const requestIDKey contextKey = iota

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

var uninstrumentedRoutes = map[string]struct{}{
	"/healthz": {},
	"/readyz":  {},
	"/drainz":  {},
	"/metrics": {},
}

func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
		return rc.RoutePattern()
	}
	return "unmatched"
}

// Health tracks readiness and propagation of SIGTERM / preStop drains.
type Health struct {
	draining atomic.Bool
	store    store.OrderStore
}

func NewHealth(st store.OrderStore) *Health {
	return &Health{store: st}
}

func (h *Health) Drain() {
	h.draining.Store(true)
}

func (h *Health) Draining() bool {
	return h.draining.Load()
}

func (h *Health) ready(ctx context.Context) error {
	if h.draining.Load() {
		return fmt.Errorf("draining")
	}
	return h.store.Ready(ctx)
}

// RequestID accepts a well-formed caller-provided ID or generates one.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if !validRequestID.MatchString(id) {
			id = cryptorand.Text()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// AccessLog emits one structured line per request, excluding probes.
func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &logRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			route := routePattern(r)
			if _, skip := uninstrumentedRoutes[route]; skip {
				return
			}

			level := slog.LevelInfo
			switch {
			case rec.status >= 500:
				level = slog.LevelError
			case rec.status >= 400:
				level = slog.LevelWarn
			}

			logger.LogAttrs(r.Context(), level, "http request",
				slog.String("method", r.Method),
				slog.String("route", route),
				slog.Int("status", rec.status),
				slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
				slog.String("request_id", requestID(r.Context())),
				slog.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}

// Recoverer converts panics into JSON 500 responses without leaking stack
// traces to clients.
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
						panic(rec)
					}
					logger.ErrorContext(r.Context(), "panic recovered",
						slog.Any("panic", rec),
						slog.String("stack", string(debug.Stack())),
						slog.String("request_id", requestID(r.Context())),
					)
					writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", requestID(r.Context()))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// FaultLatency injects bounded random latency for SLO demonstrations. It is a
// no-op unless FAULT_LATENCY_MS is configured.
func FaultLatency(max time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if max <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			delay := time.Duration(mathrand.Int64N(int64(max))) //nolint:gosec // jitter for demos, not a security primitive
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-r.Context().Done():
				return
			case <-timer.C:
			}
			next.ServeHTTP(w, r)
		})
	}
}

type logRecorder struct {
	http.ResponseWriter
	status int
}

func (r *logRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *logRecorder) Write(b []byte) (int, error) {
	return r.ResponseWriter.Write(b)
}

func (r *logRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
