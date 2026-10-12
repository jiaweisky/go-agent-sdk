package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

func TestModelListReportsLimitsAndDefault(t *testing.T) {
	primary := schema.ModelInfo{
		ID:           "m1",
		API:          schema.ModelAPI{Protocol: "openai-responses"},
		Capabilities: schema.ModelCapabilities{Input: []string{"text"}},
		Limit:        schema.ModelLimit{Context: 1000, Output: 200},
	}
	other := schema.ModelInfo{
		ID:  "m2",
		API: schema.ModelAPI{Protocol: "openai-responses"},
	}
	ts := newTestServer(t, Options{Models: []schema.ModelInfo{primary, other}, DefaultModel: "m1"})

	resp := doJSON(t, http.MethodGet, ts.URL+"/api/model", "", nil)
	defer resp.Body.Close()
	var list struct {
		Data []modelInfoDTO `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Data) != 2 {
		t.Fatalf("models = %+v, want 2", list.Data)
	}
	if list.Data[0].ContextLimit != 1000 || list.Data[0].OutputLimit != 200 {
		t.Fatalf("limits = %+v, want context 1000 output 200", list.Data[0])
	}
	if !list.Data[0].Default || list.Data[1].Default {
		t.Fatalf("default flags = %v/%v, want true/false", list.Data[0].Default, list.Data[1].Default)
	}
}

func TestAgentListMarksDefault(t *testing.T) {
	ts := newTestServer(t, Options{Agents: []string{"plan"}})

	resp := doJSON(t, http.MethodGet, ts.URL+"/api/agent", "", nil)
	defer resp.Body.Close()
	var list struct {
		Data []agentInfoDTO `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Data) != 1 || list.Data[0].ID != "plan" {
		t.Fatalf("agents = %+v, want only plan", list.Data)
	}
	if !list.Data[0].Default {
		t.Fatal("the allow-list's first agent should be marked default")
	}
}

func TestCreateSessionUsesDefaultModel(t *testing.T) {
	ts := newTestServer(t, Options{DefaultModel: "test-model"})

	resp := doJSON(t, http.MethodPost, ts.URL+"/api/session", `{"agent":"build"}`, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	var out struct {
		Data sessionInfoDTO `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	modelRef, ok := out.Data.Model.(map[string]any)
	if !ok {
		t.Fatalf("session model = %#v, want a model reference", out.Data.Model)
	}
	if modelRef["id"] != "test-model" {
		t.Fatalf("session model id = %v, want test-model", modelRef["id"])
	}
}
