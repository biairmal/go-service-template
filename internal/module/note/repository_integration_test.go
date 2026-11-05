package note_test

import (
	"context"
	"errors"
	"testing"

	"github.com/biairmal/go-sdk/lib/auth"
	"github.com/biairmal/go-sdk/lib/errorz"
	"github.com/biairmal/go-sdk/lib/repository"
	"github.com/google/uuid"

	"github.com/biairmal/go-service-template/internal/module/note"
	"github.com/biairmal/go-service-template/internal/platform/apierr"
	"github.com/biairmal/go-service-template/internal/platform/scope"
	"github.com/biairmal/go-service-template/internal/platform/testkit"
)

// caller is the context of user sub in tenant tid, as the auth middleware leaves it.
func caller(tid, sub uuid.UUID) context.Context {
	return auth.ContextWithClaims(context.Background(),
		auth.NewClaims(map[string]any{"sub": sub.String(), "tid": tid.String()}))
}

func isNotFound(err error) bool { return errors.Is(err, errorz.ErrNotFound) }

func TestUserCannotReachAnotherUsersNote(t *testing.T) {
	repo := note.NewRepository(testkit.Postgres(t))
	tenant := uuid.New()
	alice, bob := caller(tenant, uuid.New()), caller(tenant, uuid.New())
	n, err := repo.Create(alice, &note.NewNote{Title: "alice's"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Get(bob, n.ID, note.OwnNotes); !isNotFound(err) {
		t.Errorf("bob Get = %v, want not found", err)
	}
	title := "taken"
	if _, err := repo.Update(bob, n.ID, n.Version, &note.Patch{Title: &title}, note.OwnNotes); !isNotFound(err) {
		t.Errorf("bob Update = %v, want not found (not a conflict, which would leak that the note exists)", err)
	}
	if err := repo.Delete(bob, n.ID, note.OwnNotes); !isNotFound(err) {
		t.Errorf("bob Delete = %v, want not found", err)
	}
	if rows, total, err := repo.List(bob, nil, "", note.OwnNotes); err != nil || total != 0 || len(rows) != 0 {
		t.Errorf("bob List = %d rows, total %d, %v; want none", len(rows), total, err)
	}
	if got, err := repo.Get(alice, n.ID, note.OwnNotes); err != nil || got.Title != "alice's" {
		t.Errorf("alice Get = %+v, %v", got, err)
	}
}

func TestTenantNeverSeesAnotherTenantsNotes(t *testing.T) {
	repo := note.NewRepository(testkit.Postgres(t))
	user := uuid.New()
	inA, inB := caller(uuid.New(), user), caller(uuid.New(), user) // same subject, two tenants
	n, err := repo.Create(inA, &note.NewNote{Title: "a"})
	if err != nil {
		t.Fatal(err)
	}
	for _, owners := range []note.Owners{note.OwnNotes, note.AnyOwner} {
		if _, err := repo.Get(inB, n.ID, owners); !isNotFound(err) {
			t.Errorf("Get from tenant B (owners=%v) = %v, want not found", owners, err)
		}
		if err := repo.Delete(inB, n.ID, owners); !isNotFound(err) {
			t.Errorf("Delete from tenant B (owners=%v) = %v, want not found", owners, err)
		}
		if _, total, err := repo.List(inB, nil, "", owners); err != nil || total != 0 {
			t.Errorf("List from tenant B (owners=%v) = total %d, %v; want 0", owners, total, err)
		}
	}
}

func TestAnyOwnerSkipsTheOwnerFilterOnly(t *testing.T) {
	repo := note.NewRepository(testkit.Postgres(t))
	tenant := uuid.New()
	alice, admin := caller(tenant, uuid.New()), caller(tenant, uuid.New())
	n, err := repo.Create(alice, &note.NewNote{Title: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(caller(uuid.New(), uuid.New()), &note.NewNote{Title: "other tenant"}); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Get(admin, n.ID, note.AnyOwner); err != nil {
		t.Errorf("admin Get with AnyOwner = %v", err)
	}
	if rows, total, err := repo.List(admin, nil, "", note.AnyOwner); err != nil || total != 1 || rows[0].ID != n.ID {
		t.Errorf("admin List with AnyOwner = %v rows, total %d, %v; want only the tenant's note", rows, total, err)
	}
	pinned := true
	got, err := repo.Update(admin, n.ID, n.Version, &note.Patch{Pinned: &pinned}, note.AnyOwner)
	if err != nil || !got.Pinned || got.OwnerID != n.OwnerID {
		t.Errorf("admin Update with AnyOwner = %+v, %v; want pinned, owner unchanged", got, err)
	}
	if err := repo.Delete(admin, n.ID, note.AnyOwner); err != nil {
		t.Errorf("admin Delete with AnyOwner = %v", err)
	}
}

func TestUpdateWritesOnlyNamedColumns(t *testing.T) {
	repo := note.NewRepository(testkit.Postgres(t))
	ctx := caller(uuid.New(), uuid.New())
	n, err := repo.Create(ctx, &note.NewNote{Title: "t", Body: "keep me", Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	if n.Status != note.StatusDraft || n.Version != 1 || n.CreatedBy != n.OwnerID {
		t.Fatalf("created = %+v; want draft, version 1, created by the owner", n)
	}
	title := "renamed"
	got, err := repo.Update(ctx, n.ID, 1, &note.Patch{Title: &title}, note.OwnNotes)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "renamed" || got.Body != "keep me" || !got.Pinned || got.Status != note.StatusDraft ||
		got.Version != 2 || !got.UpdatedAt.After(n.UpdatedAt) {
		t.Errorf("after a title-only update = %+v; want only title, version and updated_at changed", got)
	}
}

func TestStaleVersionIsAConflict(t *testing.T) {
	repo := note.NewRepository(testkit.Postgres(t))
	ctx := caller(uuid.New(), uuid.New())
	n, err := repo.Create(ctx, &note.NewNote{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	title := "first"
	if _, err := repo.Update(ctx, n.ID, 1, &note.Patch{Title: &title}, note.OwnNotes); err != nil {
		t.Fatal(err)
	}
	title = "second"
	_, err = repo.Update(ctx, n.ID, 1, &note.Patch{Title: &title}, note.OwnNotes)
	if apierr.ReasonOf(err) != apierr.ReasonVersionConflict {
		t.Fatalf("update with version 1 after it moved to 2 = %v, want VERSION_CONFLICT", err)
	}
	if got, _ := repo.Get(ctx, n.ID, note.OwnNotes); got.Title != "first" {
		t.Errorf("the stale update changed the note: %+v", got)
	}
}

func TestListPagesNewestFirst(t *testing.T) {
	repo := note.NewRepository(testkit.Postgres(t))
	ctx := caller(uuid.New(), uuid.New())
	var ids []uuid.UUID
	for _, title := range []string{"1", "2", "3"} {
		n, err := repo.Create(ctx, &note.NewNote{Title: title})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}
	rows, total, err := repo.List(ctx, &repository.ListOptions{Pagination: repository.Pagination{Limit: 2, Offset: 0}}, "",
		note.OwnNotes)
	if err != nil || total != 3 || len(rows) != 2 || rows[0].ID != ids[2] || rows[1].ID != ids[1] {
		t.Fatalf("first page = %v, total %d, %v; want the two newest of 3", rows, total, err)
	}
	rows, _, err = repo.List(ctx, &repository.ListOptions{Pagination: repository.Pagination{Limit: 2, Offset: 2}}, "",
		note.OwnNotes)
	if err != nil || len(rows) != 1 || rows[0].ID != ids[0] {
		t.Fatalf("second page = %v, %v; want the oldest", rows, err)
	}
}

func TestNoTenantInContextFails(t *testing.T) {
	repo := note.NewRepository(testkit.Postgres(t))
	_, err := repo.Create(context.Background(), &note.NewNote{Title: "t"})
	if !errors.Is(err, scope.ErrNoTenant) {
		t.Fatalf("Create without claims = %v, want scope.ErrNoTenant (a 401)", err)
	}
}
