package scope

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/biairmal/go-sdk/lib/auth"
	"github.com/biairmal/go-sdk/lib/authz"
	"github.com/biairmal/go-sdk/lib/errorz"
	"github.com/biairmal/go-sdk/lib/repository"
	"github.com/google/uuid"
)

func TestTenant(t *testing.T) {
	tid := uuid.New()
	withClaims := func(c map[string]any) context.Context {
		return auth.ContextWithClaims(context.Background(), auth.NewClaims(c))
	}
	tests := []struct {
		name    string
		ctx     context.Context
		want    uuid.UUID
		wantErr bool
	}{
		{"tenant claim", withClaims(map[string]any{"sub": "u", "tid": tid.String()}), tid, false},
		{"no claims", context.Background(), uuid.Nil, true},
		{"no tenant claim", withClaims(map[string]any{"sub": "u"}), uuid.Nil, true},
		{"not a uuid", withClaims(map[string]any{"sub": "u", "tid": "acme"}), uuid.Nil, true},
		{"nil uuid", withClaims(map[string]any{"sub": "u", "tid": uuid.Nil.String()}), uuid.Nil, true},
		{"wrong type", withClaims(map[string]any{"sub": "u", "tid": 7}), uuid.Nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Tenant(tt.ctx)
			if tt.wantErr {
				var e *errorz.Error
				if !errors.Is(err, ErrNoTenant) || !errors.As(err, &e) || e.Code != errorz.CodeUnauthorized {
					t.Fatalf("Tenant() err = %v, want ErrNoTenant as a 401", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("Tenant() = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestTenantFilter(t *testing.T) {
	tid := uuid.New()
	ctx := auth.ContextWithClaims(context.Background(), auth.NewClaims(map[string]any{"sub": "u", "tid": tid.String()}))
	f, err := TenantFilter(ctx)
	if err != nil {
		t.Fatalf("TenantFilter: %v", err)
	}
	if f.Field != TenantColumn || f.Operator != repository.FilterOperatorEq || f.Value != tid {
		t.Errorf("TenantFilter = %+v", f)
	}
	if _, err := TenantFilter(context.Background()); !errors.Is(err, ErrNoTenant) {
		t.Errorf("TenantFilter without tenant = %v, want ErrNoTenant", err)
	}
}

func TestOwner(t *testing.T) {
	uid := uuid.New()
	withSub := func(sub any) context.Context {
		return auth.ContextWithClaims(context.Background(), auth.NewClaims(map[string]any{"sub": sub}))
	}
	tests := []struct {
		name    string
		ctx     context.Context
		want    uuid.UUID
		wantErr bool
	}{
		{"subject", withSub(uid.String()), uid, false},
		{"no claims", context.Background(), uuid.Nil, true},
		{"not a uuid", withSub("bia"), uuid.Nil, true},
		{"nil uuid", withSub(uuid.Nil.String()), uuid.Nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Owner(tt.ctx)
			if tt.wantErr {
				var e *errorz.Error
				if !errors.Is(err, ErrNoOwner) || !errors.As(err, &e) || e.Code != errorz.CodeUnauthorized {
					t.Fatalf("Owner() err = %v, want ErrNoOwner as a 401", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("Owner() = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

// grants is an authz.Authorizer that holds exactly the listed permissions.
type grants struct {
	authz.Authorizer
	perms []string
}

func (g grants) Can(_ context.Context, perm string) bool { return slices.Contains(g.perms, perm) }

func TestOwnerFilter(t *testing.T) {
	uid := uuid.New()
	ctx := auth.ContextWithClaims(context.Background(), auth.NewClaims(map[string]any{"sub": uid.String()}))
	const anyPerm = "notes:read:any"

	conds, err := OwnerFilter(ctx, grants{perms: []string{"notes:read"}}, anyPerm)
	if err != nil || len(conds) != 1 || conds[0].Field != OwnerColumn ||
		conds[0].Operator != repository.FilterOperatorEq || conds[0].Value != uid {
		t.Fatalf("OwnerFilter for a plain caller = %+v, %v; want owner_id = %v", conds, err, uid)
	}
	conds, err = OwnerFilter(ctx, grants{perms: []string{anyPerm}}, anyPerm)
	if err != nil || len(conds) != 0 {
		t.Fatalf("OwnerFilter with %s = %+v, %v; want no condition", anyPerm, conds, err)
	}
	if _, err := OwnerFilter(context.Background(), grants{}, anyPerm); !errors.Is(err, ErrNoOwner) {
		t.Fatalf("OwnerFilter without a subject = %v, want ErrNoOwner", err)
	}
}
