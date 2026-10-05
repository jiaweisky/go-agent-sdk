package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jiaweisky/go-agent-sdk/core/database"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

var (
	eventTestMessage = schema.Define(schema.DefineInput{
		Type:   "test.message",
		Schema: map[string]any{"text": nil},
	})
	eventTestSync = schema.Define(schema.DefineInput{
		Type:    "test.sync",
		Durable: &schema.EventDurable{Version: 1, Aggregate: "id"},
		Schema:  map[string]any{"id": nil, "text": nil},
	})
	eventTestSent = schema.Define(schema.DefineInput{
		Type:    "test.sent",
		Durable: &schema.EventDurable{Version: 1, Aggregate: "messageID"},
		Schema:  map[string]any{"messageID": nil, "text": nil},
	})
	eventTestVersioned = schema.Define(schema.DefineInput{
		Type:    "test.versioned",
		Durable: &schema.EventDurable{Version: 2, Aggregate: "id"},
		Schema:  map[string]any{"id": nil, "text": nil},
	})
	eventTestGlobal = schema.Define(schema.DefineInput{
		Type:   "test.global",
		Schema: map[string]any{"text": nil},
	})
	eventTestDurable = schema.Durable([]schema.EventDefinition{
		eventTestSync,
		eventTestSent,
		eventTestVersioned,
	})
)

func eventTestDB(t *testing.T) *database.Database {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "event.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestEvent(t *testing.T, cfg EventConfig) Event {
	t.Helper()
	if cfg.DB == nil {
		cfg.DB = eventTestDB(t).DB
	}
	if cfg.Durable == nil {
		cfg.Durable = eventTestDurable
	}
	return NewEvent(cfg)
}

func collectEventN(ctx context.Context, seq func(func(schema.EventPayload, error) bool), n int) []schema.EventPayload {
	var out []schema.EventPayload
	for event, err := range seq {
		if err != nil {
			break
		}
		out = append(out, event)
		if len(out) >= n {
			break
		}
	}
	return out
}

