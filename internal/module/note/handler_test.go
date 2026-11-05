package note

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/biairmal/go-sdk/lib/auth"
	"github.com/biairmal/go-sdk/lib/authz"
	"github.com/biairmal/go-sdk/lib/repository"
	"github.com/biairmal/go-sdk/lib/validator"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/biairmal/go-service-template/internal/platform/scope"
)

// The caller of every test request.
var (
	testTenant = uuid.MustParse("00000000-0000-4000-8000-0000000000a1")
	testUser   = uuid.MustParse("00000000-0000-4000-8000-0000000000b1")
)

// newTestServer serves the note routes for a caller holding perms, over a
// mocked repository.
func newTestServer(t *testing.T, perms ...string) (http.Handler, *MockRepository) {
	t.Helper()
	repo := NewMockRepository(gomock.NewController(t))
	az, err := authz.New(&authz.Config{Source: authz.SourceConfig, Roles: map[string][]string{"r": perms}})
	if err != nil {
		t.Fatal(err)
	}
	v := validator.New(validator.Config{FieldNameTag: "json"})
	h := NewHandler(NewService(Config{ListMaxSize: 50}, repo, az), v, az)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		claims := auth.NewClaims(map[string]any{
			"sub": testUser.String(), "tid": testTenant.String(), "roles": []string{"r"},
		})
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.ContextWithClaims(r.Context(), claims)))
		})
	})
	r.Route("/v1/notes", h.Register)
	return r, repo
}

type envelope struct {
	Data  NoteResponse `json:"data"`
	Error struct {
		Code string `json:"code"`
		Meta struct {
			Fields map[string]string `json:"fields"`
		} `json:"meta"`
	} `json:"error"`
}

func call(t *testing.T, h http.Handler, method, path, body string, header map[string]string) (int, envelope) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var env envelope
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("%s %s: body is not an envelope: %s", method, path, rec.Body)
		}
	}
	return rec.Code, env
}

// stored is what the mocked repository returns: a note owned by the caller.
func stored(ctx context.Context, title string) *Note {
	tenant, _ := scope.Tenant(ctx)
	owner, _ := scope.Owner(ctx)
	now := time.Now()
	return &Note{
		ID: uuid.New(), TenantID: tenant, OwnerID: owner, Title: title, Status: StatusDraft,
		Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: owner,
	}
}

func TestValidationErrorsNameTheField(t *testing.T) {
	h, _ := newTestServer(t, PermWrite) // the repository is never reached
	id := "/v1/notes/" + uuid.NewString()
	ifMatch := map[string]string{"If-Match": `"1"`}
	tests := []struct {
		name, method, path, body string
		field                    string // empty: a 400 without fields
	}{
		{"title missing", http.MethodPost, "/v1/notes", `{"body":"b"}`, "title"},
		{"title too long", http.MethodPost, "/v1/notes", `{"title":"` + strings.Repeat("x", 201) + `"}`, "title"},
		{"bad status", http.MethodPost, "/v1/notes", `{"title":"t","status":"deleted"}`, "status"},
		{"wrong type", http.MethodPost, "/v1/notes", `{"title":"t","pinned":"yes"}`, "pinned"},
		{"owner_id in body", http.MethodPost, "/v1/notes", `{"title":"t","owner_id":"` + uuid.NewString() + `"}`, "owner_id"},
		{
			"tenant_id in body", http.MethodPost, "/v1/notes",
			`{"title":"t","tenant_id":"` + uuid.NewString() + `"}`, "tenant_id",
		},
		{"patch empty title", http.MethodPatch, id, `{"title":""}`, "title"},
		{"patch bad status", http.MethodPatch, id, `{"status":"x"}`, "status"},
		{"patch owner_id", http.MethodPatch, id, `{"owner_id":"` + uuid.NewString() + `"}`, "owner_id"},
		{"patch nothing", http.MethodPatch, id, `{}`, ""},
		{"no body", http.MethodPost, "/v1/notes", ``, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, env := call(t, h, tt.method, tt.path, tt.body, ifMatch)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", status)
			}
			if tt.field != "" && env.Error.Meta.Fields[tt.field] == "" {
				t.Fatalf("meta.fields = %v, want an entry for %q", env.Error.Meta.Fields, tt.field)
			}
		})
	}
}

func TestPatchNeedsIfMatch(t *testing.T) {
	h, _ := newTestServer(t, PermWrite)
	status, _ := call(t, h, http.MethodPatch, "/v1/notes/"+uuid.NewString(), `{"title":"t"}`, nil)
	if status != http.StatusPreconditionRequired {
		t.Fatalf("PATCH without If-Match = %d, want 428", status)
	}
}

