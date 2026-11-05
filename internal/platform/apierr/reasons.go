package apierr

import "github.com/biairmal/go-sdk/lib/errorz"

// Reason is the machine-readable meta.reason clients branch on.
type Reason string

// Every meta.reason the API returns. Validation errors (400 with meta.fields)
// and not found (404) are plain errorz codes and carry no reason.
const (
	ReasonPermissionNotHeld Reason = "PERMISSION_NOT_HELD"
	ReasonVersionConflict   Reason = "VERSION_CONFLICT"
)

// reasonCodes maps each error reason to the errorz code that selects its HTTP
// status, so a reason can never be paired with the wrong status.
var reasonCodes = map[Reason]string{
	ReasonPermissionNotHeld: errorz.CodeForbidden,
	ReasonVersionConflict:   errorz.CodeConflict,
}
