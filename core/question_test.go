package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

type testQuestionEvents struct {
	asked    []QuestionRequest
	replied  []QuestionRepliedData
	rejected []QuestionRejectedData
}

func (e *testQuestionEvents) Publish(def schema.EventDefinition, data any, _ *EventPublishOptions) (schema.EventPayload, error) {
	switch def.Type {
	case schema.QuestionEventAsked.Type:
		if request, ok := data.(QuestionRequest); ok {
			e.asked = append(e.asked, request)
		}
	case schema.QuestionEventReplied.Type:
		if payload, ok := data.(QuestionRepliedData); ok {
			e.replied = append(e.replied, payload)
		}
	case schema.QuestionEventRejected.Type:
		if payload, ok := data.(QuestionRejectedData); ok {
			e.rejected = append(e.rejected, payload)
		}
	}
	return schema.EventPayload{Type: def.Type, Data: data}, nil
}

func questionTestInput() QuestionAskInput {
	return QuestionAskInput{
		SessionID: "ses_1",
		Questions: []QuestionInfo{{
			Question: "Which?",
			Header:   "Which",
			Options:  []QuestionOption{{Label: "A", Description: "first"}, {Label: "B", Description: "second"}},
		}},
		Tool: &QuestionTool{MessageID: "msg_1", CallID: "call_1"},
	}
}

