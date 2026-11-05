package app

import (
	"context"
	"testing"
	"time"

	"github.com/biairmal/go-sdk/lib/logger"

	"github.com/biairmal/go-service-template/internal/platform/database"
	"github.com/biairmal/go-service-template/internal/platform/testkit"
)

// TestWorkerRecordsUnsendableRowsAndStopsCleanly runs the real worker: a row
// whose topic has no sender is claimed, stays unsent with last_error set, and
// canceling the context (what SIGTERM does) stops the worker without error.
func TestWorkerRecordsUnsendableRowsAndStopsCleanly(t *testing.T) {
	db := testkit.Postgres(t)
	cfg := validConfig(t)
	cfg.Database = testkit.PostgresConfig(t)
	ob := database.NewOutbox(db)
	if err := ob.Enqueue(context.Background(), "nobody.listens", "k", map[string]int{"n": 1}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunWorker(ctx, &cfg, logger.NewNoOp()) }()

	waitFor(t, func() bool {
		var attempts int
		var lastError string
		var sent bool
		err := db.Leader().QueryRowContext(context.Background(),
			`SELECT attempts, last_error, sent_at IS NOT NULL FROM outbox WHERE key = 'k'`).
			Scan(&attempts, &lastError, &sent)
		if err != nil {
			t.Fatalf("read outbox row: %v", err)
		}
		if sent {
			t.Fatal("a row without a sender was marked sent")
		}
		return attempts > 0 && lastError != ""
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunWorker returned %v, want nil (clean exit)", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
}