func waitForEventRegistration(t *testing.T, events Event, check func(*eventService) bool) {
	t.Helper()
	svc, ok := events.(*eventService)
	if !ok {
		t.Fatal("unexpected event service type")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		svc.mu.Lock()
		ready := check(svc)
		svc.mu.Unlock()
		if ready {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for event registration")
}

func TestEventPublishWithLocation(t *testing.T) {
	workspace := schema.WorkspaceID("wrk_test")
	events := newTestEvent(t, EventConfig{
		Location: &schema.LocationRef{Directory: schema.AbsolutePath("project"), WorkspaceID: &workspace},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	received := make(chan schema.EventPayload, 8)
	go func() {
		for event, err := range events.Subscribe(ctx, eventTestMessage) {
			if err != nil {
				return
			}
			received <- event
		}
	}()
	waitForEventRegistration(t, events, func(svc *eventService) bool {
		return len(svc.typedSubscribers[eventTestMessage.Type]) >= 1
	})

	published, err := events.Publish(eventTestMessage, map[string]any{"text": "hello"}, nil)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	var got schema.EventPayload
	select {
	case got = <-received:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
	if got.ID != published.ID || got.Type != "test.message" {
		t.Fatalf("unexpected event: %+v", got)
	}
	if got.Durable != nil {
		t.Fatalf("live event should not carry durable metadata")
	}
	if got.Location == nil || got.Location.Directory != "project" || got.Location.WorkspaceID == nil || *got.Location.WorkspaceID != workspace {
		t.Fatalf("unexpected location: %+v", got.Location)
	}
}

func TestEventPublishOmitsLocation(t *testing.T) {
	events := newTestEvent(t, EventConfig{})
	event, err := events.Publish(eventTestGlobal, map[string]any{"text": "hello"}, nil)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if event.Location != nil {
		t.Fatalf("expected no location, got %+v", event.Location)
	}
}

func TestEventPublishDefinitionVersion(t *testing.T) {
	events := newTestEvent(t, EventConfig{})
	event, err := events.Publish(eventTestVersioned, map[string]any{"id": "one", "text": "hello"}, nil)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if event.Type != "test.versioned" {
		t.Fatalf("unexpected type: %s", event.Type)
	}
	if event.Durable == nil || event.Durable.Version != 2 {
		t.Fatalf("unexpected durable metadata: %+v", event.Durable)
	}
}

func TestEventLatestOutOfOrder(t *testing.T) {
	latest := schema.Define(schema.DefineInput{
		Type:    "test.out-of-order",
		Durable: &schema.EventDurable{Version: 2, Aggregate: "id"},
		Schema:  map[string]any{"id": nil},
	})
	historical := schema.Define(schema.DefineInput{
		Type:    "test.out-of-order",
		Durable: &schema.EventDurable{Version: 1, Aggregate: "id"},
		Schema:  map[string]any{"id": nil},
	})
	if got := schema.Latest([]schema.EventDefinition{latest, historical})["test.out-of-order"]; got.Durable.Version != 2 {
		t.Fatalf("expected version 2, got %+v", got.Durable)
	}
	if got := schema.Latest([]schema.EventDefinition{historical, latest})["test.out-of-order"]; got.Durable.Version != 2 {
		t.Fatalf("expected version 2, got %+v", got.Durable)
	}
}

func TestEventTypedAndWildcardSubscriptions(t *testing.T) {
	events := newTestEvent(t, EventConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	typed := make(chan schema.EventPayload, 8)
	all := make(chan schema.EventPayload, 8)
	go func() {
		for event, err := range events.Subscribe(ctx, eventTestMessage) {
			if err != nil {
				return
			}
			typed <- event
		}
	}()
	go func() {
		for event, err := range events.All(ctx) {
			if err != nil {
				return
			}
			all <- event
		}
	}()
	waitForEventRegistration(t, events, func(svc *eventService) bool {
		return len(svc.typedSubscribers[eventTestMessage.Type]) >= 1 && len(svc.allSubscribers) >= 1
	})

	published, err := events.Publish(eventTestMessage, map[string]any{"text": "hello"}, nil)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	for name, ch := range map[string]chan schema.EventPayload{"typed": typed, "all": all} {
		select {
		case got := <-ch:
			if got.ID != published.ID {
				t.Fatalf("%s subscriber got %s, want %s", name, got.ID, published.ID)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %s event", name)
		}
	}
}

func TestEventProjectorsInline(t *testing.T) {
	events := newTestEvent(t, EventConfig{})
	var received []schema.EventPayload
	if err := events.Project(eventTestSync, func(event schema.EventPayload, _ EventDB) error {
		received = append(received, event)
		return nil
	}); err != nil {
		t.Fatalf("project: %v", err)
	}

	first, err := events.Publish(eventTestSync, map[string]any{"id": "one", "text": "hello"}, nil)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := events.Publish(eventTestSync, map[string]any{"id": "one", "text": "after"}, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if len(received) != 2 || received[0].ID != first.ID {
		t.Fatalf("unexpected projectors: %+v", received)
	}
}

func TestEventCommitInsideTransaction(t *testing.T) {
	events := newTestEvent(t, EventConfig{})
	var received []string
	if err := events.Project(eventTestSync, func(_ schema.EventPayload, _ EventDB) error {
		received = append(received, "projector")
		return nil
	}); err != nil {
		t.Fatalf("project: %v", err)
	}

	if _, err := events.Publish(eventTestSync, map[string]any{"id": "agg", "text": "hello"}, &EventPublishOptions{
		Commit: func(_ EventDB, seq int) error {
			received = append(received, "commit")
			return nil
		},
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if len(received) != 2 || received[0] != "projector" || received[1] != "commit" {
		t.Fatalf("unexpected order: %v", received)
	}
}

func TestEventRollbackOnCommitFailure(t *testing.T) {
	db := eventTestDB(t)
	events := newTestEvent(t, EventConfig{DB: db.DB})

	if _, err := db.DB.Exec("CREATE TABLE IF NOT EXISTS event_commit_probe (value text NOT NULL)"); err != nil {
		t.Fatalf("create probe: %v", err)
	}
	if _, err := db.DB.Exec("DELETE FROM event_commit_probe"); err != nil {
		t.Fatalf("clear probe: %v", err)
	}
	if err := events.Project(eventTestSync, func(_ schema.EventPayload, tx EventDB) error {
		_, err := tx.Exec("INSERT INTO event_commit_probe (value) VALUES ('projected')")
		return err
	}); err != nil {
		t.Fatalf("project: %v", err)
	}

	_, err := events.Publish(eventTestSync, map[string]any{"id": "agg", "text": "hello"}, &EventPublishOptions{
		Commit: func(_ EventDB, seq int) error { return errors.New("commit failed") },
	})
	if err == nil || err.Error() != "commit failed" {
		t.Fatalf("expected commit failure, got %v", err)
	}

	probe := eventQueryRows(t, db.DB, "SELECT value FROM event_commit_probe")
	if len(probe) != 0 {
		t.Fatalf("expected probe rolled back, got %v", probe)
	}
	if rows := eventQueryRows(t, db.DB, "SELECT seq FROM event WHERE aggregate_id = 'agg'"); len(rows) != 0 {
		t.Fatalf("expected event rolled back, got %v", rows)
	}
	if rows := eventQueryRows(t, db.DB, "SELECT seq FROM event_sequence WHERE aggregate_id = 'agg'"); len(rows) != 0 {
		t.Fatalf("expected sequence rolled back, got %v", rows)
	}
}

func TestEventRejectsCommitOnLiveOnly(t *testing.T) {
	events := newTestEvent(t, EventConfig{})
	_, err := events.Publish(eventTestMessage, map[string]any{"text": "hello"}, &EventPublishOptions{
		Commit: func(_ EventDB, seq int) error { return nil },
	})
	if err == nil || err.Error() != "Local commit hooks require a durable event" {
		t.Fatalf("unexpected error: %v", err)
	}
	var invalid *EventInvalidDurableEventError
	if !errors.As(err, &invalid) || invalid.Type != eventTestMessage.Type {
		t.Fatalf("expected invalid durable event error, got %#v", err)
	}
}

func TestEventListenersAfterProjectors(t *testing.T) {
	events := newTestEvent(t, EventConfig{})
	var received []string
	if err := events.Project(eventTestSync, func(_ schema.EventPayload, _ EventDB) error {
		received = append(received, "projector")
		return nil
	}); err != nil {
		t.Fatalf("project: %v", err)
	}
	unsubscribe, err := events.Listen(func(_ schema.EventPayload) error {
		received = append(received, "listener")
		return nil
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	if _, err := events.Publish(eventTestSync, map[string]any{"id": "one", "text": "hello"}, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}
	unsubscribe()
	if _, err := events.Publish(eventTestSync, map[string]any{"id": "one", "text": "after"}, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	want := []string{"projector", "listener", "projector"}
	if len(received) != len(want) {
		t.Fatalf("unexpected listeners: %v", received)
	}
	for i := range want {
		if received[i] != want[i] {
			t.Fatalf("unexpected order: %v", received)
		}
	}
}

func TestEventIsolatesObserverDefects(t *testing.T) {
	events := newTestEvent(t, EventConfig{})
	var received []string
	if _, err := events.Listen(func(_ schema.EventPayload) error {
		return errors.New("listener defect")
	}); err != nil {
		t.Fatalf("listen: %v", err)
	}
	if _, err := events.Listen(func(event schema.EventPayload) error {
		received = append(received, event.Type)
		return nil
	}); err != nil {
		t.Fatalf("listen: %v", err)
	}

	event, err := events.Publish(eventTestSync, map[string]any{"id": "one", "text": "hello"}, nil)
	if err != nil {
		t.Fatalf("durable publish should isolate defects: %v", err)
	}
	if event.Durable == nil {
		t.Fatalf("expected durable metadata")
	}
	if len(received) != 1 || received[0] != "test.sync" {
		t.Fatalf("unexpected listeners: %v", received)
	}

	if _, err := events.Publish(eventTestMessage, map[string]any{"text": "hello"}, nil); err == nil {
		t.Fatal("live-only listener defect should fail fast")
	}
}

func TestEventDurableInsertAndSeq(t *testing.T) {
	db := eventTestDB(t)
	events := newTestEvent(t, EventConfig{DB: db.DB})

	for _, text := range []string{"first", "second"} {
		if _, err := events.Publish(eventTestSync, map[string]any{"id": "agg", "text": text}, nil); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	rows := eventQueryRows(t, db.DB, "SELECT seq, type FROM event WHERE aggregate_id = 'agg' ORDER BY seq ASC")
	if len(rows) != 2 || rows[0][0] != "0" || rows[1][0] != "1" {
		t.Fatalf("unexpected rows: %v", rows)
	}
	if rows[0][1] != "test.sync.1" {
		t.Fatalf("unexpected versioned type: %s", rows[0][1])
	}
}

func TestEventUsesCustomAggregateField(t *testing.T) {
	db := eventTestDB(t)
	events := newTestEvent(t, EventConfig{DB: db.DB})
	if _, err := events.Publish(eventTestSent, map[string]any{"messageID": "msg_1", "text": "sent"}, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}
	rows := eventQueryRows(t, db.DB, "SELECT aggregate_id FROM event")
	if len(rows) != 1 || rows[0][0] != "msg_1" {
		t.Fatalf("unexpected rows: %v", rows)
	}
}

func TestEventDurableTailDuringHandoff(t *testing.T) {
	db := eventTestDB(t)
	aggregateID := "ses_handoff"
	var once sync.Once
	readStarted := make(chan struct{})
	resume := make(chan struct{})
	events := newTestEvent(t, EventConfig{
		DB: db.DB,
		BeforeAggregateRead: func(string) error {
			once.Do(func() {
				close(readStarted)
				<-resume
			})
			return nil
		},
	})

	if _, err := events.Publish(eventTestSync, map[string]any{"id": aggregateID, "text": "zero"}, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan []schema.EventPayload, 1)
	go func() {
		got <- collectEventN(ctx, events.Durable(ctx, aggregateID), 2)
	}()

	select {
	case <-readStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for read")
	}
	if _, err := events.Publish(eventTestSync, map[string]any{"id": aggregateID, "text": "one"}, nil); err != nil {
		t.Fatalf("publish during handoff: %v", err)
	}
	close(resume)

	select {
	case result := <-got:
		if len(result) != 2 {
			t.Fatalf("expected 2 events, got %d", len(result))
		}
		if result[0].Durable.Seq != 0 || result[1].Durable.Seq != 1 {
			t.Fatalf("unexpected seqs: %d, %d", result[0].Durable.Seq, result[1].Durable.Seq)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for durable events")
	}
}

func TestEventReplayValidation(t *testing.T) {
	events := newTestEvent(t, EventConfig{})

	if err := events.Replay(SerializedEvent{
		ID:          schema.NewEventID(),
		Type:        schema.VersionedType(eventTestSync.Type, 1),
		Seq:         0,
		AggregateID: "agg",
		Data:        map[string]any{"id": "agg", "text": "replayed"},
	}, nil); err != nil {
		t.Fatalf("replay: %v", err)
	}

	if err := events.Replay(SerializedEvent{Type: "unknown.event.1", Data: map[string]any{}}, nil); err == nil {
		t.Fatal("expected unknown event type error")
	}

	err := events.Replay(SerializedEvent{
		ID:          schema.NewEventID(),
		Type:        schema.VersionedType(eventTestSync.Type, 1),
		Seq:         1,
		AggregateID: "envelope",
		Data:        map[string]any{"id": "payload", "text": "x"},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "Aggregate mismatch") {
		t.Fatalf("expected aggregate mismatch, got %v", err)
	}

	err = events.Replay(SerializedEvent{
		ID:          schema.NewEventID(),
		Type:        schema.VersionedType(eventTestSync.Type, 1),
		Seq:         5,
		AggregateID: "agg",
		Data:        map[string]any{"id": "agg", "text": "bad"},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "Sequence mismatch") {
		t.Fatalf("expected sequence mismatch, got %v", err)
	}
}

func TestEventReplayAllContiguous(t *testing.T) {
	events := newTestEvent(t, EventConfig{})
	source, err := events.ReplayAll([]SerializedEvent{
		{ID: schema.NewEventID(), Type: schema.VersionedType(eventTestSync.Type, 1), Seq: 0, AggregateID: "agg", Data: map[string]any{"id": "agg", "text": "one"}},
		{ID: schema.NewEventID(), Type: schema.VersionedType(eventTestSync.Type, 1), Seq: 1, AggregateID: "agg", Data: map[string]any{"id": "agg", "text": "two"}},
	}, nil)
	if err != nil || source != "agg" {
		t.Fatalf("replayAll: source=%q err=%v", source, err)
	}

	_, err = events.ReplayAll([]SerializedEvent{
		{ID: schema.NewEventID(), Type: schema.VersionedType(eventTestSync.Type, 1), Seq: 0, AggregateID: "agg", Data: map[string]any{"id": "agg"}},
		{ID: schema.NewEventID(), Type: schema.VersionedType(eventTestSync.Type, 1), Seq: 2, AggregateID: "agg", Data: map[string]any{"id": "agg"}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "sequence mismatch") {
		t.Fatalf("expected sequence mismatch, got %v", err)
	}
}

func TestEventOwnerFencing(t *testing.T) {
	events := newTestEvent(t, EventConfig{})
	if _, err := events.Publish(eventTestSync, map[string]any{"id": "agg", "text": "seed"}, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := events.Claim("agg", "owner-a"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	var received []schema.EventPayload
	if err := events.Project(eventTestSync, func(event schema.EventPayload, _ EventDB) error {
		received = append(received, event)
		return nil
	}); err != nil {
		t.Fatalf("project: %v", err)
	}

	if err := events.Replay(SerializedEvent{
		ID:          schema.NewEventID(),
		Type:        schema.VersionedType(eventTestSync.Type, 1),
		Seq:         1,
		AggregateID: "agg",
		Data:        map[string]any{"id": "agg", "text": "ignored"},
	}, &EventReplayOptions{OwnerID: "owner-b"}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(received) != 0 {
		t.Fatalf("expected fenced replay to be skipped, got %d projectors", len(received))
	}

	err := events.Replay(SerializedEvent{
		ID:          schema.NewEventID(),
		Type:        schema.VersionedType(eventTestSync.Type, 1),
		Seq:         1,
		AggregateID: "agg",
		Data:        map[string]any{"id": "agg", "text": "conflict"},
	}, &EventReplayOptions{OwnerID: "owner-b", StrictOwner: true})
	if err == nil || !strings.Contains(err.Error(), "Replay owner mismatch") {
		t.Fatalf("expected owner mismatch, got %v", err)
	}
}

func TestEventRemoveAndClaim(t *testing.T) {
	db := eventTestDB(t)
	events := newTestEvent(t, EventConfig{DB: db.DB})
	if _, err := events.Publish(eventTestSync, map[string]any{"id": "agg", "text": "seed"}, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := events.Claim("agg", "owner-1"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := events.Claim("agg", "owner-2"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	rows := eventQueryRows(t, db.DB, "SELECT seq, owner_id FROM event_sequence WHERE aggregate_id = 'agg'")
	if len(rows) != 1 || rows[0][0] != "0" || rows[0][1] != "owner-2" {
		t.Fatalf("unexpected sequence: %v", rows)
	}

	if err := events.Remove("agg"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if rows := eventQueryRows(t, db.DB, "SELECT seq FROM event WHERE aggregate_id = 'agg'"); len(rows) != 0 {
		t.Fatalf("expected events removed, got %v", rows)
	}
	if rows := eventQueryRows(t, db.DB, "SELECT seq FROM event_sequence WHERE aggregate_id = 'agg'"); len(rows) != 0 {
		t.Fatalf("expected sequence removed, got %v", rows)
	}

	var received []schema.EventPayload
	if err := events.Project(eventTestSync, func(event schema.EventPayload, _ EventDB) error {
		received = append(received, event)
		return nil
	}); err != nil {
		t.Fatalf("project: %v", err)
	}
	if err := events.Replay(SerializedEvent{
		ID:          schema.NewEventID(),
		Type:        schema.VersionedType(eventTestSync.Type, 1),
		Seq:         0,
		AggregateID: "agg",
		Data:        map[string]any{"id": "agg", "text": "replayed"},
	}, nil); err != nil {
		t.Fatalf("replay after remove: %v", err)
	}
	if len(received) != 1 {
		t.Fatalf("expected projector after remove, got %d", len(received))
	}
}

func TestEventQueueOverflow(t *testing.T) {
	q := newEventQueue(1)
	q.offer(schema.EventPayload{ID: "e1"})
	q.offer(schema.EventPayload{ID: "e2"})
	q.offer(schema.EventPayload{ID: "e3"})

	ctx := context.Background()
	_, _, err := q.next(ctx)
	var overflow *EventSubscriberOverflowError
	if !errors.As(err, &overflow) {
		t.Fatalf("expected overflow error, got %v", err)
	}
}

func TestEventAllBoundedDelivers(t *testing.T) {
	events := newTestEvent(t, EventConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan []schema.EventPayload, 1)
	go func() {
		got <- collectEventN(ctx, events.AllBounded(ctx, 8), 2)
	}()
	waitForEventRegistration(t, events, func(svc *eventService) bool {
		return len(svc.listenersSnapshot()) >= 1
	})

	if _, err := events.Publish(eventTestMessage, map[string]any{"text": "one"}, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := events.Publish(eventTestMessage, map[string]any{"text": "two"}, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case result := <-got:
		if len(result) != 2 {
			t.Fatalf("expected 2 events, got %d", len(result))
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bounded stream")
	}
}

func TestEventRollbackOnProjectorPanic(t *testing.T) {
	db := eventTestDB(t)
	events := newTestEvent(t, EventConfig{DB: db.DB})
	if err := events.Project(eventTestSync, func(_ schema.EventPayload, _ EventDB) error {
		panic("projector boom")
	}); err != nil {
		t.Fatalf("project: %v", err)
	}

	func() {
		defer func() { _ = recover() }()
		_, _ = events.Publish(eventTestSync, map[string]any{"id": "agg", "text": "x"}, nil)
	}()

	if stats := db.DB.Stats(); stats.InUse != 0 {
		t.Fatalf("connection leaked after projector panic: in use %d", stats.InUse)
	}
	if rows := eventQueryRows(t, db.DB, "SELECT seq FROM event WHERE aggregate_id = 'agg'"); len(rows) != 0 {
		t.Fatalf("expected rolled back event, got %v", rows)
	}
}

func TestEventSubscriptionStopsOnContextCancel(t *testing.T) {
	events := newTestEvent(t, EventConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{}, 1)
	go func() {
		for range events.All(ctx) {
		}
		done <- struct{}{}
	}()
	waitForEventRegistration(t, events, func(svc *eventService) bool {
		return len(svc.allSubscribers) >= 1
	})

	for i := 0; i < 3; i++ {
		if _, err := events.Publish(eventTestMessage, map[string]any{"text": "x"}, nil); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("subscription did not stop on context cancel")
	}
}

func TestEventLatestSequence(t *testing.T) {
	db := eventTestDB(t)
	events := newTestEvent(t, EventConfig{DB: db.DB})

	if seq, err := EventLatestSequence(db.DB, "agg"); err != nil || seq != -1 {
		t.Fatalf("expected -1 for unknown aggregate, got %d err=%v", seq, err)
	}
	for _, text := range []string{"one", "two"} {
		if _, err := events.Publish(eventTestSync, map[string]any{"id": "agg", "text": text}, nil); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	if seq, err := EventLatestSequence(db.DB, "agg"); err != nil || seq != 1 {
		t.Fatalf("expected seq 1, got %d err=%v", seq, err)
	}
}

func TestEventReadAggregate(t *testing.T) {
	db := eventTestDB(t)
	events := newTestEvent(t, EventConfig{DB: db.DB})
	for _, text := range []string{"one", "two", "three"} {
		if _, err := events.Publish(eventTestSync, map[string]any{"id": "agg", "text": text}, nil); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	manifest := EventReadAggregateManifest{
		Definitions: map[string]schema.EventDefinition{
			schema.VersionedType(eventTestSync.Type, 1): eventTestSync,
		},
	}

	page, err := EventReadAggregate(db.DB, EventReadAggregateInput{AggregateID: "agg", Limit: 2, Manifest: manifest})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(page.Events) != 2 || !page.HasMore {
		t.Fatalf("unexpected first page: %d events, hasMore=%v", len(page.Events), page.HasMore)
	}
	if page.Events[0].Type != eventTestSync.Type || page.Events[0].Durable == nil || page.Events[0].Durable.Seq != 0 {
		t.Fatalf("unexpected first event: %+v", page.Events[0])
	}

	after := 1
	rest, err := EventReadAggregate(db.DB, EventReadAggregateInput{AggregateID: "agg", After: &after, Limit: 2, Manifest: manifest})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(rest.Events) != 1 || rest.HasMore || rest.Events[0].Durable.Seq != 2 {
		t.Fatalf("unexpected second page: %+v", rest)
	}

	empty, err := EventReadAggregate(db.DB, EventReadAggregateInput{AggregateID: "agg", Limit: 2})
	if err != nil || len(empty.Events) != 0 {
		t.Fatalf("expected empty result for empty manifest, got %+v err=%v", empty, err)
	}
}

func TestEventInterruptErrorPropagates(t *testing.T) {
	events := newTestEvent(t, EventConfig{})
	if _, err := events.Listen(func(_ schema.EventPayload) error {
		return &EventInterruptError{}
	}); err != nil {
		t.Fatalf("listen: %v", err)
	}

	_, err := events.Publish(eventTestSync, map[string]any{"id": "agg", "text": "hello"}, nil)
	if err == nil {
		t.Fatal("expected interrupt to propagate through durable publish")
	}
	var interrupt *EventInterruptError
	if !errors.As(err, &interrupt) {
		t.Fatalf("expected interrupt error, got %v", err)
	}
}

func TestNewEventRequiresDB(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected NewEvent to panic without a database")
		}
	}()
	NewEvent(EventConfig{})
}

func eventQueryRows(t *testing.T, db *sql.DB, query string) [][]string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	var result [][]string
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		row := make([]string, len(columns))
		for i, value := range values {
			row[i] = stringify(value)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return result
}

func stringify(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return fmt.Sprintf("%v", v)
	}
}
