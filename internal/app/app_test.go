package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/biairmal/go-sdk/lib/httpkit/response"
	"github.com/biairmal/go-sdk/lib/logger"
	"github.com/biairmal/go-sdk/lib/metrics"
	"github.com/biairmal/go-sdk/lib/tracer"
	"github.com/go-chi/chi/v5"
)

// TestOpsEndpointsWithoutDatabase covers the routes that answer without a
// database: liveness, version, metrics, readiness before Serve, and the
// envelope on unknown paths and methods.
func TestOpsEndpointsWithoutDatabase(t *testing.T) {
	cfg := Config{
		Service: ServiceConfig{Name: "note-service", Version: "1.2.3", Environment: EnvLocal},
		HTTP:    HTTPConfig{MaxBodyBytes: 1 << 20},
		Metrics: MetricsConfig{Enabled: true, Path: "/metrics"},
	}
	a := &App{cfg: cfg, metrics: newRegistry()}
	h := a.routes(chi.NewRouter(), middlewareChain(&cfg, logger.NewNoOp(), tracer.NewNoOp(), metrics.NewNoOp()))

	tests := []struct {
		name, method, path string
		want               int
		wantCode           string // the error envelope code; empty = success body
	}{
		{"liveness", http.MethodGet, "/healthz", http.StatusOK, ""},
		{"version", http.MethodGet, "/version", http.StatusOK, ""},
		{"metrics", http.MethodGet, "/metrics", http.StatusOK, ""},
		{"not ready before Serve", http.MethodGet, "/readyz", http.StatusServiceUnavailable, "ERR_SERVICE_UNAVAILABLE"},
		{"unknown path", http.MethodGet, "/nope", http.StatusNotFound, "ERR_NOT_FOUND"},
		{"wrong method", http.MethodPost, "/healthz", http.StatusMethodNotAllowed, codeMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, http.NoBody))
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.want, rec.Body)
			}
			if tt.wantCode == "" {
				return
			}
			var body struct {
				Code  string                `json:"code"`
				Error response.ErrorPayload `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != "ERROR" ||
				body.Error.Code != tt.wantCode {
				t.Fatalf("want a go-sdk error envelope with code %q, got %s", tt.wantCode, rec.Body)
			}
		})
	}
}
