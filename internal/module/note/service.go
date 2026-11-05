package note

import (
	"context"
	"fmt"
	"strconv"

	"github.com/biairmal/go-sdk/lib/authz"
	"github.com/biairmal/go-sdk/lib/common/dto"
	"github.com/biairmal/go-sdk/lib/errorz"
	"github.com/biairmal/go-sdk/lib/repository"
	"github.com/google/uuid"

	"github.com/biairmal/go-service-template/internal/platform/apierr"
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
	return &Service{repo: repo, az: az, listQuery: listquery.ListParseConfig{
		MaxSize:             cfg.ListMaxSize,
		AllowedSortFields:   []string{"title", "status", "pinned", "created_at", "updated_at"},
		AllowedFilterFields: []string{"title", "status", "pinned", "owner_id"},
		AllowedParams:       []string{"search", "created_from", "created_to"},
	}}
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
// PermReadAny, filtered and sorted by params and extra. Without a sort the
// newest change comes first. The owner_id filter needs PermReadAny.
func (s *Service) List(
	ctx context.Context, params *listquery.ListParams, extra ListExtra,
) (*dto.PageResponse[Note], error) {
	if err := listquery.ValidateListParams(params, &s.listQuery); err != nil {
		return nil, err
	}
	owners := s.owners(ctx, PermReadAny)
	if _, ok := params.Filters["owner_id"]; ok && owners != AnyOwner {
		return nil, errorz.Forbidden().WithMessage("note: the owner_id filter needs "+PermReadAny).
			WithMeta("permission", PermReadAny)
	}
	opts := listquery.ToListOptions(params)
	if err := typeFilters(opts.Filter.Conditions); err != nil {
		return nil, err
	}
	if err := extra.rangeConditions(&opts.Filter); err != nil {
		return nil, err
	}
	notes, total, err := s.repo.List(ctx, opts, extra.Search, owners)
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

// typeFilters checks each filter value against its column and converts it to
// the column's type. Only title takes ";like"; the rest match exactly.
func typeFilters(conds []repository.FilterCondition) error {
	for i := range conds {
		c := &conds[i]
		raw := fmt.Sprint(c.Value) // ToListOptions leaves every value a string
		if c.Field != "title" && c.Operator != repository.FilterOperatorEq {
			return badQuery(c.Field, "only exact match is supported")
		}
		switch c.Field {
		case "status":
			if raw != StatusDraft && raw != StatusArchived {
				return badQuery(c.Field, "must be draft or archived")
			}
		case "pinned":
			b, err := strconv.ParseBool(raw)
			if err != nil {
				return badQuery(c.Field, "must be true or false")
			}
			c.Value = b
		case "owner_id":
			id, err := uuid.Parse(raw)
			if err != nil {
				return badQuery(c.Field, "must be a UUID")
			}
			c.Value = id
		}
	}
	return nil
}

// rangeConditions adds the half-open created_at range to f. A range whose
// start is after its end is a 400.
func (e ListExtra) rangeConditions(f *repository.Filter) error {
	if e.CreatedFrom != nil && e.CreatedTo != nil && e.CreatedFrom.After(*e.CreatedTo) {
		return badQuery("created_from", "must not be after created_to")
	}
	if e.CreatedFrom != nil {
		f.Conditions = append(f.Conditions,
			repository.FilterCondition{Field: "created_at", Operator: repository.FilterOperatorGte, Value: *e.CreatedFrom})
	}
	if e.CreatedTo != nil {
		f.Conditions = append(f.Conditions,
			repository.FilterCondition{Field: "created_at", Operator: repository.FilterOperatorLt, Value: *e.CreatedTo})
	}
	return nil
}

// badQuery is a 400 naming the query parameter in meta.fields.
func badQuery(field, msg string) error {
	return errorz.BadRequest().WithMessage(field+": "+msg).WithMeta(apierr.MetaFields, map[string]string{field: msg})
}
