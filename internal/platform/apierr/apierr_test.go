package apierr

import (
	"errors"
	"net/http"
	"testing"

	"github.com/biairmal/go-sdk/lib/errorz"
	"github.com/biairmal/go-sdk/lib/httpkit/handler"
)

// TestReasonStatuses pins every reason to its HTTP status.
func TestReasonStatuses(t *testing.T) {
	want := map[Reason]int{
		ReasonPermissionNotHeld: http.StatusForbidden,
		ReasonVersionConflict:   http.StatusConflict,
	}
	if len(want) != len(reasonCodes) {
		t.Fatalf("test covers %d reasons, table has %d", len(want), len(reasonCodes))
	}
	for reason, status := range want {
		t.Run(string(reason), func(t *testing.T) {
			err := New(reason)
			if got := handler.StatusCodeFromError(err); got != status {
				t.Errorf("status = %d, want %d", got, status)
			}
			if got := ReasonOf(err); got != reason {
				t.Errorf("ReasonOf = %q, want %q", got, reason)
			}
		})
	}
}

func TestUnknownReasonIs500(t *testing.T) {
	if got := handler.StatusCodeFromError(New("TYPO")); got != http.StatusInternalServerError {
		t.Errorf("New(TYPO) status = %d, want 500", got)
	}
}

func TestMetaHelpers(t *testing.T) {
	err := WithFields(ReasonPermissionNotHeld, map[string]string{"permissions": "notes:write is not held"})
	var e *errorz.Error
	if !errors.As(err, &e) || e.Meta[MetaFields].(map[string]string)["permissions"] != "notes:write is not held" {
		t.Errorf("WithFields meta = %v", e.Meta)
	}
	if ReasonOf(errorz.NotFound()) != "" || ReasonOf(nil) != "" {
		t.Error("ReasonOf should be empty for errors without a reason")
	}
}
