package session

import (
	"encoding/json"

	"github.com/user/go-agent-sdk/schema"
)

// MemoryState is the in-memory view of a session's messages.
type MemoryState struct {
	Messages []schema.SessionMessage
}

// Adapter applies session events to a message store.
type Adapter interface {
	GetCurrentAssistant() (*schema.SessionMessageAssistant, bool)
	GetAssistant(messageID schema.MessageID) (*schema.SessionMessageAssistant, bool)
	GetCurrentShell(callID string) (*schema.SessionMessageShell, bool)
	UpdateAssistant(assistant schema.SessionMessageAssistant) error
	UpdateShell(shell schema.SessionMessageShell) error
	AppendMessage(message schema.SessionMessage) error
}

func sessionMessageID(message schema.SessionMessage) schema.MessageID {
	switch m := message.(type) {
	case schema.SessionMessageAgentSwitched:
		return m.ID
	case schema.SessionMessageModelSwitched:
		return m.ID
	case schema.SessionMessageUser:
		return m.ID
	case schema.SessionMessageSynthetic:
		return m.ID
	case schema.SessionMessageSystem:
		return m.ID
	case schema.SessionMessageShell:
		return m.ID
	case schema.SessionMessageAssistant:
		return m.ID
	case schema.SessionMessageCompaction:
		return m.ID
	}
	return ""
}

// Memory returns the in-memory adapter.
func Memory(state *MemoryState) Adapter {
	assistantIndex := func(messageID schema.MessageID) int {
		for i := len(state.Messages) - 1; i >= 0; i-- {
			if sessionMessageID(state.Messages[i]) == messageID {
				return i
			}
		}
		return -1
	}
	latestAssistantIndex := func() int {
		for i := len(state.Messages) - 1; i >= 0; i-- {
			if state.Messages[i].SessionMessageType() == "assistant" {
				return i
			}
		}
		return -1
	}
	activeShellIndex := func(callID string) int {
		for i := len(state.Messages) - 1; i >= 0; i-- {
			if shell, ok := state.Messages[i].(schema.SessionMessageShell); ok && shell.CallID == callID {
				return i
			}
		}
		return -1
	}
	return &memoryAdapter{
		state:                state,
		assistantIndex:       assistantIndex,
		latestAssistantIndex: latestAssistantIndex,
		activeShellIndex:     activeShellIndex,
	}
}

type memoryAdapter struct {
	state                *MemoryState
	assistantIndex       func(schema.MessageID) int
	latestAssistantIndex func() int
	activeShellIndex     func(string) int
}

func (a *memoryAdapter) GetCurrentAssistant() (*schema.SessionMessageAssistant, bool) {
	index := a.latestAssistantIndex()
	if index < 0 {
		return nil, false
	}
	assistant, ok := a.state.Messages[index].(schema.SessionMessageAssistant)
	if !ok || assistant.Time.Completed != nil {
		return nil, false
	}
	return &assistant, true
}

func (a *memoryAdapter) GetAssistant(messageID schema.MessageID) (*schema.SessionMessageAssistant, bool) {
	index := a.assistantIndex(messageID)
	if index < 0 {
		return nil, false
	}
	assistant, ok := a.state.Messages[index].(schema.SessionMessageAssistant)
	if !ok {
		return nil, false
	}
	return &assistant, true
}

func (a *memoryAdapter) GetCurrentShell(callID string) (*schema.SessionMessageShell, bool) {
	index := a.activeShellIndex(callID)
	if index < 0 {
		return nil, false
	}
	shell, ok := a.state.Messages[index].(schema.SessionMessageShell)
	if !ok {
		return nil, false
	}
	return &shell, true
}

func (a *memoryAdapter) UpdateAssistant(assistant schema.SessionMessageAssistant) error {
	index := a.assistantIndex(assistant.ID)
	if index < 0 {
		return nil
	}
	if _, ok := a.state.Messages[index].(schema.SessionMessageAssistant); !ok {
		return nil
	}
	a.state.Messages[index] = assistant
	return nil
}

func (a *memoryAdapter) UpdateShell(shell schema.SessionMessageShell) error {
	index := a.activeShellIndex(shell.CallID)
	if index < 0 {
		return nil
	}
	if _, ok := a.state.Messages[index].(schema.SessionMessageShell); !ok {
		return nil
	}
	a.state.Messages[index] = shell
	return nil
}

