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
