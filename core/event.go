package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"strings"
	"sync"

	"github.com/user/go-agent-sdk/schema"
)

type (
	EventID             = schema.EventID
	EventDefinition     = schema.EventDefinition
	EventDurable        = schema.EventDurable
	EventPayload        = schema.EventPayload
	EventPayloadDurable = schema.EventPayloadDurable
)

func NewEventID() EventID {
	return schema.NewEventID()
}

func EventDefine(input schema.DefineInput) schema.EventDefinition {
	return schema.Define(input)
}

func EventVersionedType(eventType string, version int) string {
	return schema.VersionedType(eventType, version)
}

type EventSubscriberOverflowError struct {
	Capacity int
}

func (e *EventSubscriberOverflowError) Error() string {
	return fmt.Sprintf("event subscriber overflow: capacity %d", e.Capacity)
}

type EventInvalidDurableEventError struct {
	Type    string
	Message string
}

func (e *EventInvalidDurableEventError) Error() string {
	return e.Message
}

type EventInterruptError struct {
	Cause error
}

func (e *EventInterruptError) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "event listener interrupted"
}

func (e *EventInterruptError) Unwrap() error {
	return e.Cause
}

type EventDB interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

type EventProjector func(event EventPayload, tx EventDB) error

type EventListener func(event EventPayload) error

type EventPublishOptions struct {
	ID       *EventID
	Metadata map[string]any
	Location *schema.LocationRef
	// Commit runs inside the durable event transaction. It receives the
	// transaction so local operational projections commit atomically with the
	// event and are not replayed.
	Commit func(tx EventDB, seq int) error
}

type EventReplayOptions struct {
	Publish     bool
	OwnerID     string
	StrictOwner bool
}

type SerializedEvent struct {
	ID          EventID
	Type        string
	Seq         int
	AggregateID string
	Data        map[string]any
}

type EventConfig struct {
	DB                  *sql.DB
	Location            *schema.LocationRef
	Durable             map[string]schema.EventDefinition
	BeforeAggregateRead func(aggregateID string) error
}

type Event interface {
	Publish(def schema.EventDefinition, data any, options *EventPublishOptions) (schema.EventPayload, error)
	Subscribe(ctx context.Context, def schema.EventDefinition) iter.Seq2[schema.EventPayload, error]
	All(ctx context.Context) iter.Seq2[schema.EventPayload, error]
	AllBounded(ctx context.Context, capacity int) iter.Seq2[schema.EventPayload, error]
	Durable(ctx context.Context, aggregateID string, after ...int) iter.Seq2[schema.EventPayload, error]
	Listen(listener EventListener) (func(), error)
	Project(def schema.EventDefinition, projector EventProjector) error
	Replay(event SerializedEvent, options *EventReplayOptions) error
	ReplayAll(events []SerializedEvent, options *EventReplayOptions) (string, error)
	Remove(aggregateID string) error
	Claim(aggregateID, ownerID string) error
}

type eventQueue struct {
	mu       sync.Mutex
	items    []schema.EventPayload
	capacity int
	overflow error
	closed   bool
	wake     chan struct{}
}

func newEventQueue(capacity int) *eventQueue {
	return &eventQueue{capacity: capacity, wake: make(chan struct{}, 1)}
}

func (q *eventQueue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *eventQueue) offer(event schema.EventPayload) {
	q.mu.Lock()
	if q.closed || q.overflow != nil {
		q.mu.Unlock()
		return
	}
	if q.capacity > 0 && len(q.items) >= q.capacity {
		q.overflow = &EventSubscriberOverflowError{Capacity: q.capacity}
		q.items = nil
		q.mu.Unlock()
		q.signal()
		return
	}
	q.items = append(q.items, event)
	q.mu.Unlock()
	q.signal()
}

