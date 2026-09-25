package schema

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// DecodeSessionMessage decodes a session message payload using its embedded
// "type" discriminator.
func DecodeSessionMessage(data []byte) (SessionMessage, error) {
	object, err := decodeJSONObject(data)
	if err != nil {
		return nil, err
	}
	idRaw, err := requiredJSONField(object, "id")
	if err != nil {
		return nil, err
	}
	typeRaw, err := requiredJSONField(object, "type")
	if err != nil {
		return nil, err
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
		return decodeSessionMessageAs(data, func(m *SessionMessageAgentSwitched) { m.ID, m.Type = id, messageType })
	case "model-switched":
		return decodeSessionMessageAs(data, func(m *SessionMessageModelSwitched) { m.ID, m.Type = id, messageType })
	case "user":
		return decodeSessionMessageAs(data, func(m *SessionMessageUser) { m.ID, m.Type = id, messageType })
	case "synthetic":
		return decodeSessionMessageAs(data, func(m *SessionMessageSynthetic) { m.ID, m.Type = id, messageType })
	case "system":
		return decodeSessionMessageAs(data, func(m *SessionMessageSystem) { m.ID, m.Type = id, messageType })
	case "shell":
		return decodeSessionMessageAs(data, func(m *SessionMessageShell) { m.ID, m.Type = id, messageType })
	case "assistant":
		return decodeSessionMessageAs(data, func(m *SessionMessageAssistant) { m.ID, m.Type = id, messageType })
	case "compaction":
		return decodeSessionMessageAs(data, func(m *SessionMessageCompaction) { m.ID, m.Type = id, messageType })
	}
	return nil, fmt.Errorf("unknown session message type: %s", messageType)
}

func decodeSessionMessageAs[T SessionMessage](data []byte, apply func(*T)) (SessionMessage, error) {
	var message T
	if err := json.Unmarshal(data, &message); err != nil {
		return nil, err
	}
	apply(&message)
	return message, nil
}

// DecodeSessionMessageAssistantContent decodes an Assistant.Content entry using
// its "type" discriminator.
func DecodeSessionMessageAssistantContent(data []byte) (SessionMessageAssistantContent, error) {
	object, err := decodeJSONObject(data)
	if err != nil {
		return nil, err
	}
	typeRaw, err := requiredJSONField(object, "type")
	if err != nil {
		return nil, err
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
	statusRaw, err := requiredJSONField(object, "status")
	if err != nil {
		return nil, err
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

// UnmarshalJSON decodes the assistant message including its tagged content union.
func (m *SessionMessageAssistant) UnmarshalJSON(data []byte) error {
	type assistantAlias SessionMessageAssistant
	var aux struct {
		assistantAlias
		Content []json.RawMessage `json:"content"`
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
	*m = SessionMessageAssistant(aux.assistantAlias)
	m.Content = content
	return nil
}

// UnmarshalJSON decodes the tool assistant content including its tagged state union.
func (m *SessionMessageAssistantTool) UnmarshalJSON(data []byte) error {
	type toolAlias SessionMessageAssistantTool
	var aux struct {
		toolAlias
		State json.RawMessage `json:"state"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	state, err := DecodeSessionMessageToolState(aux.State)
	if err != nil {
		return err
	}
	*m = SessionMessageAssistantTool(aux.toolAlias)
	m.State = state
	return nil
}
