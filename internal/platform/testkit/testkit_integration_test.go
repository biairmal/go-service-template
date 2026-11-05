package testkit_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/biairmal/go-sdk/lib/auth"
	"github.com/biairmal/go-sdk/lib/errorz"
	"github.com/biairmal/go-sdk/lib/httpkit/handler"

	"github.com/biairmal/go-service-template/internal/platform/testkit"
)

// TestKit exercises every helper together, the way a service test uses them:
// a migrated Postgres that starts empty, a token signed by SigningKeys and
// validated through its JWKS, Server/Do for the round trip, and a fake clock.
func TestKit(t *testing.T) {
	ctx := context.Background()
	db := testkit.Postgres(t)
	if _, err := db.Leader().ExecContext(ctx,
		`INSERT INTO outbox (topic, payload) VALUES ('t', '{}')`); err != nil {
		t.Fatalf("insert into the migrated schema: %v", err)
	}
	db = testkit.Postgres(t) // the next test's view: emptied
	var rows int
	if err := db.Leader().QueryRowContext(ctx, `SELECT count(*) FROM outbox`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("outbox after Postgres() = %d rows, %v; want emptied", rows, err)
	}

	keys := testkit.SigningKeys(t, "identity", "api")
	v, err := auth.NewRS256(auth.WithJWKSURL(keys.JWKSURL),
		auth.WithExpectedIssuer("identity"), auth.WithExpectedAudience("api"))
	if err != nil {
		t.Fatalf("NewRS256: %v", err)
	}
	srv := testkit.Server(t, handler.Handle(func(r *http.Request) (any, error) {
		claims, err := v.Validate(r.Context(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil {
			return nil, errorz.Unauthorized()
		}
		tid, _ := claims.Get("tid")
		return map[string]any{"sub": claims.Subject(), "tid": tid, "roles": claims.Roles()}, nil
	}))

	tok := keys.Token(t, "user-1", "tenant-1", []string{"admin"}, nil)
	r := testkit.Do[map[string]any](t, srv, http.MethodGet, "/", nil, testkit.Bearer(tok))
	if r.Status != http.StatusOK || r.Data["sub"] != "user-1" || r.Data["tid"] != "tenant-1" ||
		fmt.Sprint(r.Data["roles"]) != "[admin]" {
		t.Fatalf("valid token = %d %s, want 200 with sub and tid", r.Status, r.Raw)
	}
	other := testkit.SigningKeys(t, "identity", "api").Token(t, "user-1", "tenant-1", nil, nil)
	if r := testkit.Do[any](t, srv, http.MethodGet, "/", nil, testkit.Bearer(other)); r.Status != http.StatusUnauthorized {
		t.Fatalf("token from another key = %d, want 401", r.Status)
	}

	clk := testkit.NewClock(time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC))
	clk.Advance(90 * time.Minute)
	if got := clk.Now(); got.Hour() != 1 || got.Minute() != 30 {
		t.Fatalf("clock = %v, want 01:30", got)
	}
}
