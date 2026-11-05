package note

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/biairmal/go-sdk/lib/repository"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
)

// listed is what GET /v1/notes handed the repository.
type listed struct {
	opts   *repository.ListOptions
	search string
	owners Owners
}

// listCall serves GET /v1/notes?query for a caller holding perms.
func listCall(t *testing.T, query string, perms ...string) (int, listed) {
	t.Helper()
	h, repo := newTestServer(t, perms...)
	var got listed
	repo.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().
		DoAndReturn(func(
			_ context.Context, opts *repository.ListOptions, search string, owners Owners,
		) ([]Note, int64, error) {
			got = listed{opts, search, owners}
			return nil, 0, nil
		})
	status, _ := call(t, h, http.MethodGet, "/v1/notes?"+query, "", nil)
	return status, got
}

// condition returns the filter condition on field and operator, or nil.
func (l listed) condition(field string, op repository.FilterOperator) *repository.FilterCondition {
	for _, c := range l.opts.Filter.Conditions {
		if c.Field == field && c.Operator == op {
			return &c
		}
	}
	return nil
}

func TestListQueryPatterns(t *testing.T) {
	owner := uuid.New()
	day := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }
	tests := []struct {
		name  string
		query string
		perms []string
		check func(t *testing.T, l listed)
	}{
		{"paging", "page=2&size=50", []string{PermRead}, func(t *testing.T, l listed) {
			if l.opts.Pagination != (repository.Pagination{Limit: 50, Offset: 50}) {
				t.Errorf("pagination = %+v, want limit 50 offset 50", l.opts.Pagination)
			}
		}},
		{
			"sorting, repeatable and in order", "sort=pinned,DESC&sort=updated_at,DESC",
			[]string{PermRead},
			func(t *testing.T, l listed) {
				want := []repository.Sort{{Field: "pinned", Direction: "DESC"}, {Field: "updated_at", Direction: "DESC"}}
				if !reflect.DeepEqual(l.opts.Sorts, want) {
					t.Errorf("sorts = %+v, want %+v", l.opts.Sorts, want)
				}
			},
		},
		{"equality filters, typed", "status=draft&pinned=true", []string{PermRead}, func(t *testing.T, l listed) {
			if c := l.condition("status", repository.FilterOperatorEq); c == nil || c.Value != "draft" {
				t.Errorf("status condition = %+v", c)
			}
			if c := l.condition("pinned", repository.FilterOperatorEq); c == nil || c.Value != true {
				t.Errorf("pinned condition = %+v, want the bool true", c)
			}
		}},
		{"contains filter", "title=meet%3Blike", []string{PermRead}, func(t *testing.T, l listed) {
			if c := l.condition("title", repository.FilterOperatorLike); c == nil || c.Value != "%meet%" {
				t.Errorf("title condition = %+v, want like %%meet%%", c)
			}
		}},
		{
			"search and half-open date range", "search=budget&created_from=2026-01-01&created_to=2026-02-01T00:00:00Z",
			[]string{PermRead},
			func(t *testing.T, l listed) {
				if l.search != "budget" {
					t.Errorf("search = %q", l.search)
				}
				if c := l.condition("created_at", repository.FilterOperatorGte); c == nil || c.Value != day("2026-01-01") {
					t.Errorf("created_at >= condition = %+v", c)
				}
				if c := l.condition("created_at", repository.FilterOperatorLt); c == nil || c.Value != day("2026-02-01") {
					t.Errorf("created_at < condition = %+v", c)
				}
			},
		},
		{"owner_id with notes:read:any", "owner_id=" + owner.String(), []string{PermReadAny}, func(t *testing.T, l listed) {
			if c := l.condition("owner_id", repository.FilterOperatorEq); c == nil || c.Value != owner || l.owners != AnyOwner {
				t.Errorf("owner_id condition = %+v, owners %v; want the UUID with AnyOwner", c, l.owners)
			}
		}},
		{"plain caller is owner-scoped", "", []string{PermRead}, func(t *testing.T, l listed) {
			if l.owners != OwnNotes || len(l.opts.Sorts) != 0 {
				t.Errorf("owners = %v, sorts %v; want OwnNotes and the repository's default order", l.owners, l.opts.Sorts)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, l := listCall(t, tt.query, tt.perms...)
			if status != http.StatusOK || l.opts == nil {
				t.Fatalf("status = %d, want 200 with a repository call", status)
			}
			tt.check(t, l)
		})
	}
}

func TestListQueryRejects(t *testing.T) {
	tests := []struct {
		name, query string
		perms       []string
		want        int
	}{
		{"unknown key", "foo=1", []string{PermRead}, http.StatusBadRequest},
		{"disallowed sort", "sort=body,ASC", []string{PermRead}, http.StatusBadRequest},
		{"tenant_id filter", "tenant_id=" + uuid.NewString(), []string{PermReadAny}, http.StatusBadRequest},
		{"tenant_id sort", "sort=tenant_id,ASC", []string{PermReadAny}, http.StatusBadRequest},
		{"malformed date", "created_from=yesterday", []string{PermRead}, http.StatusBadRequest},
		{"inverted range", "created_from=2026-02-01&created_to=2026-01-01", []string{PermRead}, http.StatusBadRequest},
		{"bad pinned", "pinned=maybe", []string{PermRead}, http.StatusBadRequest},
		{"bad status", "status=deleted", []string{PermRead}, http.StatusBadRequest},
		{"like on status", "status=draft%3Blike", []string{PermRead}, http.StatusBadRequest},
		{"bad owner_id", "owner_id=me", []string{PermReadAny}, http.StatusBadRequest},
		{"owner_id without notes:read:any", "owner_id=" + uuid.NewString(), []string{PermRead}, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if status, _ := listCall(t, tt.query, tt.perms...); status != tt.want {
				t.Fatalf("GET /v1/notes?%s = %d, want %d", tt.query, status, tt.want)
			}
		})
	}
}