func TestRoutesNeedTheirPermission(t *testing.T) {
	id := "/v1/notes/" + uuid.NewString()
	ifMatch := map[string]string{"If-Match": `"1"`}
	tests := []struct {
		name, method, path, body string
		perms                    []string
	}{
		{"create without notes:write", http.MethodPost, "/v1/notes", `{"title":"t"}`, []string{PermRead}},
		{"patch without notes:write", http.MethodPatch, id, `{"title":"t"}`, []string{PermRead, PermWriteAny}},
		{"delete without notes:write", http.MethodDelete, id, ``, []string{PermRead}},
		{"list without a read permission", http.MethodGet, "/v1/notes", ``, []string{PermWrite}},
		{"get without a read permission", http.MethodGet, id, ``, []string{PermWrite}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := newTestServer(t, tt.perms...) // no repository call expected
			if status, _ := call(t, h, tt.method, tt.path, tt.body, ifMatch); status != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", status)
			}
		})
	}
}

func TestCreateTakesOwnerAndTenantFromTheToken(t *testing.T) {
	h, repo := newTestServer(t, PermWrite)
	repo.EXPECT().Create(gomock.Any(), &NewNote{Title: "t", Body: "b", Pinned: true}).
		DoAndReturn(func(ctx context.Context, n *NewNote) (*Note, error) {
			if tid, _ := scope.Tenant(ctx); tid != testTenant {
				t.Errorf("repository saw tenant %s, want the token's %s", tid, testTenant)
			}
			if sub, _ := scope.Owner(ctx); sub != testUser {
				t.Errorf("repository saw owner %s, want the token's %s", sub, testUser)
			}
			return stored(ctx, n.Title), nil
		})
	status, env := call(t, h, http.MethodPost, "/v1/notes", `{"title":"t","body":"b","pinned":true}`, nil)
	if status != http.StatusCreated || env.Data.OwnerID != testUser.String() || env.Data.Version != 1 {
		t.Fatalf("POST = %d %+v, want 201 owned by the caller", status, env.Data)
	}
}

func TestAnyPermissionsWidenTheOwnerScope(t *testing.T) {
	id := uuid.New()
	tests := []struct {
		name  string
		perms []string
		want  Owners
	}{
		{"own notes only", []string{PermRead, PermWrite}, OwnNotes},
		{"any owner", []string{PermReadAny, PermWrite, PermWriteAny}, AnyOwner},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, repo := newTestServer(t, tt.perms...)
			repo.EXPECT().Get(gomock.Any(), id, tt.want).
				DoAndReturn(func(ctx context.Context, _ uuid.UUID, _ Owners) (*Note, error) { return stored(ctx, "t"), nil })
			repo.EXPECT().List(gomock.Any(), gomock.Any(), "", tt.want).Return(nil, int64(0), nil)
			title := "new"
			repo.EXPECT().Update(gomock.Any(), id, 3, &Patch{Title: &title}, tt.want).
				DoAndReturn(func(ctx context.Context, _ uuid.UUID, _ int, p *Patch, _ Owners) (*Note, error) {
					return stored(ctx, *p.Title), nil
				})
			repo.EXPECT().Delete(gomock.Any(), id, tt.want).Return(nil)

			path := "/v1/notes/" + id.String()
			if status, _ := call(t, h, http.MethodGet, path, "", nil); status != http.StatusOK {
				t.Errorf("GET = %d", status)
			}
			if status, _ := call(t, h, http.MethodGet, "/v1/notes", "", nil); status != http.StatusOK {
				t.Errorf("GET list = %d", status)
			}
			if status, env := call(t, h, http.MethodPatch, path, `{"title":"new"}`,
				map[string]string{"If-Match": `"3"`}); status != http.StatusOK || env.Data.Title != "new" {
				t.Errorf("PATCH = %d %+v", status, env.Data)
			}
			if status, _ := call(t, h, http.MethodDelete, path, "", nil); status != http.StatusNoContent {
				t.Errorf("DELETE = %d, want 204", status)
			}
		})
	}
}

func TestListIsAFirstPage(t *testing.T) {
	h, repo := newTestServer(t, PermRead)
	repo.EXPECT().List(gomock.Any(), gomock.Any(), "", OwnNotes).
		DoAndReturn(func(ctx context.Context, opts *repository.ListOptions, _ string, _ Owners) ([]Note, int64, error) {
			if opts.Pagination.Limit != 50 || opts.Pagination.Offset != 0 {
				t.Errorf("pagination = %+v, want the first page capped at list_max_size 50", opts.Pagination)
			}
			return []Note{*stored(ctx, "a")}, 1, nil
		})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/notes?size=500", http.NoBody))
	var env struct {
		Data NotePageResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || rec.Code != http.StatusOK ||
		env.Data.Total != 1 || len(env.Data.Items) != 1 || env.Data.Size != 50 || env.Data.Items[0].Title != "a" {
		t.Fatalf("GET /v1/notes = %d %s", rec.Code, rec.Body)
	}
	if status, _ := call(t, h, http.MethodGet, "/v1/notes?color=red", "", nil); status != http.StatusBadRequest {
		t.Fatalf("unknown query key = %d, want 400", status)
	}
}

func TestMalformedIDIsNotFound(t *testing.T) {
	h, _ := newTestServer(t, PermRead)
	if status, _ := call(t, h, http.MethodGet, "/v1/notes/not-a-uuid", "", nil); status != http.StatusNotFound {
		t.Fatalf("GET /v1/notes/not-a-uuid = %d, want 404", status)
	}
}