func (q *eventQueue) next(ctx context.Context) (schema.EventPayload, bool, error) {
	for {
		select {
		case <-ctx.Done():
			return schema.EventPayload{}, false, nil
		default:
		}
		q.mu.Lock()
		if q.overflow != nil {
			err := q.overflow
			q.mu.Unlock()
			return schema.EventPayload{}, false, err
		}
		if len(q.items) > 0 {
			event := q.items[0]
			q.items = q.items[1:]
			q.mu.Unlock()
			return event, true, nil
		}
		if q.closed {
			q.mu.Unlock()
			return schema.EventPayload{}, false, nil
		}
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return schema.EventPayload{}, false, nil
		case <-q.wake:
		}
	}
}

func (q *eventQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.signal()
}

type eventListenerEntry struct {
	id int
	fn EventListener
}

type eventService struct {
	cfg EventConfig

	mu               sync.Mutex
	nextListenerID   int
	listeners        []eventListenerEntry
	projectors       map[string][]EventProjector
	typedSubscribers map[string][]*eventQueue
	allSubscribers   []*eventQueue
	durableWakes     map[string][]chan struct{}
}

func NewEvent(cfg EventConfig) Event {
	if cfg.DB == nil {
		panic("event: EventConfig.DB is required")
	}
	return &eventService{
		cfg:              cfg,
		projectors:       make(map[string][]EventProjector),
		typedSubscribers: make(map[string][]*eventQueue),
		durableWakes:     make(map[string][]chan struct{}),
	}
}

func (s *eventService) Listen(listener EventListener) (func(), error) {
	s.mu.Lock()
	id := s.nextListenerID
	s.nextListenerID++
	s.listeners = append(s.listeners, eventListenerEntry{id: id, fn: listener})
	s.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			for i, entry := range s.listeners {
				if entry.id == id {
					s.listeners = append(s.listeners[:i], s.listeners[i+1:]...)
					break
				}
			}
		})
	}, nil
}

func (s *eventService) Project(def schema.EventDefinition, projector EventProjector) error {
	s.mu.Lock()
	s.projectors[def.Type] = append(s.projectors[def.Type], projector)
	s.mu.Unlock()
	return nil
}

func (s *eventService) projectorsFor(eventType string) []EventProjector {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]EventProjector(nil), s.projectors[eventType]...)
}

func (s *eventService) addTypedSubscriber(eventType string, q *eventQueue) {
	s.mu.Lock()
	s.typedSubscribers[eventType] = append(s.typedSubscribers[eventType], q)
	s.mu.Unlock()
}

func (s *eventService) removeTypedSubscriber(eventType string, q *eventQueue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.typedSubscribers[eventType]
	for i, item := range list {
		if item == q {
			s.typedSubscribers[eventType] = append(list[:i], list[i+1:]...)
			break
		}
	}
}

func (s *eventService) addAllSubscriber(q *eventQueue) {
	s.mu.Lock()
	s.allSubscribers = append(s.allSubscribers, q)
	s.mu.Unlock()
}

func (s *eventService) removeAllSubscriber(q *eventQueue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.allSubscribers {
		if item == q {
			s.allSubscribers = append(s.allSubscribers[:i], s.allSubscribers[i+1:]...)
			break
		}
	}
}

func (s *eventService) addDurableWake(aggregateID string, wake chan struct{}) {
	s.mu.Lock()
	s.durableWakes[aggregateID] = append(s.durableWakes[aggregateID], wake)
	s.mu.Unlock()
}