func (a *memoryAdapter) AppendMessage(message schema.SessionMessage) error {
	a.state.Messages = append(a.state.Messages, message)
	return nil
}

func decodeEventData[T any](data any) (T, error) {
	var result T
	encoded, err := json.Marshal(data)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return result, err
	}
	return result, nil
}

func latestTextIndex(assistant *schema.SessionMessageAssistant, textID string) int {
	for i := len(assistant.Content) - 1; i >= 0; i-- {
		if text, ok := assistant.Content[i].(schema.SessionMessageAssistantText); ok && text.ID == textID {
			return i
		}
	}
	return -1
}

func latestReasoningIndex(assistant *schema.SessionMessageAssistant, reasoningID string) int {
	for i := len(assistant.Content) - 1; i >= 0; i-- {
		if reasoning, ok := assistant.Content[i].(schema.SessionMessageAssistantReasoning); ok && reasoning.ID == reasoningID {
			return i
		}
	}
	return -1
}

func latestToolIndex(assistant *schema.SessionMessageAssistant, callID string) int {
	for i := len(assistant.Content) - 1; i >= 0; i-- {
		if tool, ok := assistant.Content[i].(schema.SessionMessageAssistantTool); ok {
			if callID == "" || tool.ID == callID {
				return i
			}
		}
	}
	return -1
}

func updateOwnedAssistant(adapter Adapter, messageID schema.MessageID, recipe func(*schema.SessionMessageAssistant)) error {
	assistant, ok := adapter.GetAssistant(messageID)
	if !ok {
		return nil
	}
	recipe(assistant)
	return adapter.UpdateAssistant(*assistant)
}

