package cleanup

import (
	"context"
	"testing"
	"time"

	"github.com/biairmal/go-sdk/lib/tracer"

	"github.com/biairmal/go-service-template/internal/platform/testkit"
)

// oneSpanTracer records the single span a drain starts.
type oneSpanTracer struct {
	tracer.Tracer
	span *recSpan
}

type recSpan struct {
	tracer.Span
	name  string
	attrs map[string]any
	ended bool
}

func (t *oneSpanTracer) Start(ctx context.Context, name string, _ ...tracer.SpanOption) (context.Context, tracer.Span) {
	t.span = &recSpan{name: name, attrs: map[string]any{}}
	return ctx, t.span
}

func (s *recSpan) End()                            { s.ended = true }
func (s *recSpan) SetError(error)                  {}
func (s *recSpan) SpanContext() tracer.SpanContext { return tracer.SpanContext{TraceID: "t1"} }
func (s *recSpan) SetAttributes(a map[string]any) {
	for k, v := range a {
		s.attrs[k] = v
	}
}

func TestDrainSpanCountsRows(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := context.Background()
	tr := &oneSpanTracer{}
	job := New(db, testkit.NewClock(now), nil, tr)
	job.batch = 1 // forces a second pass, so the counts add up across passes
	old := now.Add(-sentRetention - time.Hour)
	insertOutbox(ctx, t, db, "a", old, ptr(old))
	insertOutbox(ctx, t, db, "b", old, ptr(old))

	job.drain(ctx)

	s := tr.span
	if s == nil || s.name != "cleanup.run" || !s.ended {
		t.Fatalf("span = %+v, want an ended cleanup.run", s)
	}
	if got := s.attrs["cleanup.deleted.outbox_sent"]; got != int64(2) {
		t.Errorf("cleanup.deleted.outbox_sent = %v, want 2", got)
	}
}
