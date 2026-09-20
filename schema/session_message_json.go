package schema

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// DecodeSessionMessage decodes a Session.Message payload using its embedded
// "type" discriminator.
func DecodeSessionMessage(data []byte) (SessionMessage, error) {
	object, err := decodeJSONObject(data)
	if err != nil {
		return nil, err
	}
	idRaw, ok := object["id"]
	if !ok || isJSONNull(idRaw) {
		return nil, fmt.Errorf("missing required field %q", "id")
	}
	typeRaw, ok := object["type"]
	if !ok || isJSONNull(typeRaw) {
		return nil, fmt.Errorf("missing required field %q", "type")
	}
	var id MessageID
	if err := json.Unmarshal(idRaw, &id); err != nil {
		return nil, err
	}
	var messageType string
	if err := json.Unmarshal(typeRaw, &messageType); err != nil {
		return nil, err
	}
	message, err := decodeSessionMessageByType(data, messageType, id)
	if err != nil {
		return nil, err
	}
	if err := validateRequiredJSON(reflect.TypeOf(message), object, nil); err != nil {
		return nil, err
	}
	return message, nil
}

// DecodeSessionMessageWithMeta decodes a stored message whose id and type live
// in separate columns (the data payload omits them), then sets them.
func DecodeSessionMessageWithMeta(data []byte, id MessageID, messageType string) (SessionMessage, error) {
	message, err := decodeSessionMessageByType(data, messageType, id)
	if err != nil {
		return nil, err
	}
	object, err := decodeJSONObject(data)
	if err != nil {
		return nil, err
	}
	if err := validateRequiredJSON(reflect.TypeOf(message), object, map[string]bool{"id": true, "type": true}); err != nil {
		return nil, err
	}
	return message, nil
}

func decodeSessionMessageByType(data []byte, messageType string, id MessageID) (SessionMessage, error) {
	switch messageType {
	case "agent-switched":
		var message SessionMessageAgentSwitched
		if err := json.Unmarshal(data, &message); err != nil {
			return nil, err
		}
		message.ID, message.Type = id, messageType
		return message, nil
	case "model-switched":
		var message SessionMessageModelSwitched
		if err := json.Unmarshal(data, &message); err != nil {
			return nil, err
		}
		message.ID, message.Type = id, messageType
		return message, nil
	case "user":
		var message SessionMessageUser
		if err := json.Unmarshal(data, &message); err != nil {
			return nil, err
		}
		message.ID, message.Type = id, messageType
		return message, nil
	case "synthetic":
		var message SessionMessageSynthetic
		if err := json.Unmarshal(data, &message); err != nil {
			return nil, err
		}
		message.ID, message.Type = id, messageType
		return message, nil
	case "system":
		var message SessionMessageSystem
		if err := json.Unmarshal(data, &message); err != nil {
			return nil, err
		}
		message.ID, message.Type = id, messageType
		return message, nil
	case "shell":
		var message SessionMessageShell
		if err := json.Unmarshal(data, &message); err != nil {
			return nil, err
		}
		message.ID, message.Type = id, messageType
		return message, nil
	case "assistant":
		var message SessionMessageAssistant
		if err := json.Unmarshal(data, &message); err != nil {
			return nil, err
		}
		message.ID, message.Type = id, messageType
		return message, nil
	case "compaction":
		var message SessionMessageCompaction
		if err := json.Unmarshal(data, &message); err != nil {
			return nil, err
		}
		message.ID, message.Type = id, messageType
		return message, nil
	}
	return nil, fmt.Errorf("unknown session message type: %s", messageType)
}

// DecodeSessionMessageAssistantContent decodes an Assistant.Content entry using
// its "type" discriminator.
func DecodeSessionMessageAssistantContent(data []byte) (SessionMessageAssistantContent, error) {
	object, err := decodeJSONObject(data)
	if err != nil {
		return nil, err
	}
	typeRaw, ok := object["type"]
	if !ok || isJSONNull(typeRaw) {
		return nil, fmt.Errorf("missing required field %q", "type")
	}
	var contentType string
	if err := json.Unmarshal(typeRaw, &contentType); err != nil {
		return nil, err
	}
	var content SessionMessageAssistantContent
	switch contentType {
	case "text":
		content, err = decodeAssistantContent[SessionMessageAssistantText](data)
	case "reasoning":
		content, err = decodeAssistantContent[SessionMessageAssistantReasoning](data)
	case "tool":
		content, err = decodeAssistantContent[SessionMessageAssistantTool](data)
	default:
		return nil, fmt.Errorf("unknown assistant content type: %s", contentType)
	}
	if err != nil {
		return nil, err
	}
	if err := validateRequiredJSON(reflect.TypeOf(content), object, nil); err != nil {
		return nil, err
	}
	return content, nil
}

