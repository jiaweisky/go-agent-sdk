package core

import (
	"github.com/user/go-agent-sdk/core/config"
	"github.com/user/go-agent-sdk/core/session"
)

// CompactionInfo satisfies session.CompactionConfigEntry for config documents.
func (d ConfigDocument) CompactionInfo() *config.CompactionInfo { return d.Info.Compaction }

// CompactionInfo satisfies session.CompactionConfigEntry: directory entries
// carry no compaction settings.
func (d ConfigDirectory) CompactionInfo() *config.CompactionInfo { return nil }

// SessionCompactionConfig adapts Config.Entry to the narrow surface
// session.Compaction needs.
func SessionCompactionConfig(entries []ConfigEntry) []session.CompactionConfigEntry {
	adapted := make([]session.CompactionConfigEntry, 0, len(entries))
	for _, entry := range entries {
		if compaction, ok := entry.(session.CompactionConfigEntry); ok {
			adapted = append(adapted, compaction)
		}
	}
	return adapted
}
