package note

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/biairmal/go-sdk/lib/repository"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/biairmal/go-service-template/internal/platform/apierr"
)

// raw serves one request and returns the status and the decoded data.
func raw(t *testing.T, h http.Handler, method, path, body string, data any) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	if data != nil && rec.Code == http.StatusOK {
		env := struct{ Data any }{Data: data}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
	}
	return rec.Code
}

func TestOptions(t *testing.T) {
	a, b := uuid.New(), uuid.New()

	t.Run("search pages by title, size capped at 50", func(t *testing.T) {
		h, repo := newTestServer(t, PermRead)
		repo.EXPECT().List(gomock.Any(), gomock.Any(), "plan", OwnNotes).DoAndReturn(
			func(_ context.Context, opts *repository.ListOptions, _ string, _ Owners) ([]Note, int64, error) {
				if opts.Pagination != (repository.Pagination{Limit: 50, Offset: 50}) {
					t.Errorf("pagination = %+v, want limit 50 offset 50", opts.Pagination)
				}
				if len(opts.Sorts) != 1 || opts.Sorts[0].Field != "title" {
					t.Errorf("sorts = %+v, want title (the repository adds id)", opts.Sorts)
				}
				return []Note{{ID: a, Title: "Plan A"}}, 51, nil
			})
		var got OptionPageResponse
		if s := raw(t, h, http.MethodGet, "/v1/notes/options?search=plan&page=2&size=99", "", &got); s != http.StatusOK {
			t.Fatalf("status = %d", s)
		}
		if len(got.Items) != 1 || *got.Items[0] != (OptionResponse{Value: a.String(), Label: "Plan A"}) || got.Page != 2 {
			t.Errorf("page = %+v", got)
		}
	})

	t.Run("ids return exactly those, ignoring search and paging", func(t *testing.T) {
		h, repo := newTestServer(t, PermReadAny)
		repo.EXPECT().List(gomock.Any(), gomock.Any(), "", AnyOwner).DoAndReturn(
			func(_ context.Context, opts *repository.ListOptions, _ string, _ Owners) ([]Note, int64, error) {
				c := opts.Filter.Conditions
				if len(c) != 1 || c[0].Field != "id" || c[0].Operator != repository.FilterOperatorIn || len(c[0].Values) != 2 {
					t.Errorf("conditions = %+v, want id in (a, b)", c)
				}
				if opts.Pagination != (repository.Pagination{Limit: 2}) {
					t.Errorf("pagination = %+v, want limit 2", opts.Pagination)
				}
				return nil, 0, nil
			})
		path := "/v1/notes/options?search=x&page=3&ids=" + a.String() + "," + b.String()
		if s := raw(t, h, http.MethodGet, path, "", nil); s != http.StatusOK {
			t.Fatalf("status = %d", s)
		}
	})

	for name, query := range map[string]string{
		"bad id":       "ids=nope",
		"too many ids": "ids=" + strings.TrimSuffix(strings.Repeat(a.String()+",", 101), ","),
		"unknown key":  "title=x",
	} {
		t.Run(name, func(t *testing.T) {
			h, _ := newTestServer(t, PermRead)
			if s := raw(t, h, http.MethodGet, "/v1/notes/options?"+query, "", nil); s != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", s)
			}
		})
	}

	t.Run("needs a read permission", func(t *testing.T) {
		h, _ := newTestServer(t, PermWrite)
		if s := raw(t, h, http.MethodGet, "/v1/notes/options", "", nil); s != http.StatusForbidden {
			t.Errorf("status = %d, want 403", s)
		}
	})
}

