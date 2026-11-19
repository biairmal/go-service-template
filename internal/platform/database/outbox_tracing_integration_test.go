package database_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/biairmal/go-sdk/lib/ctxkit"
	"github.com/biairmal/go-sdk/lib/tracer"

	"github.com/biairmal/go-service-template/internal/platform/database"
	"github.com/biairmal/go-service-template/internal/platform/testkit"
)

// recTracer records the spans started through it.
type recTracer struct {
	mu    sync.Mutex
	spans []*recSpan
}

type recSpan struct {
	tracer.Span
	name  string
	attrs map[string]any
	err   error
	ended bool
}

func (t *recTracer) Start(ctx context.Context, name string, _ ...tracer.SpanOption) (context.Context, tracer.Span) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := &recSpan{name: name, attrs: map[string]any{}}
	t.spans = append(t.spans, s)
	return ctx, s
}

func (t *recTracer) Shutdown(context.Context) error { return nil }

func (t *recTracer) names() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []string
	for _, s := range t.spans {
		out = append(out, s.name)
	}
	return out
}

func (s *recSpan) End()               { s.ended = true }
func (s *recSpan) SetError(err error) { s.err = err }
func (s *recSpan) SetAttributes(a map[string]any) {
	for k, v := range a {
		s.attrs[k] = v
	}
}

func (s *recSpan) SpanContext() tracer.SpanContext {
	return tracer.SpanContext{TraceID: fmt.Sprintf("trace-%p", s)}
}

func TestDispatchSpans(t *testing.T) {
	db := testkit.Postgres(t)
	ctx := context.Background()
	tr := &recTracer{}
	var traceIDs []string
	sender := database.SenderFunc(func(ctx context.Context, m database.Message) error {
		traceIDs = append(traceIDs, ctxkit.TraceID(ctx))
		if m.Key == "bad" {
			return errors.New("smtp down")
		}
		return nil
	})
	d := database.NewDispatcher(db, map[string]database.Sender{"mail": sender}, nil, tr)

	if n, err := d.DispatchOnce(ctx); err != nil || n != 0 {
		t.Fatalf("idle DispatchOnce = %d, %v", n, err)
	}
	if got := tr.names(); len(got) != 0 {
		t.Fatalf("an idle poll started spans %v, want none", got)
	}

	ob := database.NewOutbox(db)
	for _, key := range []string{"ok", "bad"} {
		if err := ob.Enqueue(ctx, "mail", key, map[string]string{"k": key}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := d.DispatchOnce(ctx); err != nil || n != 2 {
		t.Fatalf("DispatchOnce = %d, %v; want 2", n, err)
	}

	want := []string{"outbox.dispatch", "outbox.send mail", "outbox.send mail"}
	if got := tr.names(); !slices.Equal(got, want) {
		t.Fatalf("spans = %v, want %v", got, want)
	}
	dispatch, ok, bad := tr.spans[0], tr.spans[1], tr.spans[2]
	if dispatch.attrs["outbox.claimed"] != 2 || dispatch.err != nil {
		t.Errorf("dispatch span attrs %v err %v", dispatch.attrs, dispatch.err)
	}
	if ok.err != nil || bad.err == nil {
		t.Errorf("send errors: ok=%v bad=%v; want only the failed send marked", ok.err, bad.err)
	}
	if bad.attrs["outbox.topic"] != "mail" || bad.attrs["outbox.id"] == nil {
		t.Errorf("send span attrs = %v", bad.attrs)
	}
	for _, s := range tr.spans {
		if !s.ended {
			t.Errorf("span %s not ended", s.name)
		}
	}
	wantID := dispatch.SpanContext().TraceID
	for _, id := range traceIDs {
		if id != wantID {
			t.Errorf("sender saw trace_id %q, want the dispatch span's %q", id, wantID)
		}
	}
}
