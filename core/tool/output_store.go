package tool

import (
	"github.com/jiaweisky/go-agent-sdk/core"
	llmschema "github.com/jiaweisky/go-agent-sdk/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// coreOutputStore adapts core.ToolOutputStore to the OutputStore seam the
// registry uses to bound oversized tool results before they reach the model.
type coreOutputStore struct {
	store *core.ToolOutputStore
}

// NewCoreOutputStore wraps a core.ToolOutputStore as an OutputStore.
func NewCoreOutputStore(store *core.ToolOutputStore) OutputStore {
	return coreOutputStore{store: store}
}

func (a coreOutputStore) Bound(sessionID string, toolCallID string, output llmschema.ToolOutput) BoundResult {
	result, err := a.store.Bound(core.ToolOutputStoreBoundInput{
		SessionID:  schema.SessionID(sessionID),
		ToolCallID: toolCallID,
		Output:     output,
	})
	if err != nil {
		// Bounding is best-effort: on a storage failure keep the raw output
		// rather than failing the tool call.
		return BoundResult{Output: output}
	}
	return BoundResult{Output: result.Output, OutputPaths: result.OutputPaths}
}
