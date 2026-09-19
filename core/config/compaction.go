package config

import "github.com/user/go-agent-sdk/schema"

// CompactionKeep mirrors ConfigCompaction.Keep.
type CompactionKeep struct {
	Tokens *schema.NonNegativeInt `json:"tokens,omitempty"`
}

// CompactionInfo mirrors ConfigCompaction.Info.
type CompactionInfo struct {
	Auto   *bool                  `json:"auto,omitempty"`
	Prune  *bool                  `json:"prune,omitempty"`
	Keep   *CompactionKeep        `json:"keep,omitempty"`
	Buffer *schema.NonNegativeInt `json:"buffer,omitempty"`
}