func TestBulkDelete(t *testing.T) {
	a, b := uuid.New(), uuid.New()

	t.Run("ids: missing ones come back as not_found", func(t *testing.T) {
		h, repo := newTestServer(t, PermWrite)
		repo.EXPECT().BulkDelete(gomock.Any(), gomock.Any(), "", OwnNotes, maxSelectedIDs).DoAndReturn(
			func(_ context.Context, f repository.Filter, _ string, _ Owners, _ int) ([]uuid.UUID, error) {
				if len(f.Conditions) != 1 || f.Conditions[0].Operator != repository.FilterOperatorIn {
					t.Errorf("filter = %+v, want id in", f)
				}
				return []uuid.UUID{a}, nil
			})
		var got BulkDeleteResponse
		body := `{"ids":["` + a.String() + `","` + b.String() + `"]}`
		if s := raw(t, h, http.MethodPost, "/v1/notes/bulk-delete", body, &got); s != http.StatusOK {
			t.Fatalf("status = %d", s)
		}
		want := BulkDeleteFailure{ID: b.String(), Reason: "not_found"}
		if got.Affected != 1 || len(got.Failed) != 1 || got.Failed[0] != want {
			t.Errorf("response = %+v", got)
		}
	})

	t.Run("filter: list query, paging ignored, capped", func(t *testing.T) {
		h, repo := newTestServer(t, PermWrite, PermWriteAny)
		repo.EXPECT().BulkDelete(gomock.Any(), gomock.Any(), "q", AnyOwner, bulkDeleteLimit).DoAndReturn(
			func(_ context.Context, f repository.Filter, _ string, _ Owners, _ int) ([]uuid.UUID, error) {
				ops := map[repository.FilterOperator]bool{}
				for _, c := range f.Conditions {
					ops[c.Operator] = true
				}
				if !ops[repository.FilterOperatorLike] || !ops[repository.FilterOperatorGte] || !ops[repository.FilterOperatorLte] {
					t.Errorf("conditions = %+v, want title like and created_at between", f.Conditions)
				}
				return []uuid.UUID{a, b}, nil
			})
		var got BulkDeleteResponse
		body := `{"filter":"title=foo;like&created_at=2026-01-01,2026-02-01;between&search=q&page=9&sort=title,ASC"}`
		if s := raw(t, h, http.MethodPost, "/v1/notes/bulk-delete", body, &got); s != http.StatusOK {
			t.Fatalf("status = %d", s)
		}
		if got.Affected != 2 || len(got.Failed) != 0 {
			t.Errorf("response = %+v", got)
		}
	})

	t.Run("too many rows is a 400 with the reason", func(t *testing.T) {
		h, repo := newTestServer(t, PermWrite)
		repo.EXPECT().BulkDelete(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, apierr.New(apierr.ReasonTooManyRows))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/notes/bulk-delete", strings.NewReader(`{"filter":""}`)))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "TOO_MANY_ROWS") {
			t.Errorf("status = %d body %s, want 400 TOO_MANY_ROWS", rec.Code, rec.Body)
		}
	})

	for name, body := range map[string]string{
		"both fields":        `{"ids":["` + a.String() + `"],"filter":""}`,
		"neither field":      `{}`,
		"empty selection":    `{"ids":[]}`,
		"bad id":             `{"ids":["nope"]}`,
		"too many ids":       `{"ids":[` + strings.TrimSuffix(strings.Repeat(`"`+a.String()+`",`, 101), ",") + `]}`,
		"bad filter":         `{"filter":"title=a,b;between"}`,
		"unknown filter key": `{"filter":"body=x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			h, _ := newTestServer(t, PermWrite)
			if s := raw(t, h, http.MethodPost, "/v1/notes/bulk-delete", body, nil); s != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", s)
			}
		})
	}

	t.Run("owner_id filter needs notes:write:any", func(t *testing.T) {
		h, _ := newTestServer(t, PermWrite, PermReadAny)
		body := `{"filter":"owner_id=` + a.String() + `"}`
		if s := raw(t, h, http.MethodPost, "/v1/notes/bulk-delete", body, nil); s != http.StatusForbidden {
			t.Errorf("status = %d, want 403", s)
		}
	})

	t.Run("needs notes:write", func(t *testing.T) {
		h, _ := newTestServer(t, PermRead)
		body := `{"ids":["` + a.String() + `"]}`
		if s := raw(t, h, http.MethodPost, "/v1/notes/bulk-delete", body, nil); s != http.StatusForbidden {
			t.Errorf("status = %d, want 403", s)
		}
	})
}
