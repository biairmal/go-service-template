package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/biairmal/go-sdk/lib/errorz"
	"github.com/biairmal/go-sdk/lib/logger"

	"github.com/biairmal/go-service-template/internal/platform/database"
	"github.com/biairmal/go-service-template/internal/platform/testkit"
	"github.com/biairmal/go-service-template/migrations"
)

// TestServeRefusesASchemaThatIsBehind reverts the last migration and checks
// New fails with the hint to migrate, then succeeds once the schema is current.
func TestServeRefusesASchemaThatIsBehind(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := context.Background()
	m, err := database.NewMigrator(db.Leader(), migrations.FS)
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	if _, err := m.Down(ctx); err != nil {
		t.Fatalf("Down: %v", err)
	}
	t.Cleanup(func() {
		if _, err := m.Up(ctx); err != nil {
			t.Errorf("restore schema: %v", err)
		}
	})

	cfg := validConfig(t)
	cfg.Database = testkit.PostgresConfig(t)
	_, err = New(ctx, &cfg, logger.NewNoOp())
	if !errors.Is(err, errorz.ErrPreconditionFailed) || !strings.Contains(err.Error(), "run `server migrate up`") {
		t.Fatalf("New on a schema behind = %v, want precondition failed with the migrate hint", err)
	}

	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	a, err := New(ctx, &cfg, logger.NewNoOp())
	if err != nil {
		t.Fatalf("New on a current schema: %v", err)
	}
	a.close(ctx)
}
