package note

//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -source=repository.go -destination=mock_repository_test.go -package=note

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/biairmal/go-sdk/lib/errorz"
	"github.com/biairmal/go-sdk/lib/repository"
	reposql "github.com/biairmal/go-sdk/lib/repository/sql"
	"github.com/biairmal/go-sdk/lib/sqlkit"
	"github.com/google/uuid"

	"github.com/biairmal/go-service-template/internal/platform/apierr"
	"github.com/biairmal/go-service-template/internal/platform/database"
	"github.com/biairmal/go-service-template/internal/platform/scope"
)

// Repository stores notes. Every call is limited to the caller's tenant, and
// to the caller's own notes unless it passes AnyOwner. A note outside that
// scope is not found, so other users' IDs cannot be probed.
type Repository interface {
	// Create inserts a note owned and created by the caller in their tenant.
	Create(ctx context.Context, n *NewNote) (*Note, error)
	// Get returns one note, or errorz.NotFound.
	Get(ctx context.Context, id uuid.UUID, owners Owners) (*Note, error)
	// List returns one page of notes matching opts, and the total matching.
	// A non-empty search matches the title or body, case-insensitively.
	List(ctx context.Context, opts *repository.ListOptions, search string, owners Owners) ([]Note, int64, error)
	// Update writes the patch's columns and bumps the version, provided the
	// stored version is still version: NotFound when the note is out of
	// scope, VERSION_CONFLICT when it is in scope with another version.
	Update(ctx context.Context, id uuid.UUID, version int, p *Patch, owners Owners) (*Note, error)
	// Delete removes one note, or returns errorz.NotFound.
	Delete(ctx context.Context, id uuid.UUID, owners Owners) error
	// BulkDelete removes every note in scope matching f and search, in one
	// transaction, and returns their IDs. More than limit matches deletes
	// nothing and returns TOO_MANY_ROWS.
	BulkDelete(ctx context.Context, f repository.Filter, search string, owners Owners, limit int) ([]uuid.UUID, error)
}

// NewRepository returns the SQL Repository over db.
func NewRepository(db *sqlkit.DB) Repository {
	return &sqlRepository{db: db, base: reposql.NewBaseRepository(db, "note")}
}

type sqlRepository struct {
	db   *sqlkit.DB
	base *reposql.BaseRepository
}

// noteColumns is the select list scanNote reads.
const noteColumns = `id, tenant_id, owner_id, title, body, status, pinned, version, created_at, updated_at, created_by`

// scopeWhere limits a single-note statement to $1 id, $2 tenant and, when
// $3 is not null, $3 owner.
const scopeWhere = ` WHERE id = $1 AND tenant_id = $2 AND ($3::uuid IS NULL OR owner_id = $3)`

func (r *sqlRepository) Create(ctx context.Context, n *NewNote) (*Note, error) {
	tenant, err := scope.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	owner, err := scope.Owner(ctx)
	if err != nil {
		return nil, err
	}
	status := n.Status
	if status == "" {
		status = StatusDraft
	}
	row := r.base.GetConnection(ctx).QueryRowContext(ctx,
		`INSERT INTO note (tenant_id, owner_id, created_by, title, body, status, pinned)
		 VALUES ($1, $2, $2, $3, $4, $5, $6) RETURNING `+noteColumns,
		tenant, owner, n.Title, n.Body, status, n.Pinned)
	return scanNote(row)
}

func (r *sqlRepository) Get(ctx context.Context, id uuid.UUID, owners Owners) (*Note, error) {
	tenant, owner, err := scopeOf(ctx, owners)
	if err != nil {
		return nil, err
	}
	// The leader, not a follower: Update reads here to tell a conflict from a miss.
	row := r.base.GetConnection(ctx).QueryRowContext(ctx,
		`SELECT `+noteColumns+` FROM note`+scopeWhere, id, tenant, owner)
	return scanNote(row)
}

