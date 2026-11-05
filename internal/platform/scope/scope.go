// Package scope turns the request context into the tenant and owner filters
// repositories apply. The tenant comes only from the token's tenant claim,
// never from a request body or query, and a missing tenant is an error, so a
// forgotten filter fails loudly instead of leaking another tenant's rows.
package scope

import (
	"context"
	"errors"

	"github.com/biairmal/go-sdk/lib/auth"
	"github.com/biairmal/go-sdk/lib/authz"
	"github.com/biairmal/go-sdk/lib/errorz"
	"github.com/biairmal/go-sdk/lib/repository"
	"github.com/google/uuid"
)

// TenantColumn is the tenant column on every tenant-owned table.
const TenantColumn = "tenant_id"

// ErrNoTenant reports a context without a valid tenant claim.
var ErrNoTenant = errors.New("scope: no tenant in context")

// Tenant returns the caller's tenant from the token claim. It fails with a 401
// when the context carries no claims, no tenant claim or a malformed one.
func Tenant(ctx context.Context) (uuid.UUID, error) {
	raw, ok := authz.TenantID(ctx)
	if !ok {
		return uuid.Nil, errorz.Wrap(ErrNoTenant).WithCode(errorz.CodeUnauthorized).WithMessage(ErrNoTenant.Error())
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errorz.Wrap(ErrNoTenant).WithCode(errorz.CodeUnauthorized).
			WithMessage("scope: tenant claim is not a UUID")
	}
	return id, nil
}

// TenantFilter returns the tenant condition to add to repository.ListOptions.
func TenantFilter(ctx context.Context) (repository.FilterCondition, error) {
	id, err := Tenant(ctx)
	if err != nil {
		return repository.FilterCondition{}, err
	}
	return repository.FilterCondition{Field: TenantColumn, Operator: repository.FilterOperatorEq, Value: id}, nil
}

// OwnerColumn is the owner column on every owner-scoped table.
const OwnerColumn = "owner_id"

// ErrNoOwner reports a context without an authenticated subject.
var ErrNoOwner = errors.New("scope: no subject in context")

// Owner returns the caller's user ID from the token subject, the value an
// owner-scoped row stores in owner_id. It fails with a 401 when the context
// carries no subject or a malformed one.
func Owner(ctx context.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(auth.SubjectFromContext(ctx))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errorz.Wrap(ErrNoOwner).WithCode(errorz.CodeUnauthorized).WithMessage(ErrNoOwner.Error())
	}
	return id, nil
}

// OwnerFilter returns the owner condition to add to repository.ListOptions,
// or no condition when the caller holds anyPerm (e.g. "notes:read:any"). The
// tenant filter still applies either way.
func OwnerFilter(ctx context.Context, az authz.Authorizer, anyPerm string) ([]repository.FilterCondition, error) {
	if az.Can(ctx, anyPerm) {
		return nil, nil
	}
	id, err := Owner(ctx)
	if err != nil {
		return nil, err
	}
	return []repository.FilterCondition{{Field: OwnerColumn, Operator: repository.FilterOperatorEq, Value: id}}, nil
}
