package app

import (
	"net/http"
	"slices"

	"github.com/biairmal/go-sdk/lib/auth"
	"github.com/biairmal/go-sdk/lib/httpkit/middleware"
	"github.com/biairmal/go-sdk/lib/logger"
)

// Headers the browser may send and read cross-origin.
const (
	corsAllowMethods  = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	corsAllowHeaders  = "Authorization, Content-Type, If-Match, X-Request-Id, X-Correlation-Id"
	corsExposeHeaders = "X-Request-Id, X-Correlation-Id, Retry-After"
	corsMaxAge        = "600"
)

// cors allows the configured origins to call the API from a browser and
// answers preflight requests. Requests from other origins get no CORS
// headers, so the browser blocks them; non-browser clients are unaffected.
func cors(cfg CORSConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" || !slices.Contains(cfg.AllowedOrigins, origin) {
				next.ServeHTTP(w, r)
				return
			}
			h := w.Header()
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Expose-Headers", corsExposeHeaders)
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", corsAllowMethods)
				h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
				h.Set("Access-Control-Max-Age", corsMaxAge)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// The fixed caller devClaims puts on every request when auth is off. The
// subject and tenant are constant, so rows created in one run are found in
// the next.
const (
	devSubject = "00000000-0000-4000-8000-00000000d001"
	devTenant  = "00000000-0000-4000-8000-00000000d002"
	devRole    = "admin"
)

// authMiddleware validates bearer tokens per the route policy. With auth off
// it logs a warning and returns devClaims instead, so scoped code runs
// unchanged without identity.
func authMiddleware(cfg *Config, log logger.Logger) (func(http.Handler) http.Handler, error) {
	if !cfg.Auth.Enabled {
		log.Warn("auth is disabled: every route is public and every caller is the dev admin",
			logger.F("subject", devSubject), logger.F("tenant_id", devTenant))
		return devClaims(), nil
	}
	v, err := auth.FromConfig(&cfg.Auth.Config)
	if err != nil {
		return nil, err
	}
	return middleware.Auth(v, middleware.WithPolicy(cfg.Auth.Policy())), nil
}

// devClaims puts the fixed dev caller (devSubject in tenant devTenant, role
// devRole) on every request, as if it carried a token.
func devClaims() func(http.Handler) http.Handler {
	claims := auth.NewClaims(map[string]any{"sub": devSubject, "tid": devTenant, "roles": []string{devRole}})
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.ContextWithClaims(r.Context(), claims)))
		})
	}
}

// maxBody caps every request body at limit bytes; reading past it fails.
func maxBody(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
