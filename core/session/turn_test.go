package session

import (
	"testing"
	"time"

	"github.com/user/go-agent-sdk/schema"
)

func TestTurnStoreFindByMetadata(t *testing.T) {
	db := openSessionTestDB(t)
	store := NewTurnStore(db)
	now := time.Now().UnixMilli()

	turns := []schema.TurnInfo{
		{ID: "trn_1", SessionID: "ses_1", State: schema.TurnStateRunning,
			Metadata: map[string]any{"a2a.context_id": "ctx-1", "sdk_agent_id": "build"}, TimeCreated: now, TimeUpdated: now},
		{ID: "trn_2", SessionID: "ses_1", State: schema.TurnStateCompleted,
			Metadata: map[string]any{"a2a.context_id": "ctx-2", "sdk_agent_id": "build"}, TimeCreated: now, TimeUpdated: now},
		{ID: "trn_3", SessionID: "ses_1", State: schema.TurnStateRunning,
			Metadata: map[string]any{"a2a.context_id": "ctx-1", "sdk_agent_id": "other"}, TimeCreated: now, TimeUpdated: now},
	}
	for _, turn := range turns {
		if err := store.Create(turn); err != nil {
			t.Fatalf("create %s: %v", turn.ID, err)
		}
	}

	// A dotted key must match the flat metadata key exactly.
	found, err := store.FindByMetadata("a2a.context_id", "ctx-1")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("ctx-1 matches = %d, want 2", len(found))
	}

	missing, err := store.FindByMetadata("a2a.context_id", "nope")
	if err != nil {
		t.Fatalf("find missing: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("nope matches = %d, want 0", len(missing))
	}
}
