package note_test

import (
	"testing"

	"github.com/biairmal/go-sdk/lib/repository"
	"github.com/google/uuid"

	"github.com/biairmal/go-service-template/internal/module/note"
	"github.com/biairmal/go-service-template/internal/platform/apierr"
	"github.com/biairmal/go-service-template/internal/platform/testkit"
)

func idsIn(ids ...uuid.UUID) repository.Filter {
	values := make([]any, len(ids))
	for i, id := range ids {
		values[i] = id
	}
	return repository.Filter{Conditions: []repository.FilterCondition{
		{Field: "id", Operator: repository.FilterOperatorIn, Values: values},
	}}
}

func TestOptionsByIDsLeaveOtherTenantsOut(t *testing.T) {
	repo := note.NewRepository(testkit.Postgres(t))
	user := uuid.New()
	inA, inB := caller(uuid.New(), user), caller(uuid.New(), user)
	mine, err := repo.Create(inA, &note.NewNote{Title: "mine"})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := repo.Create(inB, &note.NewNote{Title: "theirs"})
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err := repo.List(inA, &repository.ListOptions{Filter: idsIn(mine.ID, theirs.ID)}, "", note.AnyOwner)
	if err != nil || len(rows) != 1 || rows[0].ID != mine.ID {
		t.Errorf("List by ids = %+v, %v; want only tenant A's note", rows, err)
	}
}

func TestBulkDeleteStaysInScope(t *testing.T) {
	repo := note.NewRepository(testkit.Postgres(t))
	user := uuid.New()
	inA, inB := caller(uuid.New(), user), caller(uuid.New(), user)
	mine, _ := repo.Create(inA, &note.NewNote{Title: "mine"})
	theirs, _ := repo.Create(inB, &note.NewNote{Title: "theirs"})

	deleted, err := repo.BulkDelete(inA, idsIn(mine.ID, theirs.ID), "", note.AnyOwner, 100)
	if err != nil || len(deleted) != 1 || deleted[0] != mine.ID {
		t.Fatalf("BulkDelete = %v, %v; want only tenant A's note", deleted, err)
	}
	if _, err := repo.Get(inB, theirs.ID, note.AnyOwner); err != nil {
		t.Errorf("tenant B's note after A's bulk delete: %v, want still there", err)
	}
}

func TestBulkDeleteOverTheCapDeletesNothing(t *testing.T) {
	repo := note.NewRepository(testkit.Postgres(t))
	ctx := caller(uuid.New(), uuid.New())
	for _, title := range []string{"a", "b", "c"} {
		if _, err := repo.Create(ctx, &note.NewNote{Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := repo.BulkDelete(ctx, repository.Filter{}, "", note.OwnNotes, 2)
	if apierr.ReasonOf(err) != apierr.ReasonTooManyRows {
		t.Fatalf("BulkDelete over the cap = %v, want TOO_MANY_ROWS", err)
	}
	if _, total, _ := repo.List(ctx, nil, "", note.OwnNotes); total != 3 {
		t.Errorf("notes after a refused bulk delete = %d, want 3 (rolled back)", total)
	}
	deleted, err := repo.BulkDelete(ctx, repository.Filter{}, "b", note.OwnNotes, 2)
	if err != nil || len(deleted) != 1 {
		t.Errorf("BulkDelete with search = %v, %v; want 1", deleted, err)
	}
}
