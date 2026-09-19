package core

import "github.com/user/go-agent-sdk/schema"

// ProviderV2 re-exports the shared provider schema (core/provider.ts).
type (
	ProviderID      = schema.ProviderID
	ProviderAISDK   = schema.ProviderAISDK
	ProviderNative  = schema.ProviderNative
	ProviderAPI     = schema.ProviderAPI
	ProviderRequest = schema.ProviderRequest
	ProviderInfo    = schema.ProviderInfo
)

// ProviderInfoEmpty mirrors ProviderV2.Info.empty.
func ProviderInfoEmpty(id ProviderID) ProviderInfo {
	return schema.ProviderInfoEmpty(id)
}
