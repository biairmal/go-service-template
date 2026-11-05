package app

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/biairmal/go-sdk/lib/auth"
	"github.com/biairmal/go-sdk/lib/authz"
	"github.com/biairmal/go-sdk/lib/logger"
	"github.com/google/uuid"

	"github.com/biairmal/go-service-template/internal/platform/testkit"
)

// fakeIdentity stands in for identity's token and role map endpoints and
// counts the calls to each.
type fakeIdentity struct {
	tokens, roleMaps atomic.Int32
	url              string
}

func newFakeIdentity(t *testing.T) *fakeIdentity {
	t.Helper()
	f := &fakeIdentity{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/auth/token", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["grant_type"] != "client_credentials" ||
			body["client_id"] != "note-service" || body["client_secret"] != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.tokens.Add(1)
		_, _ = w.Write([]byte(`{"data":{"access_token":"svc-token","token_type":"Bearer","expires_in":300}}`))
	})
	mux.HandleFunc("GET /v1/internal/roles/permissions", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer svc-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.roleMaps.Add(1)
		_, _ = w.Write([]byte(`{"data":{"roles":{"member":["notes:read","notes:write"]}}}`))
	})
	f.url = testkit.Server(t, mux).URL
	return f
}

func identityConfig(t *testing.T, url string) Config {
	t.Helper()
	cfg := validConfig(t)
	cfg.Auth.Enabled = true
	cfg.Authz.Source = authz.SourceIdentity
	cfg.Authz.IdentityURL = url
	cfg.Identity = IdentityConfig{ClientID: "note-service", ClientSecret: "s3cret"}
	return cfg
}

func TestIdentitySourceFetchesTokenAndRoleMapOnce(t *testing.T) {
	idp := newFakeIdentity(t)
	cfg := identityConfig(t, idp.url)
	az, err := newAuthorizer(&cfg, logger.NewNoOp())
	if err != nil {
		t.Fatal(err)
	}
	ctx := auth.ContextWithClaims(context.Background(), auth.NewClaims(map[string]any{
		"sub": uuid.NewString(), "tid": uuid.NewString(), "roles": []string{"member"},
	}))

	if !az.Can(ctx, "notes:read") {
		t.Fatal("member should hold notes:read from identity's role map")
	}
	if az.Can(ctx, "notes:read:any") || !az.Can(ctx, "notes:write") {
		t.Fatal("permissions don't follow identity's role map")
	}
	if got := idp.tokens.Load(); got != 1 {
		t.Errorf("token exchanges = %d, want 1", got)
	}
	if got := idp.roleMaps.Load(); got != 1 {
		t.Errorf("role map fetches = %d, want 1 (later checks use the cache)", got)
	}
}

func TestServiceTokenIsCachedUntilShortlyBeforeExpiry(t *testing.T) {
	idp := newFakeIdentity(t)
	clk := testkit.NewClock(time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC))
	st := newServiceToken(idp.url+"/", IdentityConfig{ClientID: "note-service", ClientSecret: "s3cret"},
		http.DefaultClient, clk)
	ctx := context.Background()

	for _, step := range []struct {
		advance time.Duration
		want    int32
	}{
		{0, 1},           // first call fetches
		{time.Minute, 1}, // cached
		{300*time.Second - serviceTokenMargin - time.Minute - time.Second, 1}, // just before the margin
		{time.Second, 2}, // inside the margin: refetched
	} {
		clk.Advance(step.advance)
		tok, err := st.Token(ctx)
		if err != nil || tok != "svc-token" {
			t.Fatalf("Token() = %q, %v", tok, err)
		}
		if got := idp.tokens.Load(); got != step.want {
			t.Fatalf("after %v: token exchanges = %d, want %d", step.advance, got, step.want)
		}
	}
}

func TestServiceTokenRejected(t *testing.T) {
	idp := newFakeIdentity(t)
	st := newServiceToken(idp.url, IdentityConfig{ClientID: "note-service", ClientSecret: "wrong"},
		http.DefaultClient, testkit.NewClock(time.Now()))
	if _, err := st.Token(context.Background()); err == nil {
		t.Fatal("a rejected client secret must be an error")
	}
}
