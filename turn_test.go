package agent

import (
	"context"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

func TestLocalTurnIsRecorded(t *testing.T) {
	client := newTestClient(t)
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	for _, err := range session.Send(context.Background(), MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	turns, err := client.TurnStore().ListForSession(schema.SessionID(session.ID()))
	if err != nil {
		t.Fatalf("list turns: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(turns))
	}
	if turns[0].State != schema.TurnStateCompleted {
		t.Fatalf("state = %s, want completed", turns[0].State)
	}
	if turns[0].AnchorSeq == nil || turns[0].EndSeq == nil {
		t.Fatal("anchor/end sequence not recorded")
	}
}

func TestSubagentTurnLinksParent(t *testing.T) {
	calls := 0
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model: "test-model", Root: t.TempDir(), Permission: ApproveAll,
		LLM: taskLLM{parentCalls: &calls},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{System: "You are a test."}); err != nil {
		t.Fatalf("register: %v", err)
	}
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	for _, err := range session.Send(context.Background(), MessageOptions{Prompt: "go"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	turns, err := client.TurnStore().ListAll()
	if err != nil {
		t.Fatalf("list turns: %v", err)
	}
	var parentID schema.TurnID
	for _, turn := range turns {
		if turn.ParentTurnID == nil {
			parentID = turn.ID
		}
	}
	linked := false
	for _, turn := range turns {
		if turn.ParentTurnID == nil {
			continue
		}
		if parentID == "" || *turn.ParentTurnID != parentID {
			t.Fatalf("child parent = %v, want %v", turn.ParentTurnID, parentID)
		}
		linked = true
	}
	if !linked {
		t.Fatal("no subagent turn linked to its parent")
	}
}
