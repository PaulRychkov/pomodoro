package relay

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/PaulRychkov/pomodoro/internal/models"
	"github.com/PaulRychkov/pomodoro/internal/store/memstore"
)

type sentMessage struct {
	topic string
	key   string
	value []byte
}

type fakePublisher struct {
	sent    []sentMessage
	failOn  int
	calls   int
	closed  bool
	failErr error
}

func (f *fakePublisher) Publish(topic, key string, value []byte) error {
	f.calls++
	if f.failOn > 0 && f.calls >= f.failOn {
		return f.failErr
	}
	f.sent = append(f.sent, sentMessage{topic: topic, key: key, value: value})
	return nil
}

func (f *fakePublisher) Close() error {
	f.closed = true
	return nil
}

func seedEvent(t *testing.T, mem *memstore.Mem, eventType string, createdAt time.Time) models.OutboxEvent {
	t.Helper()
	e := models.OutboxEvent{
		ID:            uuid.New(),
		EventType:     eventType,
		AggregateType: "session",
		AggregateID:   uuid.New(),
		Payload:       models.JSONMap{"session_id": uuid.NewString(), "kind": "focus"},
		CreatedAt:     createdAt,
	}
	if err := mem.Outbox().Insert(context.Background(), &e); err != nil {
		t.Fatalf("seed outbox: %v", err)
	}
	return e
}

func TestFlushPublishesInOrderAndMarks(t *testing.T) {
	mem := memstore.New()
	base := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	first := seedEvent(t, mem, "pomodoro.started", base)
	second := seedEvent(t, mem, "pomodoro.completed", base.Add(time.Minute))

	pub := &fakePublisher{}
	connects := 0
	r := New(mem, "pomodoro.events", func() (Publisher, error) {
		connects++
		return pub, nil
	}, zap.NewNop())
	r.now = func() time.Time { return base.Add(2 * time.Minute) }

	if err := r.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if connects != 1 {
		t.Fatalf("connects = %d, want 1", connects)
	}
	if len(pub.sent) != 2 {
		t.Fatalf("sent = %d, want 2", len(pub.sent))
	}
	if pub.sent[0].topic != "pomodoro.events" || pub.sent[0].key != first.AggregateID.String() {
		t.Fatalf("first message topic/key = %s/%s", pub.sent[0].topic, pub.sent[0].key)
	}

	var envelope map[string]any
	if err := json.Unmarshal(pub.sent[0].value, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	checks := map[string]string{
		"specversion":     "1.0",
		"id":              first.ID.String(),
		"source":          "pomodoro",
		"type":            "pomodoro.started",
		"subject":         first.AggregateID.String(),
		"time":            base.Format(time.RFC3339),
		"datacontenttype": "application/json",
	}
	for k, want := range checks {
		if envelope[k] != want {
			t.Errorf("envelope[%s] = %v, want %s", k, envelope[k], want)
		}
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok || data["kind"] != "focus" {
		t.Fatalf("envelope data = %v", envelope["data"])
	}

	var secondEnvelope map[string]any
	if err := json.Unmarshal(pub.sent[1].value, &secondEnvelope); err != nil {
		t.Fatalf("unmarshal second: %v", err)
	}
	if secondEnvelope["type"] != "pomodoro.completed" {
		t.Fatalf("second type = %v", secondEnvelope["type"])
	}

	unpublished, err := mem.Outbox().Unpublished(context.Background(), 10)
	if err != nil {
		t.Fatalf("unpublished: %v", err)
	}
	if len(unpublished) != 0 {
		t.Fatalf("unpublished = %d, want 0", len(unpublished))
	}
	for _, e := range mem.OutboxEvents() {
		if e.PublishedAt == nil {
			t.Fatalf("event %s (%s) not marked published", e.ID, e.EventType)
		}
	}
	_ = second
}

func TestFlushStopsOnPublishErrorAndMarksFailed(t *testing.T) {
	mem := memstore.New()
	base := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	seedEvent(t, mem, "pomodoro.started", base)
	seedEvent(t, mem, "pomodoro.completed", base.Add(time.Minute))

	pub := &fakePublisher{failOn: 1, failErr: errors.New("broker down")}
	connects := 0
	r := New(mem, "pomodoro.events", func() (Publisher, error) {
		connects++
		return pub, nil
	}, zap.NewNop())

	if err := r.Flush(context.Background()); err == nil {
		t.Fatal("flush must return error")
	}
	if !pub.closed {
		t.Fatal("failed publisher must be closed for reconnect")
	}

	events := mem.OutboxEvents()
	if events[0].Attempts != 1 || events[0].LastError == nil {
		t.Fatalf("first event attempts=%d lastError=%v", events[0].Attempts, events[0].LastError)
	}
	if events[0].PublishedAt != nil || events[1].PublishedAt != nil {
		t.Fatal("no event may be marked published after failure")
	}

	pub.failOn = 0
	if err := r.Flush(context.Background()); err != nil {
		t.Fatalf("retry flush: %v", err)
	}
	if connects != 2 {
		t.Fatalf("connects = %d, want reconnect", connects)
	}
	if len(pub.sent) != 2 {
		t.Fatalf("sent after retry = %d, want 2", len(pub.sent))
	}
}

func TestFlushWithoutEventsDoesNotConnect(t *testing.T) {
	mem := memstore.New()
	connects := 0
	r := New(mem, "pomodoro.events", func() (Publisher, error) {
		connects++
		return &fakePublisher{}, nil
	}, zap.NewNop())
	if err := r.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if connects != 0 {
		t.Fatalf("connects = %d, want 0", connects)
	}
}

func TestFlushConnectErrorKeepsEvents(t *testing.T) {
	mem := memstore.New()
	seedEvent(t, mem, "pomodoro.started", time.Now())
	r := New(mem, "pomodoro.events", func() (Publisher, error) {
		return nil, errors.New("kafka unreachable")
	}, zap.NewNop())
	if err := r.Flush(context.Background()); err == nil {
		t.Fatal("flush must return connect error")
	}
	unpublished, _ := mem.Outbox().Unpublished(context.Background(), 10)
	if len(unpublished) != 1 {
		t.Fatalf("unpublished = %d, want 1", len(unpublished))
	}
}