// Update applies one event through the adapter.
func Update(adapter Adapter, event schema.EventPayload) error {
	switch event.Type {
	case schema.SessionEventAgentSwitched.Type:
		data, err := decodeEventData[schema.SessionEventAgentSwitchedData](event.Data)
		if err != nil {
			return err
		}
		return adapter.AppendMessage(schema.SessionMessageAgentSwitched{
			ID: data.MessageID, Metadata: event.Metadata, Time: schema.SessionMessageTime{Created: data.Timestamp},
			Type: "agent-switched", Agent: data.Agent,
		})

	case schema.SessionEventModelSwitched.Type:
		data, err := decodeEventData[schema.SessionEventModelSwitchedData](event.Data)
		if err != nil {
			return err
		}
		return adapter.AppendMessage(schema.SessionMessageModelSwitched{
			ID: data.MessageID, Metadata: event.Metadata, Time: schema.SessionMessageTime{Created: data.Timestamp},
			Type: "model-switched", Model: data.Model,
		})

	case schema.SessionEventMoved.Type, schema.SessionEventPromptAdmitted.Type,
		schema.SessionEventToolInputDelta.Type, schema.SessionEventRetried.Type,
		schema.SessionEventCompactionStarted.Type, schema.SessionEventCompactionDelta.Type,
		schema.SessionEventRevertStaged.Type, schema.SessionEventRevertCleared.Type,
		schema.SessionEventRevertCommitted.Type:
		return nil

	case schema.SessionEventPrompted.Type:
		data, err := decodeEventData[schema.SessionEventPromptData](event.Data)
		if err != nil {
			return err
		}
		return adapter.AppendMessage(schema.SessionMessageUser{
			ID: data.MessageID, Metadata: event.Metadata, Time: schema.SessionMessageTime{Created: data.Timestamp},
			Type: "user", Text: data.Prompt.Text, Files: data.Prompt.Files, Agents: data.Prompt.Agents,
		})

	case schema.SessionEventContextUpdated.Type:
		data, err := decodeEventData[schema.SessionEventMessageTextData](event.Data)
		if err != nil {
			return err
		}
		return adapter.AppendMessage(schema.SessionMessageSystem{
			ID: data.MessageID, Time: schema.SessionMessageTime{Created: data.Timestamp}, Type: "system", Text: data.Text,
		})

	case schema.SessionEventSynthetic.Type:
		data, err := decodeEventData[schema.SessionEventMessageTextData](event.Data)
		if err != nil {
			return err
		}
		return adapter.AppendMessage(schema.SessionMessageSynthetic{
			ID: data.MessageID, Time: schema.SessionMessageTime{Created: data.Timestamp}, Type: "synthetic",
			SessionID: data.SessionID, Text: data.Text,
		})

	case schema.SessionEventShellStarted.Type:
		data, err := decodeEventData[schema.SessionEventShellStartedData](event.Data)
		if err != nil {
			return err
		}
		return adapter.AppendMessage(schema.SessionMessageShell{
			ID: data.MessageID, Metadata: event.Metadata, Type: "shell", CallID: data.CallID, Command: data.Command,
			Output: "", Time: schema.SessionMessageTimeRange{Created: data.Timestamp},
		})

	case schema.SessionEventShellEnded.Type:
		data, err := decodeEventData[schema.SessionEventShellEndedData](event.Data)
		if err != nil {
			return err
		}
		shell, ok := adapter.GetCurrentShell(data.CallID)
		if !ok {
			return nil
		}
		shell.Output = data.Output
		completed := data.Timestamp
		shell.Time.Completed = &completed
		return adapter.UpdateShell(*shell)

	case schema.SessionEventStepStarted.Type:
		data, err := decodeEventData[schema.SessionEventStepStartedData](event.Data)
		if err != nil {
			return err
		}
		if current, ok := adapter.GetCurrentAssistant(); ok {
			completed := data.Timestamp
			current.Time.Completed = &completed
			if err := adapter.UpdateAssistant(*current); err != nil {
				return err
			}
		}
		var snapshot *schema.SessionMessageAssistantSnapshot
		if data.Snapshot != nil {
			snapshot = &schema.SessionMessageAssistantSnapshot{Start: data.Snapshot}
		}
		return adapter.AppendMessage(schema.SessionMessageAssistant{
			ID: data.AssistantMessageID, Time: schema.SessionMessageTimeRange{Created: data.Timestamp},
			Type: "assistant", Agent: data.Agent, Model: data.Model,
			Content: []schema.SessionMessageAssistantContent{}, Snapshot: snapshot,
		})

	case schema.SessionEventStepEnded.Type:
		data, err := decodeEventData[schema.SessionEventStepEndedData](event.Data)
		if err != nil {
			return err
		}
		return updateOwnedAssistant(adapter, data.AssistantMessageID, func(draft *schema.SessionMessageAssistant) {
			completed := data.Timestamp
			draft.Time.Completed = &completed
			finish := data.Finish
			draft.Finish = &finish
			cost := data.Cost
			draft.Cost = &cost
			tokens := data.Tokens
			draft.Tokens = &tokens
			if data.Snapshot != nil || data.Files != nil {
				snapshot := draft.Snapshot
				if snapshot == nil {
					snapshot = &schema.SessionMessageAssistantSnapshot{}
				}
				snapshot.End = data.Snapshot
				snapshot.Files = data.Files
				draft.Snapshot = snapshot
			}
		})

	case schema.SessionEventStepFailed.Type:
		data, err := decodeEventData[schema.SessionEventStepFailedData](event.Data)
		if err != nil {
			return err
		}
		return updateOwnedAssistant(adapter, data.AssistantMessageID, func(draft *schema.SessionMessageAssistant) {
			completed := data.Timestamp
			draft.Time.Completed = &completed
			finish := "error"
			draft.Finish = &finish
			stepError := data.Error
			draft.Error = &stepError
		})

	case schema.SessionEventTextStarted.Type:
		data, err := decodeEventData[schema.SessionEventTextStartedData](event.Data)
		if err != nil {
			return err
		}
		return updateOwnedAssistant(adapter, data.AssistantMessageID, func(draft *schema.SessionMessageAssistant) {
			draft.Content = append(draft.Content, schema.SessionMessageAssistantText{Type: "text", ID: data.TextID, Text: ""})
		})

	case schema.SessionEventTextDelta.Type:
		data, err := decodeEventData[schema.SessionEventTextDeltaData](event.Data)
		if err != nil {
			return err
		}
		return updateOwnedAssistant(adapter, data.AssistantMessageID, func(draft *schema.SessionMessageAssistant) {
			if index := latestTextIndex(draft, data.TextID); index >= 0 {
				text := draft.Content[index].(schema.SessionMessageAssistantText)
				text.Text += data.Delta
				draft.Content[index] = text
			}
		})

	case schema.SessionEventTextEnded.Type:
		data, err := decodeEventData[schema.SessionEventTextEndedData](event.Data)
		if err != nil {
			return err
		}
		return updateOwnedAssistant(adapter, data.AssistantMessageID, func(draft *schema.SessionMessageAssistant) {
			if index := latestTextIndex(draft, data.TextID); index >= 0 {
				text := draft.Content[index].(schema.SessionMessageAssistantText)
				text.Text = data.Text
				draft.Content[index] = text
			}
		})

	case schema.SessionEventToolInputStarted.Type:
		data, err := decodeEventData[schema.SessionEventToolInputStartedData](event.Data)
		if err != nil {
			return err
		}
		return updateOwnedAssistant(adapter, data.AssistantMessageID, func(draft *schema.SessionMessageAssistant) {
			draft.Content = append(draft.Content, schema.SessionMessageAssistantTool{
				Type: "tool", ID: data.CallID, Name: data.Name,
				Time:  schema.SessionMessageAssistantToolTime{Created: data.Timestamp},
				State: schema.SessionMessageToolStatePending{Status: "pending", Input: ""},
			})
		})

	case schema.SessionEventToolInputEnded.Type:
		data, err := decodeEventData[schema.SessionEventToolInputEndedData](event.Data)
		if err != nil {
			return err
		}
		return updateOwnedAssistant(adapter, data.AssistantMessageID, func(draft *schema.SessionMessageAssistant) {
			if index := latestToolIndex(draft, data.CallID); index >= 0 {
				tool := draft.Content[index].(schema.SessionMessageAssistantTool)
				if pending, ok := tool.State.(schema.SessionMessageToolStatePending); ok {
					pending.Input = data.Text
					tool.State = pending
					draft.Content[index] = tool
				}
			}
		})

	case schema.SessionEventToolCalled.Type:
		data, err := decodeEventData[schema.SessionEventToolCalledData](event.Data)
		if err != nil {
			return err
		}
		return updateOwnedAssistant(adapter, data.AssistantMessageID, func(draft *schema.SessionMessageAssistant) {
			if index := latestToolIndex(draft, data.CallID); index >= 0 {
				tool := draft.Content[index].(schema.SessionMessageAssistantTool)
				tool.Provider = &schema.SessionMessageAssistantProvider{
					Executed: data.Provider.Executed, Metadata: data.Provider.Metadata,
				}
				ran := data.Timestamp
				tool.Time.Ran = &ran
				tool.State = schema.SessionMessageToolStateRunning{
					Status: "running", Input: data.Input, Structured: map[string]any{}, Content: []schema.ToolContent{},
				}
				draft.Content[index] = tool
			}
		})

	case schema.SessionEventToolProgress.Type:
		data, err := decodeEventData[schema.SessionEventToolProgressData](event.Data)
		if err != nil {
			return err
		}
		return updateOwnedAssistant(adapter, data.AssistantMessageID, func(draft *schema.SessionMessageAssistant) {
			if index := latestToolIndex(draft, data.CallID); index >= 0 {
				tool := draft.Content[index].(schema.SessionMessageAssistantTool)
				if running, ok := tool.State.(schema.SessionMessageToolStateRunning); ok {
					running.Structured = data.Structured
					running.Content = data.Content
					tool.State = running
					draft.Content[index] = tool
				}
			}
		})

	case schema.SessionEventToolSuccess.Type:
		data, err := decodeEventData[schema.SessionEventToolSuccessData](event.Data)
		if err != nil {
			return err
		}
		return updateOwnedAssistant(adapter, data.AssistantMessageID, func(draft *schema.SessionMessageAssistant) {
			if index := latestToolIndex(draft, data.CallID); index >= 0 {
				tool := draft.Content[index].(schema.SessionMessageAssistantTool)
				running, ok := tool.State.(schema.SessionMessageToolStateRunning)
				if !ok {
					return
				}
				executed := data.Provider.Executed
				var previousMetadata *schema.ProviderMetadata
				if tool.Provider != nil {
					if tool.Provider.Executed {
						executed = true
					}
					previousMetadata = tool.Provider.Metadata
				}
				tool.Provider = &schema.SessionMessageAssistantProvider{
					Executed: executed, Metadata: previousMetadata, ResultMetadata: data.Provider.Metadata,
				}
				completed := data.Timestamp
				tool.Time.Completed = &completed
				outputPaths := data.OutputPaths
				if outputPaths == nil {
					outputPaths = []string{}
				}
				tool.State = schema.SessionMessageToolStateCompleted{
					Status: "completed", Input: running.Input, Structured: data.Structured, Content: data.Content,
					OutputPaths: outputPaths, Result: data.Result,
				}
				draft.Content[index] = tool
			}
		})

	case schema.SessionEventToolFailed.Type:
		data, err := decodeEventData[schema.SessionEventToolFailedData](event.Data)
		if err != nil {
			return err
		}
		return updateOwnedAssistant(adapter, data.AssistantMessageID, func(draft *schema.SessionMessageAssistant) {
			if index := latestToolIndex(draft, data.CallID); index >= 0 {
				tool := draft.Content[index].(schema.SessionMessageAssistantTool)
				executed := data.Provider.Executed
				var previousMetadata *schema.ProviderMetadata
				if tool.Provider != nil {
					if tool.Provider.Executed {
						executed = true
					}
					previousMetadata = tool.Provider.Metadata
				}
				stateError := schema.SessionMessageToolStateError{
					Status: "error", Error: data.Error, Result: data.Result,
					Input: map[string]any{}, Structured: map[string]any{}, Content: []schema.ToolContent{},
				}
				switch state := tool.State.(type) {
				case schema.SessionMessageToolStatePending:
					// pending input is a raw string; the error state uses an object.
				case schema.SessionMessageToolStateRunning:
					stateError.Input = state.Input
					stateError.Structured = state.Structured
					stateError.Content = state.Content
				default:
					return
				}
				tool.Provider = &schema.SessionMessageAssistantProvider{
					Executed: executed, Metadata: previousMetadata, ResultMetadata: data.Provider.Metadata,
				}
				completed := data.Timestamp
				tool.Time.Completed = &completed
				tool.State = stateError
				draft.Content[index] = tool
			}
		})

	case schema.SessionEventReasoningStarted.Type:
		data, err := decodeEventData[schema.SessionEventReasoningStartedData](event.Data)
		if err != nil {
			return err
		}
		return updateOwnedAssistant(adapter, data.AssistantMessageID, func(draft *schema.SessionMessageAssistant) {
			draft.Content = append(draft.Content, schema.SessionMessageAssistantReasoning{
				Type: "reasoning", ID: data.ReasoningID, Text: "", ProviderMetadata: data.ProviderMetadata,
				Time: &schema.SessionMessageTimeRange{Created: data.Timestamp},
			})
		})

	case schema.SessionEventReasoningDelta.Type:
		data, err := decodeEventData[schema.SessionEventReasoningDeltaData](event.Data)
		if err != nil {
			return err
		}
		return updateOwnedAssistant(adapter, data.AssistantMessageID, func(draft *schema.SessionMessageAssistant) {
			if index := latestReasoningIndex(draft, data.ReasoningID); index >= 0 {
				reasoning := draft.Content[index].(schema.SessionMessageAssistantReasoning)
				reasoning.Text += data.Delta
				draft.Content[index] = reasoning
			}
		})

	case schema.SessionEventReasoningEnded.Type:
		data, err := decodeEventData[schema.SessionEventReasoningEndedData](event.Data)
		if err != nil {
			return err
		}
		return updateOwnedAssistant(adapter, data.AssistantMessageID, func(draft *schema.SessionMessageAssistant) {
			if index := latestReasoningIndex(draft, data.ReasoningID); index >= 0 {
				reasoning := draft.Content[index].(schema.SessionMessageAssistantReasoning)
				reasoning.Text = data.Text
				created := data.Timestamp
				if reasoning.Time != nil {
					created = reasoning.Time.Created
				}
				completed := data.Timestamp
				reasoning.Time = &schema.SessionMessageTimeRange{Created: created, Completed: &completed}
				if data.ProviderMetadata != nil {
					reasoning.ProviderMetadata = data.ProviderMetadata
				}
				draft.Content[index] = reasoning
			}
		})

	case schema.SessionEventCompactionEnded.Type:
		data, err := decodeEventData[schema.SessionEventCompactionEndedData](event.Data)
		if err != nil {
			return err
		}
		return adapter.AppendMessage(schema.SessionMessageCompaction{
			ID: data.MessageID, Metadata: event.Metadata, Time: schema.SessionMessageTime{Created: data.Timestamp},
			Type: "compaction", Reason: data.Reason, Summary: data.Text, Recent: data.Recent,
		})
	}
	return nil
}
