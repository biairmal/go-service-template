package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/biairmal/go-service-template/internal/platform/httpx"
)

// groupsModule mounts one route in every group.
type groupsModule struct{}

func (groupsModule) Register(rt *httpx.Routes) {
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	rt.Root.Get("/root", ok)
	rt.API.Get("/api", ok)
	rt.Me.Get("/profile", ok)
	rt.Admin.Get("/users", ok)
	rt.Internal.Get("/users", ok)
}

func TestRouteGroups(t *testing.T) {
	r := (&App{}).routes(chi.NewRouter(), []Module{groupsModule{}}, nil)
	var got []string
	if err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got = append(got, method+" "+route)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/root", "/v1/api", "/v1/me/profile", "/v1/admin/users", "/v1/internal/users"} {
		if !slices.Contains(got, "GET "+p) {
			t.Errorf("chi.Walk has no GET %s (routes: %v)", p, got)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, http.NoBody))
		if rec.Code != http.StatusNoContent {
			t.Errorf("GET %s = %d, want 204", p, rec.Code)
		}
	}
}

func TestUnknownRoutesUseErrorEnvelope(t *testing.T) {
	r := (&App{}).routes(chi.NewRouter(), []Module{groupsModule{}}, nil)
	tests := []struct {
		method, path string
		status       int
		code         string
	}{
		{http.MethodGet, "/nope", http.StatusNotFound, "ERR_NOT_FOUND"},
		{http.MethodGet, "/v1/nope", http.StatusNotFound, "ERR_NOT_FOUND"},
		{http.MethodGet, "/v1/admin/nope", http.StatusNotFound, "ERR_NOT_FOUND"},
		{http.MethodPost, "/healthz", http.StatusMethodNotAllowed, codeMethodNotAllowed},
		{http.MethodPost, "/v1/me/profile", http.StatusMethodNotAllowed, codeMethodNotAllowed},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, http.NoBody))
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != tt.status ||
			body.Error.Code != tt.code {
			t.Errorf("%s %s = %d %s, want %d %s", tt.method, tt.path, rec.Code, rec.Body, tt.status, tt.code)
		}
	}
}

func TestSwaggerNeedsBasicAuth(t *testing.T) {
	a := &App{cfg: Config{Swagger: SwaggerConfig{Enabled: true, Username: "u", Password: "p"}}}
	r := a.routes(chi.NewRouter(), nil, nil)
	for _, tc := range []struct {
		name, path, user, pass string
		status                 int
	}{
		{"no credentials", "/swagger/index.html", "", "", http.StatusUnauthorized},
		{"wrong password", "/swagger/index.html", "u", "x", http.StatusUnauthorized},
		{"right credentials", "/swagger/index.html", "u", "p", http.StatusOK},
		{"spec with credentials", "/swagger/doc.json", "u", "p", http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, http.NoBody)
		if tc.user != "" {
			req.SetBasicAuth(tc.user, tc.pass)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Errorf("%s: status = %d, want %d", tc.name, rec.Code, tc.status)
		}
		if tc.status == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("%s: no WWW-Authenticate challenge", tc.name)
		}
	}
}

func TestSwaggerOffIs404(t *testing.T) {
	r := (&App{}).routes(chi.NewRouter(), nil, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/swagger/index.html", http.NoBody))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/swagger/index.html with swagger off = %d, want 404", rec.Code)
	}
}
