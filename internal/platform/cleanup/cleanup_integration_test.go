package cleanup

import (
	"context"
	"testing"
	"time"

	"github.com/biairmal/go-sdk/lib/sqlkit"

	"github.com/biairmal/go-service-template/internal/platform/testkit"
)

// now is the fixed point the fake clock starts at.
var now = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

// TestRunOnceRemovesOnlyRowsPastTheirWindow is the acceptance criterion: sent
// and expired outbox rows go, every row inside its window stays.
func TestRunOnceRemovesOnlyRowsPastTheirWindow(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := context.Background()
	job := New(db, testkit.NewClock(now), nil)

	insertOutbox(ctx, t, db, "sent-recently", now.Add(-time.Hour), ptr(now.Add(-time.Hour)))
	insertOutbox(ctx, t, db, "sent-long-ago", now.Add(-sentRetention-time.Hour),
		ptr(now.Add(-sentRetention-time.Hour)))
	insertOutbox(ctx, t, db, "unsent-recent", now.Add(-time.Hour), nil)
	insertOutbox(ctx, t, db, "unsent-ancient", now.Add(-retention-time.Hour), nil)

	deleted, err := job.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce() = %v", err)
	}
	for table, want := range map[string]int64{"outbox_sent": 1, "outbox_unsendable": 1} {
		if deleted[table] != want {
			t.Errorf("deleted[%s] = %d, want %d", table, deleted[table], want)
		}
	}
	assertKeys(ctx, t, db, `SELECT key FROM outbox`, []string{"sent-recently", "unsent-recent"})
}

// TestRunOnceOnAnEmptyDatabase checks the job is a no-op when there is
// nothing to delete, so a daily run on a quiet system does nothing.
func TestRunOnceOnAnEmptyDatabase(t *testing.T) {
	db := testkit.Postgres(t)
	job := New(db, testkit.NewClock(now), nil)

	deleted, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() = %v", err)
	}
	for table, n := range deleted {
		if n != 0 {
			t.Errorf("deleted[%s] = %d on an empty database, want 0", table, n)
		}
	}
}

// TestRunOnceRespectsTheBatchSize checks one pass is bounded, so a large
// backlog never locks a table for long, and that repeating drains it.
func TestRunOnceRespectsTheBatchSize(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := context.Background()
	job := New(db, testkit.NewClock(now), nil)
	job.batch = 2

	sent := ptr(now.Add(-sentRetention - time.Hour))
	for i := range 5 {
		insertOutbox(ctx, t, db, "sent-"+string(rune('a'+i)), *sent, sent)
	}

	deleted, err := job.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce() = %v", err)
	}
	if deleted["outbox_sent"] != 2 {
		t.Fatalf("first pass deleted %d rows, want the batch size of 2", deleted["outbox_sent"])
	}

	// Repeating drains the rest: 2, then the last 1.
	total := deleted["outbox_sent"]
	for range 5 {
		d, err := job.RunOnce(ctx)
		if err != nil {
			t.Fatalf("RunOnce() = %v", err)
		}
		total += d["outbox_sent"]
		if d["outbox_sent"] == 0 {
			break
		}
	}
	if total != 5 {
		t.Errorf("drained %d rows in total, want 5", total)
	}
}

// TestRunOnceDropsOutboxRowsPastTheirExpiry checks an unsent row goes as soon
// as its payload's expires_at passes, while a row without one, or with one
// that does not parse, waits for the retention window.
func TestRunOnceDropsOutboxRowsPastTheirExpiry(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := context.Background()
	job := New(db, testkit.NewClock(now), nil)

	for label, payload := range map[string]string{
		"expired":   `{"expires_at": "` + now.Add(-time.Minute).Format(time.RFC3339Nano) + `"}`,
		"live":      `{"expires_at": "` + now.Add(time.Minute).Format(time.RFC3339Nano) + `"}`,
		"no-expiry": `{}`,
		"malformed": `{"expires_at": "soon"}`,
	} {
		if _, err := db.Leader().ExecContext(ctx,
			`INSERT INTO outbox (topic, key, payload, created_at) VALUES ('note.created', $1, $2, $3)`,
			label, payload, now.Add(-time.Hour)); err != nil {
			t.Fatalf("insert outbox %s: %v", label, err)
		}
	}

	deleted, err := job.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce() = %v", err)
	}
	if deleted["outbox_unsendable"] != 1 {
		t.Errorf("deleted[outbox_unsendable] = %d, want 1", deleted["outbox_unsendable"])
	}
	assertKeys(ctx, t, db, `SELECT key FROM outbox`, []string{"live", "no-expiry", "malformed"})
}

func ptr(t time.Time) *time.Time { return &t }

func insertOutbox(
	ctx context.Context, t *testing.T, db *sqlkit.DB, label string, created time.Time, sent *time.Time,
) {
	t.Helper()
	_, err := db.Leader().ExecContext(ctx,
		`INSERT INTO outbox (topic, key, payload, created_at, sent_at)
		 VALUES ('note.created', $1, '{}', $2, $3)`, label, created, sent)
	if err != nil {
		t.Fatalf("insert outbox %s: %v", label, err)
	}
}

// assertKeys checks the surviving rows are exactly want, in any order.
func assertKeys(ctx context.Context, t *testing.T, db *sqlkit.DB, query string, want []string) {
	t.Helper()
	rows, err := db.Leader().QueryContext(ctx, query)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string]bool{}
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[label] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(got) != len(want) {
		t.Errorf("%q left %v, want exactly %v", query, keys(got), want)
		return
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("%q removed %q, which is inside its retention window", query, w)
		}
	}
}

// keys lists a set, for failure messages.
func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}
