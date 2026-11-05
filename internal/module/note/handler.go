package note

import (
	"net/http"
	"time"

	"github.com/biairmal/go-sdk/lib/authz"
	"github.com/biairmal/go-sdk/lib/errorz"
	"github.com/biairmal/go-sdk/lib/httpkit/middleware"
	"github.com/biairmal/go-sdk/lib/httpkit/response"
	"github.com/biairmal/go-sdk/lib/validator"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/biairmal/go-service-template/internal/platform/httpx"
	"github.com/biairmal/go-service-template/internal/platform/listquery"
)

// Handler is the HTTP side of notes: decode, validate, call the service,
// map the result to a response DTO.
type Handler struct {
	svc *Service
	v   validator.Validator
	az  authz.Authorizer
}

// NewHandler returns the note handlers.
func NewHandler(svc *Service, v validator.Validator, az authz.Authorizer) *Handler {
	return &Handler{svc: svc, v: v, az: az}
}

// Register mounts the note routes on r (/v1/notes), each behind its permission.
func (h *Handler) Register(r chi.Router) {
	read := middleware.RequireAny(h.az, PermRead, PermReadAny)
	write := middleware.RequirePermission(h.az, PermWrite)
	r.With(write).Post("/", httpx.Handle(h.create))
	r.With(read).Get("/", httpx.Handle(h.list))
	r.With(read).Get("/{id}", httpx.Handle(h.get))
	r.With(write).Patch("/{id}", httpx.Handle(h.update))
	r.With(write).Delete("/{id}", httpx.Handle(h.delete))
}

// create answers POST /v1/notes with 201 and the new note.
//
//	@Summary     Create a note
//	@Description Creates a note owned by the caller in the caller's tenant. Needs notes:write.
//	@Tags        notes
//	@Accept      json
//	@Produce     json
//	@Param       body body note.CreateNoteRequest true "Request body"
//	@Security    BearerAuth
//	@Success     201 {object} response.BaseResponse[note.NoteResponse]
//	@Failure     400 {object} response.BaseResponse[any] "Invalid body or validation error (meta.fields)"
//	@Failure     401 {object} response.BaseResponse[any] "Missing, invalid or expired token"
//	@Failure     403 {object} response.BaseResponse[any] "No notes:write"
//	@Failure     500 {object} response.BaseResponse[any] "Internal error"
//	@Router      /v1/notes [post]
func (h *Handler) create(r *http.Request) (any, error) {
	var in CreateNoteRequest
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return nil, err
	}
	if err := h.v.ValidateStruct(&in); err != nil {
		return nil, err
	}
	n, err := h.svc.Create(r.Context(), &NewNote{Title: in.Title, Body: in.Body, Status: in.Status, Pinned: in.Pinned})
	if err != nil {
		return nil, err
	}
	return &response.Success{HTTPStatusCode: http.StatusCreated, Data: newNoteResponse(n)}, nil
}

// list answers GET /v1/notes with one page of notes, newest first.
//
//	@Summary     List notes
//	@Description The caller's notes, or the tenant's with notes:read:any. Needs notes:read or notes:read:any.
//	@Description Filters: field=value, or title=value%3Blike for contains (the ; must be percent-encoded).
//	@Description Sortable: title, status, pinned, created_at, updated_at; default updated_at,DESC.
//	@Tags        notes
//	@Produce     json
//	@Param       page query int false "Page, 1-based"
//	@Param       size query int false "Page size, max modules.note.list_max_size"
//	@Param       sort         query []string false "field,ASC|DESC; repeatable" collectionFormat(multi)
//	@Param       title        query string   false "Exact title, or value%3Blike for contains (case-sensitive)"
//	@Param       status       query string   false "draft or archived"
//	@Param       pinned       query bool     false "true or false"
//	@Param       owner_id     query string   false "One owner's notes; needs notes:read:any"
//	@Param       search       query string   false "Title or body contains, case-insensitive"
//	@Param       created_from query string   false "created_at >= this; RFC 3339 date or timestamp"
//	@Param       created_to   query string   false "created_at < this; RFC 3339 date or timestamp"
//	@Security    BearerAuth
//	@Success     200 {object} response.BaseResponse[note.NotePageResponse]
//	@Failure     400 {object} response.BaseResponse[any] "Unknown key, disallowed sort or filter, bad value or range"
//	@Failure     401 {object} response.BaseResponse[any] "Missing, invalid or expired token"
//	@Failure     403 {object} response.BaseResponse[any] "No read permission, or owner_id without notes:read:any"
//	@Failure     500 {object} response.BaseResponse[any] "Internal error"
//	@Router      /v1/notes [get]
func (h *Handler) list(r *http.Request) (any, error) {
	q := r.URL.Query()
	params, err := listquery.ParseListParams(q, &h.svc.listQuery)
	if err != nil {
		return nil, err
	}
	extra := ListExtra{Search: q.Get("search")}
	if extra.CreatedFrom, err = queryTime(q.Get("created_from"), "created_from"); err != nil {
		return nil, err
	}
	if extra.CreatedTo, err = queryTime(q.Get("created_to"), "created_to"); err != nil {
		return nil, err
	}
	page, err := h.svc.List(r.Context(), params, extra)
	if err != nil {
		return nil, err
	}
	items := make([]*NoteResponse, len(page.Items))
	for i, n := range page.Items {
		items[i] = newNoteResponse(n)
	}
	return &NotePageResponse{
		Items: items, Total: page.Total, Page: page.Page, Size: page.Size, TotalPages: page.TotalPages,
		HasPrev: page.HasPrev, HasNext: page.HasNext,
	}, nil
}

