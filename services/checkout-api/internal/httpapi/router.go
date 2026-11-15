package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/meridian-platform/meridian/services/checkout-api/internal/obs"
	"github.com/meridian-platform/meridian/services/checkout-api/internal/store"
)

// Deps are the collaborators required by the HTTP layer.
type Deps struct {
	Store        store.OrderStore
	Metrics      *obs.Metrics
	Logger       *slog.Logger
	Health       *Health
	MaxBodyBytes int64
	FaultLatency time.Duration
	Now          func() time.Time
}

// New builds the fully instrumented HTTP handler.
func New(deps Deps) http.Handler {
	if deps.Now == nil {
		deps.Now = time.Now
	}

	api := &API{deps: deps}
	router := chi.NewRouter()

	router.Use(RequestID)
	router.Use(Recoverer(deps.Logger))
	router.Use(AccessLog(deps.Logger))
	router.Use(deps.Metrics.Middleware)

	router.Get("/healthz", api.health)
	router.Get("/readyz", api.readiness)
	router.Get("/drainz", api.drain)
	router.Handle("/metrics", deps.Metrics.Handler())

	router.Group(func(r chi.Router) {
		r.Use(FaultLatency(deps.FaultLatency))
		r.Post("/v1/orders", api.createOrder)
		r.Get("/v1/orders", api.listOrders)
		r.Get("/v1/orders/{orderID}", api.getOrder)
	})

	router.NotFound(api.notFound)
	router.MethodNotAllowed(api.methodNotAllowed)

	return router
}
