package core

import (
	"strings"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

// ModelV2 re-exports the shared model schema.
type (
	ModelID           = schema.ModelID
	ModelVariantID    = schema.ModelVariantID
	ModelFamily       = schema.ModelFamily
	ModelRef          = schema.ModelRef
	ModelCapabilities = schema.ModelCapabilities
	ModelCost         = schema.ModelCost
	ModelAPI          = schema.ModelAPI
	ModelInfo         = schema.ModelInfo
)

// ModelRefParts is the result of ParseModel.
type ModelRefParts struct {
	ProviderID ProviderID
	ModelID    ModelID
}

// ParseModel splits a "provider/model" reference on the first slash, treating the
// remainder as the model ID.
func ParseModel(input string) ModelRefParts {
	parts := strings.SplitN(input, "/", 2)
	if len(parts) == 1 {
		return ModelRefParts{ProviderID: ProviderID(parts[0])}
	}
	return ModelRefParts{ProviderID: ProviderID(parts[0]), ModelID: ModelID(parts[1])}
}