func waitForPending(t *testing.T, q Question) QuestionRequest {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if list := q.List(); len(list) > 0 {
			return list[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for pending question")
	return QuestionRequest{}
}

func TestQuestion_AskReply(t *testing.T) {
	events := &testQuestionEvents{}
	q := NewQuestion(QuestionConfig{Events: events})

	answers := make(chan []QuestionAnswer, 1)
	errs := make(chan error, 1)
	go func() {
		result, err := q.Ask(context.Background(), questionTestInput())
		if err != nil {
			errs <- err
			return
		}
		answers <- result
	}()

	pending := waitForPending(t, q)
	if pending.SessionID != "ses_1" {
		t.Fatalf("unexpected session: %s", pending.SessionID)
	}
	if pending.Tool == nil || pending.Tool.CallID != "call_1" {
		t.Fatalf("unexpected tool metadata: %+v", pending.Tool)
	}

	if err := q.Reply(QuestionReplyInput{RequestID: pending.ID, Answers: []QuestionAnswer{{"A"}}}); err != nil {
		t.Fatalf("reply: %v", err)
	}

	select {
	case result := <-answers:
		if len(result) != 1 || len(result[0]) != 1 || result[0][0] != "A" {
			t.Fatalf("unexpected answers: %+v", result)
		}
	case err := <-errs:
		t.Fatalf("ask failed: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for answers")
	}

	if len(events.asked) != 1 {
		t.Fatalf("expected 1 asked event, got %d", len(events.asked))
	}
	if len(events.replied) != 1 {
		t.Fatalf("expected 1 replied event, got %d", len(events.replied))
	}
	if events.replied[0].SessionID != "ses_1" || events.replied[0].RequestID != pending.ID {
		t.Fatalf("unexpected replied payload: %+v", events.replied[0])
	}
	if len(events.replied[0].Answers) != 1 || events.replied[0].Answers[0][0] != "A" {
		t.Fatalf("unexpected replied answers: %+v", events.replied[0].Answers)
	}
	if list := q.List(); len(list) != 0 {
		t.Fatalf("expected no pending questions, got %d", len(list))
	}
}

func TestQuestion_AskRejected(t *testing.T) {
	events := &testQuestionEvents{}
	q := NewQuestion(QuestionConfig{Events: events})

	errs := make(chan error, 1)
	go func() {
		_, err := q.Ask(context.Background(), questionTestInput())
		errs <- err
	}()

	pending := waitForPending(t, q)
	if err := q.Reject(pending.ID); err != nil {
		t.Fatalf("reject: %v", err)
	}

	select {
	case err := <-errs:
		var rejected *QuestionRejectedError
		if !errors.As(err, &rejected) {
			t.Fatalf("expected rejected error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for rejection")
	}

	if len(events.rejected) != 1 {
		t.Fatalf("expected 1 rejected event, got %d", len(events.rejected))
	}
	if events.rejected[0].SessionID != "ses_1" || events.rejected[0].RequestID != pending.ID {
		t.Fatalf("unexpected rejected payload: %+v", events.rejected[0])
	}
}

func TestQuestion_PublishesToEventV2(t *testing.T) {
	db := eventTestDB(t)
	events := NewEvent(EventConfig{DB: db.DB})
	q := NewQuestion(QuestionConfig{Events: events})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	received := make(chan schema.EventPayload, 1)
	go func() {
		for event, err := range events.Subscribe(ctx, schema.QuestionEventAsked) {
			if err != nil {
				return
			}
			received <- event
		}
	}()
	waitForEventRegistration(t, events, func(svc *eventService) bool {
		return len(svc.typedSubscribers[schema.QuestionEventAsked.Type]) >= 1
	})

	go func() { _, _ = q.Ask(context.Background(), questionTestInput()) }()

	select {
	case event := <-received:
		request, ok := event.Data.(QuestionRequest)
		if !ok {
			t.Fatalf("unexpected asked payload type: %T", event.Data)
		}
		if request.SessionID != "ses_1" || request.Questions[0].Question != "Which?" {
			t.Fatalf("unexpected asked payload: %+v", request)
		}
		if err := q.Reply(QuestionReplyInput{RequestID: request.ID, Answers: []QuestionAnswer{{"A"}}}); err != nil {
			t.Fatalf("reply: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for asked event")
	}
}

func TestQuestion_ReplyNotFound(t *testing.T) {
	q := NewQuestion(QuestionConfig{Events: &testQuestionEvents{}})
	err := q.Reply(QuestionReplyInput{RequestID: "que_missing"})
	var notFound *QuestionNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestQuestion_CloseRejectsPending(t *testing.T) {
	events := &testQuestionEvents{}
	q := NewQuestion(QuestionConfig{Events: events})

	errs := make(chan error, 1)
	go func() {
		_, err := q.Ask(context.Background(), questionTestInput())
		errs <- err
	}()
	waitForPending(t, q)

	if err := q.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case err := <-errs:
		var rejected *QuestionRejectedError
		if !errors.As(err, &rejected) {
			t.Fatalf("expected rejected error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for close to reject pending ask")
	}

	if list := q.List(); len(list) != 0 {
		t.Fatalf("expected no pending after close, got %d", len(list))
	}
	if len(events.rejected) != 0 {
		t.Fatalf("close must not publish rejected events, got %d", len(events.rejected))
	}
}

func TestQuestion_AskAfterClose(t *testing.T) {
	q := NewQuestion(QuestionConfig{Events: &testQuestionEvents{}})
	if err := q.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, err := q.Ask(context.Background(), questionTestInput())
	var rejected *QuestionRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("expected rejected error, got %v", err)
	}
}

func TestQuestion_CloseWithoutPending(t *testing.T) {
	q := NewQuestion(QuestionConfig{Events: &testQuestionEvents{}})
	if err := q.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := q.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestQuestion_ContextCancelRejectsPending(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := NewQuestionContext(ctx, QuestionConfig{Events: &testQuestionEvents{}})

	errs := make(chan error, 1)
	go func() {
		_, err := q.Ask(context.Background(), questionTestInput())
		errs <- err
	}()
	waitForPending(t, q)

	cancel()

	select {
	case err := <-errs:
		var rejected *QuestionRejectedError
		if !errors.As(err, &rejected) {
			t.Fatalf("expected rejected error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for context cancel to reject pending ask")
	}
	if list := q.List(); len(list) != 0 {
		t.Fatalf("expected no pending after cancel, got %d", len(list))
	}
}

func TestQuestion_DoubleReply(t *testing.T) {
	q := NewQuestion(QuestionConfig{Events: &testQuestionEvents{}})

	answers := make(chan []QuestionAnswer, 1)
	go func() {
		result, _ := q.Ask(context.Background(), questionTestInput())
		answers <- result
	}()

	pending := waitForPending(t, q)
	first := QuestionReplyInput{RequestID: pending.ID, Answers: []QuestionAnswer{{"A"}}}
	if err := q.Reply(first); err != nil {
		t.Fatalf("first reply: %v", err)
	}
	if err := q.Reply(first); err == nil {
		t.Fatal("expected second reply to fail")
	} else {
		var notFound *QuestionNotFoundError
		if !errors.As(err, &notFound) {
			t.Fatalf("expected not found error, got %v", err)
		}
	}

	select {
	case result := <-answers:
		if len(result) != 1 || result[0][0] != "A" {
			t.Fatalf("unexpected answers: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for answers")
	}
}