func decodeAssistantContent[T SessionMessageAssistantContent](data []byte) (SessionMessageAssistantContent, error) {
	var content T
	if err := json.Unmarshal(data, &content); err != nil {
		return nil, err
	}
	return content, nil
}

// DecodeSessionMessageToolState decodes a ToolState using its "status" discriminator.
func DecodeSessionMessageToolState(data []byte) (SessionMessageToolState, error) {
	object, err := decodeJSONObject(data)
	if err != nil {
		return nil, err
	}
	statusRaw, ok := object["status"]
	if !ok || isJSONNull(statusRaw) {
		return nil, fmt.Errorf("missing required field %q", "status")
	}
	var status string
	if err := json.Unmarshal(statusRaw, &status); err != nil {
		return nil, err
	}
	var state SessionMessageToolState
	switch status {
	case "pending":
		state, err = decodeToolState[SessionMessageToolStatePending](data)
	case "running":
		state, err = decodeToolState[SessionMessageToolStateRunning](data)
	case "completed":
		state, err = decodeToolState[SessionMessageToolStateCompleted](data)
	case "error":
		state, err = decodeToolState[SessionMessageToolStateError](data)
	default:
		return nil, fmt.Errorf("unknown tool state status: %s", status)
	}
	if err != nil {
		return nil, err
	}
	if err := validateRequiredJSON(reflect.TypeOf(state), object, nil); err != nil {
		return nil, err
	}
	return state, nil
}

func decodeToolState[T SessionMessageToolState](data []byte) (SessionMessageToolState, error) {
	var state T
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return state, nil
}

// UnmarshalJSON decodes the Assistant message including its tagged content union.
func (m *SessionMessageAssistant) UnmarshalJSON(data []byte) error {
	var aux struct {
		ID       MessageID                        `json:"id"`
		Metadata map[string]any                   `json:"metadata,omitempty"`
		Time     SessionMessageTimeRange          `json:"time"`
		Type     string                           `json:"type"`
		Agent    string                           `json:"agent"`
		Model    ModelRef                         `json:"model"`
		Content  []json.RawMessage                `json:"content"`
		Snapshot *SessionMessageAssistantSnapshot `json:"snapshot,omitempty"`
		Finish   *string                          `json:"finish,omitempty"`
		Cost     *float64                         `json:"cost,omitempty"`
		Tokens   *SessionMessageTokens            `json:"tokens,omitempty"`
		Error    *SessionMessageUnknownError      `json:"error,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	content := make([]SessionMessageAssistantContent, 0, len(aux.Content))
	for _, raw := range aux.Content {
		item, err := DecodeSessionMessageAssistantContent(raw)
		if err != nil {
			return err
		}
		content = append(content, item)
	}
	*m = SessionMessageAssistant{
		ID:       aux.ID,
		Metadata: aux.Metadata,
		Time:     aux.Time,
		Type:     aux.Type,
		Agent:    aux.Agent,
		Model:    aux.Model,
		Content:  content,
		Snapshot: aux.Snapshot,
		Finish:   aux.Finish,
		Cost:     aux.Cost,
		Tokens:   aux.Tokens,
		Error:    aux.Error,
	}
	return nil
}

// UnmarshalJSON decodes the tool assistant content including its tagged state union.
func (m *SessionMessageAssistantTool) UnmarshalJSON(data []byte) error {
	var aux struct {
		Type     string                           `json:"type"`
		ID       string                           `json:"id"`
		Name     string                           `json:"name"`
		Provider *SessionMessageAssistantProvider `json:"provider,omitempty"`
		State    json.RawMessage                  `json:"state"`
		Time     SessionMessageAssistantToolTime  `json:"time"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	state, err := DecodeSessionMessageToolState(aux.State)
	if err != nil {
		return err
	}
	*m = SessionMessageAssistantTool{
		Type:     aux.Type,
		ID:       aux.ID,
		Name:     aux.Name,
		Provider: aux.Provider,
		State:    state,
		Time:     aux.Time,
	}
	return nil
}
