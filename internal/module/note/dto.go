package note

import (
	"time"

	"github.com/biairmal/go-sdk/lib/common/dto"
)

// CreateNoteRequest is the body of POST /v1/notes. The owner and tenant are
// the caller; a body naming them is rejected as an unknown field.
type CreateNoteRequest struct {
	Title  string `json:"title" validate:"required,max=200"`
	Body   string `json:"body"`
	Status string `json:"status" validate:"omitempty,oneof=draft archived"`
	Pinned bool   `json:"pinned"`
}

// UpdateNoteRequest is the body of PATCH /v1/notes/{id}. Absent fields keep
// their value; the version comes from the If-Match header.
type UpdateNoteRequest struct {
	Title  *string `json:"title" validate:"omitnil,min=1,max=200"`
	Body   *string `json:"body"`
	Status *string `json:"status" validate:"omitnil,oneof=draft archived"`
	Pinned *bool   `json:"pinned"`
}

// NoteResponse is one note. Version goes back in If-Match on the next PATCH.
type NoteResponse struct { //nolint:revive // named for its resource: the package holds several note DTOs
	ID        string    `json:"id"`
	OwnerID   string    `json:"owner_id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Status    string    `json:"status"`
	Pinned    bool      `json:"pinned"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NotePageResponse is the data of GET /v1/notes.
type NotePageResponse = dto.PageResponse[NoteResponse] //nolint:revive // resource-named DTO

func newNoteResponse(n *Note) *NoteResponse {
	return &NoteResponse{
		ID: n.ID.String(), OwnerID: n.OwnerID.String(), Title: n.Title, Body: n.Body, Status: n.Status,
		Pinned: n.Pinned, Version: n.Version, CreatedAt: n.CreatedAt.UTC(), UpdatedAt: n.UpdatedAt.UTC(),
	}
}

// OptionResponse is one dropdown choice: the note's ID and title.
type OptionResponse struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// OptionPageResponse is the data of GET /v1/notes/options.
type OptionPageResponse = dto.PageResponse[OptionResponse]

// BulkDeleteRequest is the body of POST /v1/notes/bulk-delete. Send exactly
// one of IDs (1 to 100) or Filter, a GET /v1/notes query string whose page,
// size and sort are ignored; an empty Filter matches every note in scope.
type BulkDeleteRequest struct {
	IDs    []string `json:"ids" validate:"omitempty,max=100,dive,uuid"`
	Filter *string  `json:"filter" example:"title=foo;like&created_at=2026-01-01,2026-02-01;between"`
}

// BulkDeleteFailure is one selected ID that was not deleted.
type BulkDeleteFailure struct {
	ID     string `json:"id"`
	Reason string `json:"reason" example:"not_found"`
}

// BulkDeleteResponse is the data of POST /v1/notes/bulk-delete.
type BulkDeleteResponse struct {
	Affected int                 `json:"affected"`
	Failed   []BulkDeleteFailure `json:"failed"`
}
