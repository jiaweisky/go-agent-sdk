package schema

import (
	"encoding/json"
	"fmt"
)

// ProviderMetadata mirrors LLM.ProviderMetadata.
type ProviderMetadata = map[string]map[string]any

type ToolTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (ToolTextContent) ToolContentType() string { return "text" }

type ToolFileContent struct {
	Type string  `json:"type"`
	URI  string  `json:"uri"`
	Mime string  `json:"mime"`
	Name *string `json:"name,omitempty"`
}

func (ToolFileContent) ToolContentType() string { return "file" }

// ToolContent mirrors the tagged LLM.ToolContent union (text | file).
type ToolContent interface {
	ToolContentType() string
}

// ToolContents is a JSON-decodable slice of ToolContent, dispatching on the
// "type" discriminator.
type ToolContents []ToolContent

func (c *ToolContents) UnmarshalJSON(data []byte) error {
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}
	result := make(ToolContents, 0, len(items))
	for _, item := range items {
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(item, &probe); err != nil {
			return err
		}
		switch probe.Type {
		case "text":
			var value ToolTextContent
			if err := json.Unmarshal(item, &value); err != nil {
				return err
			}
			result = append(result, value)
		case "file":
			var value ToolFileContent
			if err := json.Unmarshal(item, &value); err != nil {
				return err
			}
			result = append(result, value)
		default:
			return fmt.Errorf("unknown tool content type: %s", probe.Type)
		}
	}
	*c = result
	return nil
}
