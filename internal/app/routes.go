package app

import (
	"context"
	"net/http"

	"github.com/biairmal/go-sdk/lib/errorz"
	"github.com/biairmal/go-sdk/lib/httpkit"
	"github.com/biairmal/go-sdk/lib/httpkit/handler"
	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// routes mounts the middleware chain and the operational endpoints onto r.
// Unknown paths and methods get the go-sdk error envelope.
func (a *App) routes(r chi.Router, chain []func(http.Handler) http.Handler) chi.Router {
	r.Use(chain...)
	// Set before any group is mounted: chi copies them into subrouters at mount time.
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		handler.WriteErrorResponse(w, http.StatusNotFound, errorz.NotFound().WithMessage("route not found"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		handler.WriteErrorResponse(w, http.StatusMethodNotAllowed,
			errorz.New("method not allowed").WithCode(codeMethodNotAllowed))
	})

	r.Get("/healthz", httpkit.Health())
	r.Get("/readyz", httpkit.Readiness(a.readiness))
	r.Get("/version", httpkit.Version(a.cfg.Service.Version))
	if a.cfg.Metrics.Enabled {
		r.Method(http.MethodGet, a.cfg.Metrics.Path, promhttp.HandlerFor(a.metrics, promhttp.HandlerOpts{}))
	}
	return r
}

// codeMethodNotAllowed is the error code of the 405 envelope; errorz has no
// code for it, and the status is written directly, so it never maps to 500.
const codeMethodNotAllowed = "ERR_METHOD_NOT_ALLOWED"

// readiness fails while the service is starting or shutting down, or while
// the leader database is unhealthy.
func (a *App) readiness(context.Context) error {
	if !a.ready.Load() {
		return errorz.ServiceUnavailable().WithMessage("not ready")
	}
	if !a.db.IsHealthy() {
		return errorz.ServiceUnavailable().WithMessage("database unavailable")
	}
	return nil
}
