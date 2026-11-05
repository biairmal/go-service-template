package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/biairmal/go-sdk/lib/errorz"
	"github.com/biairmal/go-sdk/lib/httpkit/handler"

	"github.com/biairmal/go-service-template/internal/platform/apierr"
)

type createRole struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	Version     *int     `json:"version"`
}

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var e *errorz.Error
	if !errors.As(err, &e) {
		return nil
	}
	f, ok := e.Meta[apierr.MetaFields].(map[string]string)
	if !ok {
		return nil
	}
	return f
}

func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantErr   bool
		wantField string
	}{
		{"valid", `{"name":"ops","permissions":["a:b"]}`, false, ""},
		{"unknown field", `{"name":"ops","tenant_id":"x"}`, true, "tenant_id"},
		{"wrong type", `{"name":7}`, true, "name"},
		{"empty body", ``, true, ""},
		{"not json", `name=ops`, true, ""},
		{"truncated", `{"name":"ops"`, true, ""},
		{"trailing data", `{"name":"ops"} {"name":"x"}`, true, ""},
		{"too large", `{"name":"` + strings.Repeat("a", MaxBodyBytes) + `"}`, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			var dst createRole
			err := DecodeJSON(r, &dst)
			if !tt.wantErr {
				if err != nil || dst.Name != "ops" {
					t.Fatalf("DecodeJSON = %v, dst %+v", err, dst)
				}
				return
			}
			if handler.StatusCodeFromError(err) != http.StatusBadRequest {
				t.Fatalf("DecodeJSON err = %v, want a 400", err)
			}
			if tt.wantField != "" && fieldsOf(t, err)[tt.wantField] == "" {
				t.Errorf("meta.fields = %v, want an entry for %q", fieldsOf(t, err), tt.wantField)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	three := 3
	zero := 0
	tests := []struct {
		name       string
		header     string
		body       *int
		want       int
		wantStatus int
	}{
		{"quoted header", `"3"`, nil, 3, 0},
		{"weak etag", `W/"4"`, nil, 4, 0},
		{"bare number", `5`, nil, 5, 0},
		{"header wins over body", `"6"`, &three, 6, 0},
		{"body version", ``, &three, 3, 0},
		{"missing is 428", ``, nil, 0, http.StatusPreconditionRequired},
		{"garbage header", `"abc"`, nil, 0, http.StatusBadRequest},
		{"zero body version", ``, &zero, 0, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPatch, "/", http.NoBody)
			if tt.header != "" {
				r.Header.Set(IfMatchHeader, tt.header)
			}
			got, err := Version(r, tt.body)
			if tt.wantStatus != 0 {
				if s := handler.StatusCodeFromError(err); s != tt.wantStatus {
					t.Fatalf("Version err = %v (status %d), want %d", err, s, tt.wantStatus)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("Version = %d, %v; want %d", got, err, tt.want)
			}
		})
	}
	err := ErrVersionConflict()
	if handler.StatusCodeFromError(err) != http.StatusConflict || apierr.ReasonOf(err) != apierr.ReasonVersionConflict {
		t.Errorf("ErrVersionConflict = %v, want 409 VERSION_CONFLICT", err)
	}
}

func TestHandle(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantReason bool
	}{
		{"reasoned error", apierr.New(apierr.ReasonPermissionNotHeld), http.StatusForbidden, true},
		{"plain error", errorz.NotFound(), http.StatusNotFound, false},
		{"success", nil, http.StatusOK, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := Handle(func(*http.Request) (any, error) {
				if tt.err != nil {
					return nil, tt.err
				}
				return map[string]string{"ok": "yes"}, nil
			})
			rec := httptest.NewRecorder()
			h(rec, httptest.NewRequest(http.MethodPost, "/", http.NoBody))
			if rec.Code != tt.wantStatus {
				t.Errorf("status %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := strings.Contains(rec.Body.String(), `"reason":"`); got != tt.wantReason {
				t.Errorf("meta.reason in body = %v, want %v: %s", got, tt.wantReason, rec.Body.String())
			}
		})
	}
}