func (r *sqlRepository) List(
	ctx context.Context, opts *repository.ListOptions, search string, owners Owners,
) ([]Note, int64, error) {
	if opts == nil {
		opts = &repository.ListOptions{}
	}
	where, args, err := scopedWhere(ctx, opts.Filter, search, owners)
	if err != nil {
		return nil, 0, err
	}
	conn := r.base.GetReadConnection(ctx)

	var total int64
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM note `+where, args...).Scan(&total); err != nil {
		return nil, 0, errorz.Wrap(err).WithCode(errorz.CodeInternal).WithMessage("note: count")
	}
	order := reposql.BuildOrderByClause(opts.Sorts)
	if order == "" {
		order = "ORDER BY updated_at DESC"
	}
	page, pageArgs := reposql.BuildPaginationClause(reposql.Postgres{}, opts.Pagination, len(args))
	// id last, so rows with equal sort keys keep one order across pages.
	rows, err := conn.QueryContext(ctx,
		`SELECT `+noteColumns+` FROM note `+where+` `+order+`, id `+page, append(args, pageArgs...)...)
	if err != nil {
		return nil, 0, errorz.Wrap(err).WithCode(errorz.CodeInternal).WithMessage("note: list")
	}
	defer func() { _ = rows.Close() }()
	var out []Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, errorz.Wrap(err).WithCode(errorz.CodeInternal).WithMessage("note: list rows")
	}
	return out, total, nil
}

func (r *sqlRepository) Update(
	ctx context.Context, id uuid.UUID, version int, p *Patch, owners Owners,
) (*Note, error) {
	tenant, owner, err := scopeOf(ctx, owners)
	if err != nil {
		return nil, err
	}
	args := []any{id, tenant, owner, version}
	sets := []string{"version = version + 1", "updated_at = now()"}
	set := func(column string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	if p.Title != nil {
		set("title", *p.Title)
	}
	if p.Body != nil {
		set("body", *p.Body)
	}
	if p.Status != nil {
		set("status", *p.Status)
	}
	if p.Pinned != nil {
		set("pinned", *p.Pinned)
	}
	row := r.base.GetConnection(ctx).QueryRowContext(ctx,
		`UPDATE note SET `+strings.Join(sets, ", ")+scopeWhere+` AND version = $4 RETURNING `+noteColumns, args...)
	n, err := scanNote(row)
	if !errors.Is(err, errorz.ErrNotFound) {
		return n, err
	}
	if _, err := r.Get(ctx, id, owners); err != nil {
		return nil, err // out of scope or gone: not found
	}
	return nil, apierr.New(apierr.ReasonVersionConflict)
}

func (r *sqlRepository) Delete(ctx context.Context, id uuid.UUID, owners Owners) error {
	tenant, owner, err := scopeOf(ctx, owners)
	if err != nil {
		return err
	}
	res, err := r.base.GetConnection(ctx).ExecContext(ctx, `DELETE FROM note`+scopeWhere, id, tenant, owner)
	if err != nil {
		return errorz.Wrap(err).WithCode(errorz.CodeInternal).WithMessage("note: delete")
	}
	n, err := res.RowsAffected()
	if err != nil {
		return errorz.Wrap(err).WithCode(errorz.CodeInternal).WithMessage("note: rows affected")
	}
	if n == 0 {
		return errNotFound()
	}
	return nil
}

func (r *sqlRepository) BulkDelete(
	ctx context.Context, f repository.Filter, search string, owners Owners, limit int,
) ([]uuid.UUID, error) {
	where, args, err := scopedWhere(ctx, f, search, owners)
	if err != nil {
		return nil, err
	}
	args = append(args, limit+1)
	query := fmt.Sprintf(`DELETE FROM note WHERE id IN (SELECT id FROM note %s LIMIT $%d FOR UPDATE) RETURNING id`,
		where, len(args))
	var ids []uuid.UUID
	err = database.WithTx(ctx, r.db, func(ctx context.Context) error {
		rows, err := r.base.GetConnection(ctx).QueryContext(ctx, query, args...)
		if err != nil {
			return errorz.Wrap(err).WithCode(errorz.CodeInternal).WithMessage("note: bulk delete")
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return errorz.Wrap(err).WithCode(errorz.CodeInternal).WithMessage("note: bulk delete scan")
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return errorz.Wrap(err).WithCode(errorz.CodeInternal).WithMessage("note: bulk delete rows")
		}
		if len(ids) > limit {
			return apierr.New(apierr.ReasonTooManyRows) // rolls the delete back
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// scopedWhere is the WHERE clause and args for f within the caller's scope,
// plus a case-insensitive title-or-body match when search is set.
func scopedWhere(
	ctx context.Context, f repository.Filter, search string, owners Owners,
) (where string, args []any, err error) {
	conds, err := scopeConditions(ctx, owners)
	if err != nil {
		return "", nil, err
	}
	where, args = reposql.BuildWhereClause(reposql.Postgres{},
		repository.Filter{Conditions: append(conds, f.Conditions...)})
	if search != "" {
		args = append(args, "%"+search+"%")
		where += fmt.Sprintf(" AND (title ILIKE $%d OR body ILIKE $%d)", len(args), len(args))
	}
	return where, args, nil
}

// scopeOf returns the caller's tenant and, for OwnNotes, the caller as owner
// (nil for AnyOwner), as scopeWhere's $2 and $3.
func scopeOf(ctx context.Context, owners Owners) (tenant uuid.UUID, owner *uuid.UUID, err error) {
	tenant, err = scope.Tenant(ctx)
	if err != nil || owners == AnyOwner {
		return tenant, nil, err
	}
	id, err := scope.Owner(ctx)
	if err != nil {
		return uuid.Nil, nil, err
	}
	return tenant, &id, nil
}

// scopeConditions is scopeOf as list filter conditions.
func scopeConditions(ctx context.Context, owners Owners) ([]repository.FilterCondition, error) {
	tenant, owner, err := scopeOf(ctx, owners)
	if err != nil {
		return nil, err
	}
	conds := []repository.FilterCondition{
		{Field: scope.TenantColumn, Operator: repository.FilterOperatorEq, Value: tenant},
	}
	if owner != nil {
		conds = append(conds,
			repository.FilterCondition{Field: scope.OwnerColumn, Operator: repository.FilterOperatorEq, Value: *owner})
	}
	return conds, nil
}

func errNotFound() error { return errorz.NotFound().WithMessage("note: not found") }

// scanNote reads one note, mapping no rows to errorz.NotFound.
func scanNote(row interface{ Scan(dest ...any) error }) (*Note, error) {
	var n Note
	err := row.Scan(&n.ID, &n.TenantID, &n.OwnerID, &n.Title, &n.Body, &n.Status, &n.Pinned, &n.Version,
		&n.CreatedAt, &n.UpdatedAt, &n.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound()
	}
	if err != nil {
		return nil, errorz.Wrap(err).WithCode(errorz.CodeInternal).WithMessage("note: scan")
	}
	return &n, nil
}
