package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

// TestForkedTitle guards the "(fork #N)" suffix bumping.
func TestForkedTitle(t *testing.T) {
	cases := map[string]string{
		"New session - 2026": "New session - 2026 (fork #1)",
		"My chat (fork #1)":  "My chat (fork #2)",
		"My chat (fork #9)":  "My chat (fork #10)",
	}
	for in, want := range cases {
		if got := forkedTitle(in); got != want {
			t.Errorf("forkedTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestForkSessionCopiesHistory guards that a fork clones the source's messages
// with fresh ids, leaves the source untouched and titles the copy.
func TestForkSessionCopiesHistory(t *testing.T) {
	client := newTestClient(t)
	source, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer source.Close()
	sendForTest(t, source, "first question")

	sourceMsgs, err := source.Messages()
	if err != nil {
		t.Fatalf("source messages: %v", err)
	}
	if len(sourceMsgs) == 0 {
		t.Fatal("source has no messages")
	}

	forked, err := client.ForkSession(context.Background(), source.ID(), "")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	defer forked.Close()

	forkMsgs, err := forked.Messages()
	if err != nil {
		t.Fatalf("fork messages: %v", err)
	}
	if len(forkMsgs) != len(sourceMsgs) {
		t.Fatalf("fork has %d messages, want %d", len(forkMsgs), len(sourceMsgs))
	}
	for i := range sourceMsgs {
		if messageBody(t, forkMsgs[i]) != messageBody(t, sourceMsgs[i]) {
			t.Fatalf("message %d content changed", i)
		}
		if marshalMessage(t, forkMsgs[i]) == marshalMessage(t, sourceMsgs[i]) {
			t.Fatalf("message %d kept its id", i)
		}
	}

	title := sessionTitle(t, client, forked.ID())
	if !strings.HasSuffix(title, "(fork #1)") {
		t.Fatalf("fork title = %q, want a (fork #1) suffix", title)
	}
}

// TestForkSessionBoundary guards that a messageID boundary drops that message
// and everything after it.
func TestForkSessionBoundary(t *testing.T) {
	client := newTestClient(t)
	source, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer source.Close()
	sendForTest(t, source, "first")
	sendForTest(t, source, "second")

	boundary := secondUserMessageID(t, source)
	if boundary == "" {
		t.Fatal("no second user message")
	}

	forked, err := client.ForkSession(context.Background(), source.ID(), boundary)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	defer forked.Close()

	forkMsgs, err := forked.Messages()
	if err != nil {
		t.Fatalf("fork messages: %v", err)
	}
	if users := countUserMessages(forkMsgs); users != 1 {
		t.Fatalf("fork has %d user messages, want 1 (before the boundary)", users)
	}
}

// TestForkSessionMissing guards forking an unknown session.
func TestForkSessionMissing(t *testing.T) {
	client := newTestClient(t)
	if _, err := client.ForkSession(context.Background(), "ses_missing", ""); err == nil {
		t.Fatal("forking a missing session should fail")
	}
}

func sendForTest(t *testing.T, session *Session, prompt string) {
	t.Helper()
	for _, err := range session.Send(context.Background(), MessageOptions{Prompt: prompt}) {
		if err != nil {
			t.Fatalf("send %q: %v", prompt, err)
		}
	}
}

// secondUserMessageID returns the id of a session's second user message, or "".
func secondUserMessageID(t *testing.T, session *Session) string {
	t.Helper()
	messages, err := session.Messages()
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	users := 0
	for _, message := range messages {
		if user, ok := message.(schema.SessionMessageUser); ok {
			users++
			if users == 2 {
				return string(user.ID)
			}
		}
	}
	return ""
}

func countUserMessages(messages []schema.SessionMessage) int {
	count := 0
	for _, message := range messages {
		if _, ok := message.(schema.SessionMessageUser); ok {
			count++
		}
	}
	return count
}

func sessionTitle(t *testing.T, client *Client, id string) string {
	t.Helper()
	infos, err := client.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	for _, info := range infos {
		if info.ID == id {
			return info.Title
		}
	}
	return ""
}

func marshalMessage(t *testing.T, message schema.SessionMessage) string {
	t.Helper()
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	return string(encoded)
}

// messageBody is a message's JSON without its id, so two copies can be compared
// for equal content regardless of their ids.
func messageBody(t *testing.T, message schema.SessionMessage) string {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(marshalMessage(t, message)), &object); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	delete(object, "id")
	encoded, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return string(encoded)
}
