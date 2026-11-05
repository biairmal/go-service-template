// Package note is the example business module: notes owned by a user inside
// a tenant. Copy it to start a new module.
package note

import (
	"time"

	"github.com/google/uuid"
)

// Status values of a note.
const (
	StatusDraft    = "draft"
	StatusArchived = "archived"
)

// Note is one note row. TenantID and OwnerID come from the caller's token,
// never from a request.
type Note struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	OwnerID   uuid.UUID
	Title     string
	Body      string
	Status    string
	Pinned    bool
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
	CreatedBy uuid.UUID
}

// NewNote is what Create writes. The tenant, owner and creator are the caller.
type NewNote struct {
	Title  string
	Body   string
	Status string // empty means StatusDraft
	Pinned bool
}

// Patch names the columns an update writes; a nil field is left untouched.
type Patch struct {
	Title  *string
	Body   *string
	Status *string
	Pinned *bool
}

// Owners says whose notes a repository call may touch. The tenant scope
// always applies.
type Owners bool

const (
	// OwnNotes limits the call to notes the caller owns.
	OwnNotes Owners = false
	// AnyOwner reaches every note in the caller's tenant, for callers holding
	// an ":any" permission.
	AnyOwner Owners = true
)

// ListExtra holds the GET /v1/notes parameters the generic list filters
// can't express: a search over two columns and a range on one.
type ListExtra struct {
	// Search matches the title or body, case-insensitively.
	Search string
	// CreatedFrom and CreatedTo bound created_at as [from, to); nil is open.
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}
