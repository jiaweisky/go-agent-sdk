package agent

import (
	"context"
	"testing"
	"time"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/schema"
)

// TestHoldPermissionRoundTrip guards the out-of-band permission flow: a held
// assert blocks, surfaces a PermissionAsked event, and is released by
// Client.Permission().Reply.
func TestHoldPermissionRoundTrip(t *testing.T) {
	client := newTestClient(t)
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	asked := make(chan PermissionAsked, 1)
	client.On(func(event Event) {
		if request, ok := event.(PermissionAsked); ok {
			asked <- request
		}
	})

	client.HoldInteractions(true)
	done := make(chan error, 1)
	go func() {
		// external_directory outside the allowed paths is an "ask" in the
		// default agent rules, so the assert is held.
		done <- client.rt.permission.Assert(core.PermissionAssertInput{
			SessionID: session.ID(), Action: "external_directory", Resources: []string{"/nonexistent/zzz"},
		})
	}()

	request := waitFor(t, asked, "permission asked")
	if request.Request.ID == "" {
		t.Fatal("PermissionAsked request has no id")
	}
	if pending := client.Permission().List(); len(pending) != 1 {
		t.Fatalf("pending = %+v, want one", pending)
	}
	if err := client.Permission().Reply(PermissionReplyInput{
		RequestID: request.Request.ID, Reply: PermissionReplyOnce,
	}); err != nil {
		t.Fatalf("reply: %v", err)
	}
	if err := waitFor(t, done, "assert"); err != nil {
		t.Fatalf("assert: %v", err)
	}
}

// TestHoldQuestionRoundTrip guards the out-of-band question flow: a held ask
// blocks, surfaces a QuestionAsked event, and is released by
// Client.Question().Reply.
func TestHoldQuestionRoundTrip(t *testing.T) {
	client := newTestClient(t)
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	asked := make(chan QuestionAsked, 1)
	client.On(func(event Event) {
		if request, ok := event.(QuestionAsked); ok {
			asked <- request
		}
	})

	client.HoldInteractions(true)
	answers := make(chan []core.QuestionAnswer, 1)
	go func() {
		result, _ := client.rt.question.Ask(context.Background(), core.QuestionAskInput{
			SessionID: session.ID(),
			Questions: []schema.QuestionInfo{{Question: "Pick", Options: []schema.QuestionOption{{Label: "a"}}}},
		})
		answers <- result
	}()

	request := waitFor(t, asked, "question asked")
	if err := client.Question().Reply(request.Request.ID, []schema.QuestionAnswer{{"a"}}); err != nil {
		t.Fatalf("reply: %v", err)
	}
	result := waitFor(t, answers, "ask")
	if len(result) != 1 || len(result[0]) != 1 || result[0][0] != "a" {
		t.Fatalf("answers = %+v, want [[a]]", result)
	}
}

// TestHoldQuestionReject guards Client.Question().Reject settling a held ask
// with an error and a QuestionRejected event.
func TestHoldQuestionReject(t *testing.T) {
	client := newTestClient(t)
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	asked := make(chan QuestionAsked, 1)
	rejected := make(chan QuestionRejected, 1)
	client.On(func(event Event) {
		switch e := event.(type) {
		case QuestionAsked:
			asked <- e
		case QuestionRejected:
			rejected <- e
		}
	})

	client.HoldInteractions(true)
	failures := make(chan error, 1)
	go func() {
		_, err := client.rt.question.Ask(context.Background(), core.QuestionAskInput{
			SessionID: session.ID(),
			Questions: []schema.QuestionInfo{{Question: "Pick"}},
		})
		failures <- err
	}()

	request := waitFor(t, asked, "question asked")
	if err := client.Question().Reject(request.Request.ID); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if err := waitFor(t, failures, "ask"); err == nil {
		t.Fatal("a rejected ask should fail")
	}
	waitFor(t, rejected, "question rejected")
}

// waitFor waits for a value on ch, failing the test on timeout.
func waitFor[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		var zero T
		t.Fatalf("timed out waiting for %s", what)
		return zero
	}
}
