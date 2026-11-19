package note

import (
	"context"
	"fmt"
	"strconv"

	"github.com/biairmal/go-sdk/lib/authz"
	"github.com/biairmal/go-sdk/lib/common/dto"
	"github.com/biairmal/go-sdk/lib/errorz"
	"github.com/biairmal/go-sdk/lib/repository"
	"github.com/biairmal/go-sdk/lib/tracer"
	"github.com/google/uuid"

	"github.com/biairmal/go-service-template/internal/platform/apierr"
	"github.com/biairmal/go-service-template/internal/platform/listquery"
	"github.com/biairmal/go-service-template/internal/platform/tracing"
)

// Service holds the note rules: who may reach which notes. It knows nothing
// about HTTP.
type Service struct {
	repo      Repository
	az        authz.Authorizer
	tr        tracer.Tracer
	listQuery listquery.ListParseConfig
	optsQuery listquery.ListParseConfig
}

// Bounds of the options and bulk-delete endpoints.
const (
	optionsMaxSize  = 50
	maxSelectedIDs  = 100
	bulkDeleteLimit = 1000
)

// NewService returns a Service over repo, checking ":any" permissions with az
// and starting spans with tr.
func NewService(cfg Config, repo Repository, az authz.Authorizer, tr tracer.Tracer) *Service {
	eq := []repository.FilterOperator{repository.FilterOperatorEq}
	return &Service{repo: repo, az: az, tr: tr, listQuery: listquery.ListParseConfig{
		MaxSize:             cfg.ListMaxSize,
		AllowedSortFields:   []string{"title", "status", "pinned", "created_at", "updated_at"},
		AllowedFilterFields: []string{"id", "title", "status", "pinned", "owner_id", "created_at"},
		FilterOperators: map[string][]repository.FilterOperator{
			"title": {repository.FilterOperatorEq, repository.FilterOperatorNe, repository.FilterOperatorLike},
			"created_at": {
				repository.FilterOperatorGt, repository.FilterOperatorGte, repository.FilterOperatorLt,
				repository.FilterOperatorLte, listquery.OperatorBetween,
			},
			"id":     {repository.FilterOperatorEq, repository.FilterOperatorIn},
			"status": eq, "pinned": eq, "owner_id": eq,
		},
		AllowedParams: []string{"search", "created_from", "created_to"},
	}, optsQuery: listquery.ListParseConfig{
		MaxSize:       optionsMaxSize,
		AllowedParams: []string{"search", "ids"},
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
) (_ *dto.PageResponse[Note], err error) {
	ctx, span := s.tr.Start(ctx, "note.List")
	defer func() { tracing.End(span, err) }()

	owners := s.owners(ctx, PermReadAny)
	opts, err := s.listOptions(params, extra, owners, PermReadAny)
	if err != nil {
		return nil, err
	}
	notes, total, err := s.repo.List(ctx, opts, extra.Search, owners)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(map[string]any{"note.count": len(notes), "note.total": total})
	return pageOf(notes, total, params.Page, opts.Pagination.Limit), nil
}

// Options returns one page of the notes a dropdown may offer, in the list's
// scope, ordered by title then id so infinite scroll is stable. A non-empty
// ids returns exactly those notes instead, ignoring search and paging; IDs
// out of scope are left out.
//
// shortcut: reads whole rows through List; add a two-column query when bodies get large.
func (s *Service) Options(
	ctx context.Context, params *listquery.ListParams, search string, ids []uuid.UUID,
) (*dto.PageResponse[Note], error) {
	if err := listquery.ValidateListParams(params, &s.optsQuery); err != nil {
		return nil, err
	}
	if len(ids) > maxSelectedIDs {
		return nil, badQuery("ids", fmt.Sprintf("at most %d ids", maxSelectedIDs))
	}
	opts := listquery.ToListOptions(params)
	opts.Sorts = []repository.Sort{{Field: "title", Direction: repository.SortAsc}}
	page := params.Page
	if len(ids) > 0 {
		search, page = "", 1
		opts.Filter.Conditions = []repository.FilterCondition{idsIn(ids)}
		opts.Pagination = repository.Pagination{Limit: len(ids)}
	}
	notes, total, err := s.repo.List(ctx, opts, search, s.owners(ctx, PermReadAny))
	if err != nil {
		return nil, err
	}
	return pageOf(notes, total, page, opts.Pagination.Limit), nil
}

// BulkDeleteIDs deletes the given notes in one transaction and returns the
// IDs deleted. IDs out of the write scope are skipped, as if they did not
// exist. There is no version check: bulk deletes are last-write-wins.
func (s *Service) BulkDeleteIDs(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 || len(ids) > maxSelectedIDs {
		return nil, badQuery("ids", fmt.Sprintf("1 to %d ids", maxSelectedIDs))
	}
	f := repository.Filter{Conditions: []repository.FilterCondition{idsIn(ids)}}
	return s.repo.BulkDelete(ctx, f, "", s.owners(ctx, PermWriteAny), maxSelectedIDs)
}

// BulkDeleteMatching deletes every note in the write scope matching the
// list's params and extra (paging and sort ignored), in one transaction. More
// than bulkDeleteLimit matches deletes nothing: TOO_MANY_ROWS.
func (s *Service) BulkDeleteMatching(
	ctx context.Context, params *listquery.ListParams, extra ListExtra,
) ([]uuid.UUID, error) {
	owners := s.owners(ctx, PermWriteAny)
	opts, err := s.listOptions(params, extra, owners, PermWriteAny)
	if err != nil {
		return nil, err
	}
	return s.repo.BulkDelete(ctx, opts.Filter, extra.Search, owners, bulkDeleteLimit)
}

// listOptions checks params against the list's allow-lists and turns them and
// extra into typed repository options. The owner_id filter needs anyPerm.
func (s *Service) listOptions(
	params *listquery.ListParams, extra ListExtra, owners Owners, anyPerm string,
) (*repository.ListOptions, error) {
	if err := listquery.ValidateListParams(params, &s.listQuery); err != nil {
		return nil, err
	}
	if _, ok := params.Filters["owner_id"]; ok && owners != AnyOwner {
		return nil, errorz.Forbidden().WithMessage("note: the owner_id filter needs "+anyPerm).
			WithMeta("permission", anyPerm)
	}
	opts := listquery.ToListOptions(params)
	if err := typeFilters(opts.Filter.Conditions); err != nil {
		return nil, err
	}
	if err := extra.rangeConditions(&opts.Filter); err != nil {
		return nil, err
	}
	return opts, nil
}

// idsIn is the condition id IN ids. Never call it with no ids: go-sdk drops
// an empty IN, which would widen the statement to every row.
func idsIn(ids []uuid.UUID) repository.FilterCondition {
	values := make([]any, len(ids))
	for i, id := range ids {
		values[i] = id
	}
	return repository.FilterCondition{Field: "id", Operator: repository.FilterOperatorIn, Values: values}
}

// pageOf wraps one page of notes.
func pageOf(notes []Note, total int64, page, size int) *dto.PageResponse[Note] {
	items := make([]*Note, len(notes))
	for i := range notes {
		items[i] = &notes[i]
	}
	return dto.NewPageResponse(items, total, page, size)
}

// Update applies p to one of the caller's notes (any in the tenant with
// PermWriteAny), provided its version is still version.
func (s *Service) Update(ctx context.Context, id uuid.UUID, version int, p *Patch) (_ *Note, err error) {
	ctx, span := s.tr.Start(ctx, "note.Update", tracer.WithAttributes(map[string]any{"note.id": id.String()}))
	defer func() { tracing.End(span, err) }()

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
// the column's type. Which operators a field takes is listQuery's job.
func typeFilters(conds []repository.FilterCondition) error {
	for i := range conds {
		c := &conds[i]
		raw := fmt.Sprint(c.Value) // ToListOptions leaves every value a string
		switch c.Field {
		case "created_at":
			t, err := queryTime(raw, c.Field)
			if err != nil || t == nil {
				return badQuery(c.Field, "must be an RFC 3339 date or timestamp")
			}
			c.Value = *t
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
		case "owner_id", "id":
			if err := typeUUIDs(c); err != nil {
				return err
			}
		}
	}
	return nil
}

// typeUUIDs converts a UUID condition's value, or each of an "in"'s values.
func typeUUIDs(c *repository.FilterCondition) error {
	parse := func(v any) (uuid.UUID, error) {
		id, err := uuid.Parse(fmt.Sprint(v))
		if err != nil {
			return uuid.Nil, badQuery(c.Field, "must be a UUID")
		}
		return id, nil
	}
	if c.Operator != repository.FilterOperatorIn {
		id, err := parse(c.Value)
		c.Value = id
		return err
	}
	for i, v := range c.Values {
		id, err := parse(v)
		if err != nil {
			return err
		}
		c.Values[i] = id
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
