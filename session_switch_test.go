package agent

import (
	"context"
	"testing"

	"github.com/user/go-agent-sdk/schema"
)

// TestSetModelUpdatesSession guards that SetModel switches the stored model of
// the current session (no new session) and is a no-op for the same model.
func TestSetModelUpdatesSession(t *testing.T) {
	client := newTestClient(t)
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	if err := session.SetModel(schema.ModelRef{ID: "m2"}); err != nil {
		t.Fatalf("set model: %v", err)
	}
	stored, err := client.rt.store.Get(session.id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Model == nil || stored.Model.ID != "m2" {
		t.Fatalf("stored model = %+v, want m2", stored.Model)
	}

	// Setting the same model again is a no-op.
	if err := session.SetModel(schema.ModelRef{ID: "m2", ProviderID: stored.Model.ProviderID}); err != nil {
		t.Fatalf("set same model: %v", err)
	}
}

// TestSetAgentUpdatesSession guards that SetAgent switches the stored agent of
// the current session.
func TestSetAgentUpdatesSession(t *testing.T) {
	client := newTestClient(t)
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	if err := session.SetAgent("plan"); err != nil {
		t.Fatalf("set agent: %v", err)
	}
	stored, err := client.rt.store.Get(session.id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Agent == nil || *stored.Agent != "plan" {
		t.Fatalf("stored agent = %v, want plan", stored.Agent)
	}
}