func (s *eventService) removeDurableWake(aggregateID string, wake chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.durableWakes[aggregateID]
	for i, item := range list {
		if item == wake {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(s.durableWakes, aggregateID)
	} else {
		s.durableWakes[aggregateID] = list
	}
}

func (s *eventService) signalDurableWakes(aggregateID string) {
	s.mu.Lock()
	wakes := append([]chan struct{}(nil), s.durableWakes[aggregateID]...)
	s.mu.Unlock()
	for _, wake := range wakes {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

func (s *eventService) Subscribe(ctx context.Context, def schema.EventDefinition) iter.Seq2[schema.EventPayload, error] {
	return func(yield func(schema.EventPayload, error) bool) {
		q := newEventQueue(0)
		s.addTypedSubscriber(def.Type, q)
		defer func() {
			q.close()
			s.removeTypedSubscriber(def.Type, q)
		}()
		drainQueue(ctx, q, yield)
	}
}

func (s *eventService) All(ctx context.Context) iter.Seq2[schema.EventPayload, error] {
	return func(yield func(schema.EventPayload, error) bool) {
		q := newEventQueue(0)
		s.addAllSubscriber(q)
		defer func() {
			q.close()
			s.removeAllSubscriber(q)
		}()
		drainQueue(ctx, q, yield)
	}
}

// AllBounded streams every published event through a bounded listener queue,
// reporting an overflow error when the consumer falls too far behind.
func (s *eventService) AllBounded(ctx context.Context, capacity int) iter.Seq2[schema.EventPayload, error] {
	return func(yield func(schema.EventPayload, error) bool) {
		q := newEventQueue(capacity)
		unsubscribe, err := s.Listen(func(event schema.EventPayload) error {
			q.offer(event)
			return nil
		})
		if err != nil {
			yield(schema.EventPayload{}, err)
			return
		}
		defer unsubscribe()
		defer q.close()
		drainQueue(ctx, q, yield)
	}
}

func (s *eventService) Durable(ctx context.Context, aggregateID string, after ...int) iter.Seq2[schema.EventPayload, error] {
	start := -1
	if len(after) > 0 {
		start = after[0]
	}
	return func(yield func(schema.EventPayload, error) bool) {
		wake := make(chan struct{}, 1)
		s.addDurableWake(aggregateID, wake)
		defer s.removeDurableWake(aggregateID, wake)

		sequence := start
		for {
			events, err := s.readAfter(aggregateID, sequence)
			if err != nil {
				yield(schema.EventPayload{}, err)
				return
			}
			for _, event := range events {
				if event.Durable != nil {
					sequence = event.Durable.Seq
				}
				if !yield(event, nil) {
					return
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-wake:
			}
		}
	}
}

func drainQueue(ctx context.Context, q *eventQueue, yield func(schema.EventPayload, error) bool) {
	for {
		event, ok, err := q.next(ctx)
		if err != nil {
			yield(schema.EventPayload{}, err)
			return
		}
		if !ok {
			return
		}
		if !yield(event, nil) {
			return
		}
	}
}

func (s *eventService) Publish(def schema.EventDefinition, data any, options *EventPublishOptions) (schema.EventPayload, error) {
	event := schema.EventPayload{Type: def.Type, Data: data}
	if options != nil && options.ID != nil {
		event.ID = *options.ID
	} else {
		event.ID = schema.NewEventID()
	}
	if options != nil {
		event.Metadata = options.Metadata
		if options.Location != nil {
			event.Location = options.Location
		} else {
			event.Location = s.cfg.Location
		}
	} else {
		event.Location = s.cfg.Location
	}

	if def.Durable == nil && options != nil && options.Commit != nil {
		return schema.EventPayload{}, &EventInvalidDurableEventError{
			Type:    def.Type,
			Message: "Local commit hooks require a durable event",
		}
	}

	if def.Durable != nil {
		committed, err := s.commitDurableEvent(def, event, nil, options)
		if err != nil {
			return schema.EventPayload{}, err
		}
		if committed != nil {
			event.Durable = committed
			if err := s.notify(event, true); err != nil {
				return schema.EventPayload{}, err
			}
			return event, nil
		}
	}

	if err := s.notify(event, false); err != nil {
		return schema.EventPayload{}, err
	}
	return event, nil
}

func (s *eventService) Replay(event SerializedEvent, options *EventReplayOptions) error {
	definition, ok := s.cfg.Durable[event.Type]
	if !ok || definition.Durable == nil {
		return &EventInvalidDurableEventError{
			Type:    event.Type,
			Message: fmt.Sprintf("Unknown durable event type %s", event.Type),
		}
	}
	payload := schema.EventPayload{ID: event.ID, Type: definition.Type, Data: event.Data}
	committed, err := s.commitDurableEvent(definition, payload, &eventCommitInput{
		seq:         event.Seq,
		aggregateID: event.AggregateID,
		ownerID:     ownerIDOf(options),
		strictOwner: options != nil && options.StrictOwner,
	}, nil)
	if err != nil {
		return err
	}
	if committed != nil && options != nil && options.Publish {
		payload.Durable = committed
		return s.notify(payload, true)
	}
	return nil
}

func (s *eventService) ReplayAll(events []SerializedEvent, options *EventReplayOptions) (string, error) {
	if len(events) == 0 {
		return "", nil
	}
	source := events[0].AggregateID
	for _, event := range events {
		if event.AggregateID != source {
			return "", &EventInvalidDurableEventError{
				Type:    events[0].Type,
				Message: "Replay events must belong to the same aggregate",
			}
		}
	}
	start := events[0].Seq
	for index, event := range events {
		expected := start + index
		if event.Seq != expected {
			return "", &EventInvalidDurableEventError{
				Type:    event.Type,
				Message: fmt.Sprintf("Replay sequence mismatch at index %d: expected %d, got %d", index, expected, event.Seq),
			}
		}
	}
	for _, event := range events {
		if err := s.Replay(event, options); err != nil {
			return "", err
		}
	}
	return source, nil
}

func (s *eventService) Remove(aggregateID string) error {
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM `event_sequence` WHERE `aggregate_id` = ?", aggregateID); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM `event` WHERE `aggregate_id` = ?", aggregateID); err != nil {
			return err
		}
		return nil
	})
}

func (s *eventService) Claim(aggregateID, ownerID string) error {
	_, err := s.cfg.DB.Exec("UPDATE `event_sequence` SET `owner_id` = ? WHERE `aggregate_id` = ?", ownerID, aggregateID)
	return err
}

type eventCommitInput struct {
	seq         int
	aggregateID string
	ownerID     string
	strictOwner bool
}

func (s *eventService) commitDurableEvent(
	def schema.EventDefinition,
	event schema.EventPayload,
	input *eventCommitInput,
	options *EventPublishOptions,
) (*schema.EventPayloadDurable, error) {
	if def.Durable == nil {
		return nil, nil
	}

	encoded, aggregateID, err := durableAggregate(def, event)
	if err != nil {
		return nil, err
	}
	if input != nil && input.aggregateID != aggregateID {
		return nil, &EventInvalidDurableEventError{
			Type:    event.Type,
			Message: fmt.Sprintf("Aggregate mismatch: expected %s, got %s", input.aggregateID, aggregateID),
		}
	}

	var committed *schema.EventPayloadDurable
	err = s.withTx(func(tx *sql.Tx) error {
		latest, ownerID, err := readEventSequence(tx, aggregateID)
		if err != nil {
			return err
		}
		handled, err := s.replayOrDivergence(tx, def, event, input, aggregateID, encoded, ownerID, latest)
		if err != nil || handled {
			return err
		}
		if input != nil && ownerID.Valid && ownerID.String != input.ownerID {
			return nil
		}
		seq, err := nextSequence(latest, input, event, aggregateID)
		if err != nil {
			return err
		}
		committed, err = s.insertDurableEvent(tx, def, event, options, aggregateID, seq, encoded, ownerID, input)
		return err
	})
	if err != nil {
		return nil, err
	}

	if committed != nil {
		s.signalDurableWakes(committed.AggregateID)
	}
	return committed, nil
}

// replayOrDivergence handles the replay path. It reports handled=true when the
// event is already committed identically, and returns an error when the strict
// owner check fails or the replay diverges.
func (s *eventService) replayOrDivergence(
	tx *sql.Tx,
	def schema.EventDefinition,
	event schema.EventPayload,
	input *eventCommitInput,
	aggregateID string,
	encoded map[string]any,
	ownerID sql.NullString,
	latest int,
) (bool, error) {
	if input == nil {
		return false, nil
	}
	if input.strictOwner && ownerID.Valid && ownerID.String != input.ownerID {
		return false, &EventInvalidDurableEventError{
			Type: event.Type,
			Message: fmt.Sprintf(
				"Replay owner mismatch for aggregate %s: expected %s, got %s",
				aggregateID,
				ownerID.String,
				input.ownerID,
			),
		}
	}
	if input.seq > latest {
		return false, nil
	}
	matched, err := replayMatches(tx, def, event, input, aggregateID, encoded, ownerID)
	if err != nil {
		return false, err
	}
	if matched {
		return true, nil
	}
	return false, &EventInvalidDurableEventError{
		Type:    event.Type,
		Message: fmt.Sprintf("Replay diverged at aggregate %s sequence %d", aggregateID, input.seq),
	}
}

// nextSequence returns the sequence to commit at, validating an explicit replay
// sequence against the latest committed one.
func nextSequence(latest int, input *eventCommitInput, event schema.EventPayload, aggregateID string) (int, error) {
	if input == nil {
		return latest + 1, nil
	}
	if input.seq != latest+1 {
		return 0, &EventInvalidDurableEventError{
			Type:    event.Type,
			Message: fmt.Sprintf("Sequence mismatch for aggregate %s: expected %d, got %d", aggregateID, latest+1, input.seq),
		}
	}
	return input.seq, nil
}

// durableAggregate encodes the event data and resolves its aggregate id.
func durableAggregate(def schema.EventDefinition, event schema.EventPayload) (map[string]any, string, error) {
	encoded, err := eventEncodeData(event.Data)
	if err != nil {
		return nil, "", err
	}
	aggregateID, ok := encoded[def.Durable.Aggregate].(string)
	if !ok {
		return nil, "", &EventInvalidDurableEventError{
			Type:    event.Type,
			Message: fmt.Sprintf("Expected string aggregate field %s", def.Durable.Aggregate),
		}
	}
	return encoded, aggregateID, nil
}

// readEventSequence reads the latest committed sequence and owner for an
// aggregate, treating a missing row as sequence -1 with no owner.
func readEventSequence(tx *sql.Tx, aggregateID string) (int, sql.NullString, error) {
	latest := -1
	var ownerID sql.NullString
	row := tx.QueryRow("SELECT `seq`, `owner_id` FROM `event_sequence` WHERE `aggregate_id` = ?", aggregateID)
	switch err := row.Scan(&latest, &ownerID); err {
	case nil:
	case sql.ErrNoRows:
		latest = -1
	default:
		return 0, sql.NullString{}, err
	}
	return latest, ownerID, nil
}

// replayMatches reports whether the stored event at input.seq is identical to
// the replayed one. When it matches, it also claims the aggregate for the
// replayer if the stored row has no owner yet.
func replayMatches(
	tx *sql.Tx,
	def schema.EventDefinition,
	event schema.EventPayload,
	input *eventCommitInput,
	aggregateID string,
	encoded map[string]any,
	ownerID sql.NullString,
) (bool, error) {
	var storedID, storedType, storedData string
	err := tx.QueryRow(
		"SELECT `id`, `type`, `data` FROM `event` WHERE `aggregate_id` = ? AND `seq` = ?",
		aggregateID,
		input.seq,
	).Scan(&storedID, &storedType, &storedData)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var stored map[string]any
	if err := json.Unmarshal([]byte(storedData), &stored); err != nil {
		return false, err
	}
	versioned := schema.VersionedType(def.Type, def.Durable.Version)
	if storedID != string(event.ID) || storedType != versioned || !reflect.DeepEqual(stored, encoded) {
		return false, nil
	}
	if input.ownerID != "" && !ownerID.Valid {
		if _, err := tx.Exec(
			"UPDATE `event_sequence` SET `owner_id` = ? WHERE `aggregate_id` = ?",
			input.ownerID,
			aggregateID,
		); err != nil {
			return false, err
		}
	}
	return true, nil
}

// insertDurableEvent appends the event, updates the aggregate sequence, and
// runs the projectors and commit hook inside the transaction.
func (s *eventService) insertDurableEvent(
	tx *sql.Tx,
	def schema.EventDefinition,
	event schema.EventPayload,
	options *EventPublishOptions,
	aggregateID string,
	seq int,
	encoded map[string]any,
	ownerID sql.NullString,
	input *eventCommitInput,
) (*schema.EventPayloadDurable, error) {
	var storedAggregate string
	var storedSeq int
	err := tx.QueryRow("SELECT `aggregate_id`, `seq` FROM `event` WHERE `id` = ?", string(event.ID)).
		Scan(&storedAggregate, &storedSeq)
	if err == nil {
		return nil, &EventInvalidDurableEventError{
			Type:    event.Type,
			Message: fmt.Sprintf("Event %s already exists at aggregate %s sequence %d", event.ID, storedAggregate, storedSeq),
		}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	payload := schema.EventPayloadDurable{AggregateID: aggregateID, Seq: seq, Version: def.Durable.Version}
	committedEvent := event
	committedEvent.Durable = &payload

	for _, projector := range s.projectorsFor(def.Type) {
		if err := projector(committedEvent, tx); err != nil {
			return nil, err
		}
	}
	if options != nil && options.Commit != nil {
		if err := options.Commit(tx, seq); err != nil {
			return nil, err
		}
	}

	var valueOwner any
	if input != nil && input.ownerID != "" {
		valueOwner = input.ownerID
	}
	claim := input != nil && input.ownerID != "" && !ownerID.Valid
	upsert := "INSERT INTO `event_sequence` (`aggregate_id`, `seq`, `owner_id`) VALUES (?, ?, ?) " +
		"ON CONFLICT(`aggregate_id`) DO UPDATE SET `seq` = excluded.`seq`"
	if claim {
		upsert += ", `owner_id` = excluded.`owner_id`"
	}
	if _, err := tx.Exec(upsert, aggregateID, seq, valueOwner); err != nil {
		return nil, err
	}

	dataJSON, err := json.Marshal(encoded)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(
		"INSERT INTO `event` (`id`, `aggregate_id`, `seq`, `type`, `data`) VALUES (?, ?, ?, ?, ?)",
		string(event.ID),
		aggregateID,
		seq,
		schema.VersionedType(def.Type, def.Durable.Version),
		string(dataJSON),
	); err != nil {
		return nil, err
	}

	return &payload, nil
}

func (s *eventService) readAfter(aggregateID string, after int) ([]schema.EventPayload, error) {
	if s.cfg.BeforeAggregateRead != nil {
		if err := s.cfg.BeforeAggregateRead(aggregateID); err != nil {
			return nil, err
		}
	}
	rows, err := s.cfg.DB.Query(
		"SELECT `id`, `aggregate_id`, `seq`, `type`, `data` FROM `event` "+
			"WHERE `aggregate_id` = ? AND `seq` > ? ORDER BY `seq` ASC",
		aggregateID,
		after,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []schema.EventPayload
	for rows.Next() {
		var id, agg, eventType, dataJSON string
		var seq int
		if err := rows.Scan(&id, &agg, &seq, &eventType, &dataJSON); err != nil {
			return nil, err
		}
		definition, ok := s.cfg.Durable[eventType]
		if !ok || definition.Durable == nil {
			return nil, &EventInvalidDurableEventError{
				Type:    eventType,
				Message: fmt.Sprintf("Unknown durable event type %s", eventType),
			}
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
			return nil, err
		}
		events = append(events, schema.EventPayload{
			ID:   schema.EventID(id),
			Type: definition.Type,
			Data: data,
			Durable: &schema.EventPayloadDurable{
				AggregateID: agg,
				Seq:         seq,
				Version:     definition.Durable.Version,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *eventService) notify(event schema.EventPayload, isolate bool) error {
	s.mu.Lock()
	listeners := append([]eventListenerEntry(nil), s.listeners...)
	typed := append([]*eventQueue(nil), s.typedSubscribers[event.Type]...)
	all := append([]*eventQueue(nil), s.allSubscribers...)
	s.mu.Unlock()

	for _, entry := range listeners {
		if err := s.runListener(entry.fn, event, isolate); err != nil {
			return err
		}
	}
	for _, q := range typed {
		q.offer(event)
	}
	for _, q := range all {
		q.offer(event)
	}
	return nil
}

func (s *eventService) runListener(listener EventListener, event schema.EventPayload, isolate bool) (err error) {
	if !isolate {
		return listener(event)
	}
	defer func() {
		if recover() != nil {
			err = nil
		}
	}()
	err = listener(event)
	if err != nil && !isEventInterrupt(err) {
		return nil
	}
	return err
}

func isEventInterrupt(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var interrupt *EventInterruptError
	return errors.As(err, &interrupt)
}

func (s *eventService) withTx(fn func(tx *sql.Tx) error) error {
	tx, err := s.cfg.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func eventEncodeData(data any) (map[string]any, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var encoded map[string]any
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}

func ownerIDOf(options *EventReplayOptions) string {
	if options == nil {
		return ""
	}
	return options.OwnerID
}

func EventLatestSequence(db EventDB, aggregateID string) (int, error) {
	var seq int
	err := db.QueryRow("SELECT `seq` FROM `event_sequence` WHERE `aggregate_id` = ?", aggregateID).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return -1, nil
	}
	if err != nil {
		return -1, err
	}
	return seq, nil
}

type EventReadAggregateManifest struct {
	Definitions map[string]schema.EventDefinition
}

type EventReadAggregateInput struct {
	AggregateID string
	After       *int
	Limit       int
	Manifest    EventReadAggregateManifest
}

type EventReadAggregateResult struct {
	Events  []schema.EventPayload
	HasMore bool
}

func EventReadAggregate(db EventDB, input EventReadAggregateInput) (EventReadAggregateResult, error) {
	types := make([]string, 0, len(input.Manifest.Definitions))
	for eventType := range input.Manifest.Definitions {
		types = append(types, eventType)
	}
	if len(types) == 0 {
		return EventReadAggregateResult{Events: []schema.EventPayload{}}, nil
	}

	after := -1
	if input.After != nil {
		after = *input.After
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(types)), ",")
	query := "SELECT `id`, `aggregate_id`, `seq`, `type`, `data` FROM `event` " +
		"WHERE `aggregate_id` = ? AND `seq` > ? AND `type` IN (" + placeholders + ") " +
		"ORDER BY `seq` ASC LIMIT ?"
	args := make([]any, 0, len(types)+3)
	args = append(args, input.AggregateID, after)
	for _, eventType := range types {
		args = append(args, eventType)
	}
	args = append(args, input.Limit+1)

	rows, err := db.Query(query, args...)
	if err != nil {
		return EventReadAggregateResult{}, err
	}
	defer rows.Close()

	var events []schema.EventPayload
	for rows.Next() {
		var id, agg, eventType, dataJSON string
		var seq int
		if err := rows.Scan(&id, &agg, &seq, &eventType, &dataJSON); err != nil {
			return EventReadAggregateResult{}, err
		}
		definition := input.Manifest.Definitions[eventType]
		payloadType := definition.Type
		if payloadType == "" {
			payloadType = eventType
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
			return EventReadAggregateResult{}, err
		}
		payload := schema.EventPayload{
			ID:   schema.EventID(id),
			Type: payloadType,
			Data: data,
		}
		if definition.Durable != nil {
			payload.Durable = &schema.EventPayloadDurable{
				AggregateID: agg,
				Seq:         seq,
				Version:     definition.Durable.Version,
			}
		}
		events = append(events, payload)
	}
	if err := rows.Err(); err != nil {
		return EventReadAggregateResult{}, err
	}

	hasMore := len(events) > input.Limit
	if hasMore {
		events = events[:input.Limit]
	}
	if events == nil {
		events = []schema.EventPayload{}
	}
	return EventReadAggregateResult{Events: events, HasMore: hasMore}, nil
}
