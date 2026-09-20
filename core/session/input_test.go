package session

import (
	"errors"
	"testing"

	"github.com/user/go-agent-sdk/core/database"
	"github.com/user/go-agent-sdk/schema"
)

// inputTestPublisher simulates the event projector for the session-input
// lifecycle events so the input functions can be tested end to end.
type inputTestPublisher struct {
	db        *database.Database
	seq       int
	published []string
}

func (p *inputTestPublisher) Publish(def schema.EventDefinition, data any) (*schema.EventPayloadDurable, error) {
	p.published = append(p.published, def.Type)
	p.seq++
	durable := &schema.EventPayloadDurable{Seq: p.seq}
	payload := data.(map[string]any)
	switch def.Type {
	case schema.SessionEventPromptAdmitted.Type:
		return durable, ProjectAdmitted(p.db.DB, ProjectAdmittedInput{
			AdmittedSeq: int64(p.seq),
			ID:          payload["messageID"].(schema.MessageID),
			SessionID:   payload["sessionID"].(schema.SessionID),
			Prompt:      payload["prompt"].(schema.Prompt),
			Delivery:    payload["delivery"].(schema.SessionDelivery),
			TimeCreated: payload["timestamp"].(int64),
		})
	case schema.SessionEventPrompted.Type:
		return durable, ProjectPrompted(p.db.DB, ProjectPromptedInput{
			ID:          payload["messageID"].(schema.MessageID),
			SessionID:   payload["sessionID"].(schema.SessionID),
			Prompt:      payload["prompt"].(schema.Prompt),
			Delivery:    schema.SessionDelivery(payload["delivery"].(string)),
			TimeCreated: payload["timestamp"].(int64),
			PromotedSeq: int64(p.seq),
		})
	}
	return durable, nil
}

// fallbackPublisher projects the admitted row but returns no durable pointer,
// exercising Admit's find fallback.
type fallbackPublisher struct{ db *database.Database }

func (p *fallbackPublisher) Publish(def schema.EventDefinition, data any) (*schema.EventPayloadDurable, error) {
	payload := data.(map[string]any)
	if err := ProjectAdmitted(p.db.DB, ProjectAdmittedInput{
		AdmittedSeq: 7,
		ID:          payload["messageID"].(schema.MessageID),
		SessionID:   payload["sessionID"].(schema.SessionID),
		Prompt:      payload["prompt"].(schema.Prompt),
		Delivery:    payload["delivery"].(schema.SessionDelivery),
		TimeCreated: payload["timestamp"].(int64),
	}); err != nil {
		return nil, err
	}
	return nil, nil
}

func TestInputAdmitFallsBackToStored(t *testing.T) {
	db := openSessionTestDB(t)
	admitted, err := Admit(db.DB, &fallbackPublisher{db: db}, AdmitInput{
		ID: "msg_1", SessionID: "ses_1", Prompt: schema.Prompt{Text: "hi"}, Delivery: schema.SessionDeliveryQueue,
	})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if admitted.AdmittedSeq != 7 {
		t.Fatalf("admitted = %+v", admitted)
	}
}

func TestInputAdmitIsIdempotent(t *testing.T) {
	db := openSessionTestDB(t)
	publisher := &inputTestPublisher{db: db}

	first, err := Admit(db.DB, publisher, AdmitInput{
		ID: "msg_1", SessionID: "ses_1", Prompt: schema.Prompt{Text: "hi"}, Delivery: schema.SessionDeliveryQueue,
	})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if first.AdmittedSeq != 1 || first.Prompt.Text != "hi" || first.PromotedSeq != nil {
		t.Fatalf("admitted = %+v", first)
	}

	second, err := Admit(db.DB, publisher, AdmitInput{
		ID: "msg_1", SessionID: "ses_1", Prompt: schema.Prompt{Text: "hi"}, Delivery: schema.SessionDeliveryQueue,
	})
	if err != nil {
		t.Fatalf("second admit: %v", err)
	}
	if second.AdmittedSeq != first.AdmittedSeq {
		t.Fatalf("second = %+v", second)
	}
	if len(publisher.published) != 1 {
		t.Fatalf("published = %v", publisher.published)
	}
}