// get answers GET /v1/notes/{id}.
//
//	@Summary     Get a note
//	@Description One of the caller's notes, or any in the tenant with notes:read:any.
//	@Tags        notes
//	@Produce     json
//	@Param       id path string true "Note ID"
//	@Security    BearerAuth
//	@Success     200 {object} response.BaseResponse[note.NoteResponse]
//	@Failure     401 {object} response.BaseResponse[any] "Missing, invalid or expired token"
//	@Failure     403 {object} response.BaseResponse[any] "No notes:read or notes:read:any"
//	@Failure     404 {object} response.BaseResponse[any] "No such note, or not the caller's"
//	@Failure     500 {object} response.BaseResponse[any] "Internal error"
//	@Router      /v1/notes/{id} [get]
func (h *Handler) get(r *http.Request) (any, error) {
	id, err := noteID(r)
	if err != nil {
		return nil, err
	}
	n, err := h.svc.Get(r.Context(), id)
	if err != nil {
		return nil, err
	}
	return newNoteResponse(n), nil
}

// update answers PATCH /v1/notes/{id} with the changed note.
//
//	@Summary     Update a note
//	@Description Changes the fields sent. If-Match carries the version read last; a stale one is 409.
//	@Tags        notes
//	@Accept      json
//	@Produce     json
//	@Param       id       path   string                  true "Note ID"
//	@Param       If-Match header string                  true "Version, e.g. \"3\""
//	@Param       body     body   note.UpdateNoteRequest  true "Fields to change"
//	@Security    BearerAuth
//	@Success     200 {object} response.BaseResponse[note.NoteResponse]
//	@Failure     400 {object} response.BaseResponse[any] "Invalid body or validation error (meta.fields)"
//	@Failure     401 {object} response.BaseResponse[any] "Missing, invalid or expired token"
//	@Failure     403 {object} response.BaseResponse[any] "No notes:write"
//	@Failure     404 {object} response.BaseResponse[any] "No such note, or not the caller's without notes:write:any"
//	@Failure     409 {object} response.BaseResponse[any] "VERSION_CONFLICT"
//	@Failure     428 {object} response.BaseResponse[any] "No If-Match"
//	@Failure     500 {object} response.BaseResponse[any] "Internal error"
//	@Router      /v1/notes/{id} [patch]
func (h *Handler) update(r *http.Request) (any, error) {
	id, err := noteID(r)
	if err != nil {
		return nil, err
	}
	version, err := httpx.Version(r, nil)
	if err != nil {
		return nil, err
	}
	var in UpdateNoteRequest
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return nil, err
	}
	if err := h.v.ValidateStruct(&in); err != nil {
		return nil, err
	}
	if in == (UpdateNoteRequest{}) {
		return nil, errorz.BadRequest().WithMessage("body must set at least one field")
	}
	n, err := h.svc.Update(r.Context(), id, version,
		&Patch{Title: in.Title, Body: in.Body, Status: in.Status, Pinned: in.Pinned})
	if err != nil {
		return nil, err
	}
	return newNoteResponse(n), nil
}

// delete answers DELETE /v1/notes/{id} with 204.
//
//	@Summary     Delete a note
//	@Description Deletes one of the caller's notes, or any in the tenant with notes:write:any.
//	@Tags        notes
//	@Param       id path string true "Note ID"
//	@Security    BearerAuth
//	@Success     204
//	@Failure     401 {object} response.BaseResponse[any] "Missing, invalid or expired token"
//	@Failure     403 {object} response.BaseResponse[any] "No notes:write"
//	@Failure     404 {object} response.BaseResponse[any] "No such note, or not the caller's without notes:write:any"
//	@Failure     500 {object} response.BaseResponse[any] "Internal error"
//	@Router      /v1/notes/{id} [delete]
func (h *Handler) delete(r *http.Request) (any, error) {
	id, err := noteID(r)
	if err != nil {
		return nil, err
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		return nil, err
	}
	return &response.Success{HTTPStatusCode: http.StatusNoContent}, nil
}

// noteID reads the {id} path parameter. A malformed ID names no note: 404.
func noteID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, errNotFound()
	}
	return id, nil
}

// queryTime parses an RFC 3339 date (midnight UTC) or timestamp; empty is nil.
func queryTime(raw, field string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	for _, layout := range []string{time.DateOnly, time.RFC3339} {
		if t, err := time.Parse(layout, raw); err == nil {
			return &t, nil
		}
	}
	return nil, badQuery(field, "must be an RFC 3339 date or timestamp")
}
