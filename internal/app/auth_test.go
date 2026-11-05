package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/biairmal/go-sdk/lib/logger"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/biairmal/go-service-template/internal/platform/scope"
	"github.com/biairmal/go-service-template/internal/platform/testkit"
)

func TestAuthEnabledNeedsAValidToken(t *testing.T) {
	keys := testkit.SigningKeys(t, "identity", "api")
	cfg := validConfig(t)
	cfg.Auth.Enabled = true
	cfg.Auth.Local.JWKSURL = keys.JWKSURL
	mw, err := authMiddleware(&cfg, logger.NewNoOp())
	if err != nil {
		t.Fatal(err)
	}
	h := (&App{}).routes(chi.NewRouter(), []Module{groupsModule{}}, []func(http.Handler) http.Handler{mw})

	good := keys.Token(t, uuid.NewString(), uuid.NewString(), []string{"member"}, nil)
	other := testkit.SigningKeys(t, "identity", "api").Token(t, uuid.NewString(), uuid.NewString(), nil, nil)
	tests := []struct {
		name, path, token string
		want              int
	}{
		{"no token", "/v1/api", "", http.StatusUnauthorized},
		{"token signed by another key", "/v1/api", other, http.StatusUnauthorized},
		{"identity token", "/v1/api", good, http.StatusNoContent},
		{"ops path stays public", "/healthz", "", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, http.NoBody)
			if tt.token != "" {
				req.Header = testkit.Bearer(tt.token)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

func TestAuthDisabledRunsAsTheDevAdmin(t *testing.T) {
	cfg := validConfig(t) // AUTH_ENABLED defaults to false
	mw, err := authMiddleware(&cfg, logger.NewNoOp())
	if err != nil {
		t.Fatal(err)
	}
	az, err := newAuthorizer(&cfg, logger.NewNoOp())
	if err != nil {
		t.Fatal(err)
	}

	var tenant, owner uuid.UUID
	var tenantErr, ownerErr error
	var can bool
	mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		tenant, tenantErr = scope.Tenant(r.Context())
		owner, ownerErr = scope.Owner(r.Context())
		can = az.Can(r.Context(), "notes:write:any")
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/notes", http.NoBody))

	if tenantErr != nil || tenant.String() != devTenant {
		t.Errorf("scope.Tenant = %v, %v; want %s", tenant, tenantErr, devTenant)
	}
	if ownerErr != nil || owner.String() != devSubject {
		t.Errorf("scope.Owner = %v, %v; want %s", owner, ownerErr, devSubject)
	}
	if !can {
		t.Error("the dev admin should hold every permission")
	}
}