func TestInputProjectAdmittedAndPrompted(t *testing.T) {
	db := openSessionTestDB(t)
	prompt := schema.Prompt{Text: "hello"}

	if err := ProjectAdmitted(db.DB, ProjectAdmittedInput{
		AdmittedSeq: 1, ID: "msg_1", SessionID: "ses_1", Prompt: prompt,
		Delivery: schema.SessionDeliveryQueue, TimeCreated: 100,
	}); err != nil {
		t.Fatalf("projectAdmitted: %v", err)
	}
	admitted, err := Find(db.DB, "msg_1")
	if err != nil || admitted == nil {
		t.Fatalf("find: %v %+v", err, admitted)
	}
	if admitted.AdmittedSeq != 1 || admitted.Delivery != schema.SessionDeliveryQueue ||
		admitted.Prompt.Text != "hello" || admitted.PromotedSeq != nil || admitted.TimeCreated != 100 {
		t.Fatalf("admitted = %+v", admitted)
	}
	pending, err := HasPending(db.DB, "ses_1", schema.SessionDeliveryQueue)
	if err != nil || !pending {
		t.Fatalf("hasPending = %v %v", pending, err)
	}

	if err := ProjectPrompted(db.DB, ProjectPromptedInput{
		ID: "msg_1", SessionID: "ses_1", Prompt: prompt,
		Delivery: schema.SessionDeliveryQueue, TimeCreated: 100, PromotedSeq: 2,
	}); err != nil {
		t.Fatalf("projectPrompted: %v", err)
	}
	admitted, _ = Find(db.DB, "msg_1")
	if admitted.PromotedSeq == nil || *admitted.PromotedSeq != 2 {
		t.Fatalf("promoted = %+v", admitted)
	}
	pending, _ = HasPending(db.DB, "ses_1", schema.SessionDeliveryQueue)
	if pending {
		t.Fatalf("expected no pending")
	}
}

func TestInputProjectAdmittedConflict(t *testing.T) {
	db := openSessionTestDB(t)
	insertMessage(t, db, "msg_1", "ses_1", "system", 0, `{"text":"x","time":{"created":0}}`)

	err := ProjectAdmitted(db.DB, ProjectAdmittedInput{
		AdmittedSeq: 1, ID: "msg_1", SessionID: "ses_1", Prompt: schema.Prompt{Text: "x"},
		Delivery: schema.SessionDeliveryQueue, TimeCreated: 1,
	})
	var conflict *LifecycleConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("expected LifecycleConflict, got %v", err)
	}
}

func TestInputProjectAdmittedDuplicate(t *testing.T) {
	db := openSessionTestDB(t)
	input := ProjectAdmittedInput{
		AdmittedSeq: 1, ID: "msg_1", SessionID: "ses_1", Prompt: schema.Prompt{Text: "x"},
		Delivery: schema.SessionDeliveryQueue, TimeCreated: 1,
	}
	if err := ProjectAdmitted(db.DB, input); err != nil {
		t.Fatalf("first: %v", err)
	}
	err := ProjectAdmitted(db.DB, input)
	var conflict *LifecycleConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("expected LifecycleConflict, got %v", err)
	}
}

func TestInputProjectPromptedInserts(t *testing.T) {
	db := openSessionTestDB(t)
	if err := ProjectPrompted(db.DB, ProjectPromptedInput{
		ID: "msg_9", SessionID: "ses_1", Prompt: schema.Prompt{Text: "x"},
		Delivery: schema.SessionDeliverySteer, TimeCreated: 5, PromotedSeq: 3,
	}); err != nil {
		t.Fatalf("projectPrompted: %v", err)
	}
	admitted, err := Find(db.DB, "msg_9")
	if err != nil || admitted == nil {
		t.Fatalf("find: %v %+v", err, admitted)
	}
	if admitted.AdmittedSeq != 3 || admitted.PromotedSeq == nil || *admitted.PromotedSeq != 3 {
		t.Fatalf("admitted = %+v", admitted)
	}
}

func TestInputPromote(t *testing.T) {
	db := openSessionTestDB(t)
	publisher := &inputTestPublisher{db: db}
	for i, id := range []schema.MessageID{"msg_1", "msg_2"} {
		if err := ProjectAdmitted(db.DB, ProjectAdmittedInput{
			AdmittedSeq: int64(i + 1), ID: id, SessionID: "ses_1", Prompt: schema.Prompt{Text: string(id)},
			Delivery: schema.SessionDeliveryQueue, TimeCreated: int64(i + 1),
		}); err != nil {
			t.Fatalf("projectAdmitted: %v", err)
		}
	}

	if ok, err := PromoteNextQueued(db.DB, publisher, "ses_1"); err != nil || !ok {
		t.Fatalf("first promote = %v %v", ok, err)
	}
	if ok, err := PromoteNextQueued(db.DB, publisher, "ses_1"); err != nil || !ok {
		t.Fatalf("second promote = %v %v", ok, err)
	}
	if ok, err := PromoteNextQueued(db.DB, publisher, "ses_1"); err != nil || ok {
		t.Fatalf("third promote = %v %v", ok, err)
	}
	if len(publisher.published) != 2 {
		t.Fatalf("published = %v", publisher.published)
	}

	admitted, _ := Find(db.DB, "msg_1")
	if admitted.PromotedSeq == nil || *admitted.PromotedSeq != 1 {
		t.Fatalf("msg_1 = %+v", admitted)
	}
	admitted, _ = Find(db.DB, "msg_2")
	if admitted.PromotedSeq == nil || *admitted.PromotedSeq != 2 {
		t.Fatalf("msg_2 = %+v", admitted)
	}
}
