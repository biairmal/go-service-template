package note_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/biairmal/go-sdk/lib/auth"
	"github.com/biairmal/go-sdk/lib/authz"
	"github.com/biairmal/go-sdk/lib/validator"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/biairmal/go-service-template/internal/module/note"
	"github.com/biairmal/go-service-template/internal/platform/testkit"
)

// seed is one note of the list fixture, with its creation time.
type seed struct {
	title, body string
	pinned      bool
	created     string // RFC 3339
}

// listFixture: dates sit on the edges of created_from=2026-01-01 and
// created_to=2026-02-01.
var listFixture = []seed{
	{"Budget", "q1 numbers", false, "2025-12-31T23:59:59Z"},          // just before from
	{"Meeting notes", "budget review", true, "2026-01-01T00:00:00Z"}, // exactly from: in
	{"Shopping", "milk", false, "2026-01-15T12:00:00Z"},
	{"Team meet", "", true, "2026-01-31T23:59:59Z"},  // just before to: in
	{"Archive", "old", true, "2026-02-01T00:00:00Z"}, // exactly to: out
}

// listServer serves /v1/notes over the real repository for one caller with
// every note permission, after seeding listFixture as that caller.
func listServer(t *testing.T) func(query string) []string {
	t.Helper()
	db := testkit.Postgres(t)
	repo := note.NewRepository(db)
	claims := auth.NewClaims(map[string]any{
		"sub": uuid.NewString(), "tid": uuid.NewString(), "roles": []string{"member"},
	})
	ctx := auth.ContextWithClaims(context.Background(), claims)
	for _, s := range listFixture {
		n, err := repo.Create(ctx, &note.NewNote{Title: s.title, Body: s.body, Pinned: s.pinned})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Leader().ExecContext(ctx, `UPDATE note SET created_at = $2, updated_at = $2 WHERE id = $1`,
			n.ID, s.created); err != nil {
			t.Fatal(err)
		}
	}

	az, err := authz.New(&authz.Config{Source: authz.SourceConfig, Roles: map[string][]string{
		"member": {note.PermRead, note.PermWrite},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := note.NewHandler(note.NewService(note.Config{ListMaxSize: 100}, repo, az),
		validator.New(validator.Config{FieldNameTag: "json"}), az)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.ContextWithClaims(r.Context(), claims)))
		})
	})
	r.Route("/v1/notes", h.Register)
	srv := testkit.Server(t, r)

	return func(query string) []string {
		t.Helper()
		res := testkit.Do[note.NotePageResponse](t, srv, http.MethodGet, "/v1/notes?"+query, nil, nil)
		if res.Status != http.StatusOK {
			t.Fatalf("GET /v1/notes?%s = %d %s", query, res.Status, res.Raw)
		}
		titles := make([]string, len(res.Data.Items))
		for i, n := range res.Data.Items {
			titles[i] = n.Title
		}
		return titles
	}
}

func TestListQueriesAgainstPostgres(t *testing.T) {
	list := listServer(t)
	tests := []struct {
		name, query string
		want        []string
	}{
		{"default: newest first", "", []string{"Archive", "Team meet", "Shopping", "Meeting notes", "Budget"}},
		{
			"multi-sort applied in order", "sort=pinned,DESC&sort=title,ASC",
			[]string{"Archive", "Meeting notes", "Team meet", "Budget", "Shopping"},
		},
		{"contains, case-sensitive", "title=meet%3Blike&sort=title,ASC", []string{"Team meet"}},
		{"exact title", "title=Shopping", []string{"Shopping"}},
		{"search title or body, any case", "search=BUDGET&sort=title,ASC", []string{"Budget", "Meeting notes"}},
		{
			"half-open date range", "created_from=2026-01-01&created_to=2026-02-01&sort=created_at,ASC",
			[]string{"Meeting notes", "Shopping", "Team meet"},
		},
		{"timestamp bounds", "created_from=2025-12-31T23:59:59Z&created_to=2026-01-01T00:00:00Z", []string{"Budget"}},
		{"filters combine", "pinned=true&created_to=2026-02-01&sort=title,ASC", []string{"Meeting notes", "Team meet"}},
		{"paging", "sort=title,ASC&size=2&page=2", []string{"Meeting notes", "Shopping"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := list(tt.query)
			if len(got) != len(tt.want) {
				t.Fatalf("titles = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("titles = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
