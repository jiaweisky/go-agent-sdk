package agent

import (
	"fmt"

	"github.com/user/go-agent-sdk/llm/protocols"
	llmschema "github.com/user/go-agent-sdk/llm/schema"
	"github.com/user/go-agent-sdk/schema"
)

// NewModelInfo builds a ModelInfo for a registered protocol id, for
// example "openai-responses", "anthropic-messages" or "gemini". baseURL is
// optional: an empty value uses the route's default endpoint. The ProviderID is
// set to the protocol id, so ClientOptions.Credentials is called with it. Hand
// the result to ClientOptions.ModelInfo.
func NewModelInfo(protocol schema.ProtocolID, modelID, baseURL string, limit schema.ModelLimit) (schema.ModelInfo, error) {
	if _, ok := protocols.LookupProtocol(llmschema.ProtocolID(protocol)); !ok {
		return schema.ModelInfo{}, fmt.Errorf("unknown protocol %q", protocol)
	}
	info := schema.ModelInfoEmpty(schema.ProviderID(protocol), schema.ModelID(modelID))
	info.API.Protocol = protocol
	if baseURL != "" {
		value := baseURL
		info.API.URL = &value
	}
	info.Limit = limit
	return info, nil
}
