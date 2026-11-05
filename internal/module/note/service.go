package note

import (
	"context"

	"github.com/biairmal/go-sdk/lib/authz"
	"github.com/biairmal/go-sdk/lib/common/dto"
	"github.com/google/uuid"

	"github.com/biairmal/go-service-template/internal/platform/listquery"
)

// Service holds the note rules: who may reach which notes. It knows nothing
// about HTTP.
type Service struct {
	repo      Repository
	az        authz.Authorizer
	listQuery listquery.ListParseConfig
}

// NewService returns a Service over repo, checking ":any" permissions with az.
func NewService(cfg Config, repo Repository, az authz.Authorizer) *Service {
	return &Service{repo: repo, az: az, listQuery: listquery.ListParseConfig{MaxSize: cfg.ListMaxSize}}
}

// Create stores a note owned by the caller in the caller's tenant.
func (s *Service) Create(ctx context.Context, n *NewNote) (*Note, error) {
	return s.repo.Create(ctx, n)
}

// Get returns one of the caller's notes, or any note in the tenant with
// PermReadAny. Anything else is not found.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Note, error) {
	return s.repo.Get(ctx, id, s.owners(ctx, PermReadAny))
}

// List returns one page of the caller's notes, or of the tenant's with
// PermReadAny.
func (s *Service) List(ctx context.Context, params *listquery.ListParams) (*dto.PageResponse[Note], error) {
	if err := listquery.ValidateListParams(params, &s.listQuery); err != nil {
		return nil, err
	}
	opts := listquery.ToListOptions(params)
	notes, total, err := s.repo.List(ctx, opts, s.owners(ctx, PermReadAny))
	if err != nil {
		return nil, err
	}
	items := make([]*Note, len(notes))
	for i := range notes {
		items[i] = &notes[i]
	}
	return dto.NewPageResponse(items, total, params.Page, opts.Pagination.Limit), nil
}

// Update applies p to one of the caller's notes (any in the tenant with
// PermWriteAny), provided its version is still version.
func (s *Service) Update(ctx context.Context, id uuid.UUID, version int, p *Patch) (*Note, error) {
	return s.repo.Update(ctx, id, version, p, s.owners(ctx, PermWriteAny))
}

// Delete removes one of the caller's notes (any in the tenant with PermWriteAny).
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id, s.owners(ctx, PermWriteAny))
}

// owners widens a call to every owner when the caller holds anyPerm.
func (s *Service) owners(ctx context.Context, anyPerm string) Owners {
	if s.az.Can(ctx, anyPerm) {
		return AnyOwner
	}
	return OwnNotes
}
