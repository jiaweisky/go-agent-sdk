package core

import (
	"testing"

	"github.com/user/go-agent-sdk/core/config"
)

func TestSessionCompactionConfigAdaptsEntries(t *testing.T) {
	auto := false
	entries := []ConfigEntry{
		ConfigDirectory{},
		ConfigDocument{Info: ConfigInfo{Compaction: &config.CompactionInfo{Auto: &auto}}},
	}
	adapted := SessionCompactionConfig(entries)
	if len(adapted) != 2 {
		t.Fatalf("adapted = %d", len(adapted))
	}
	if adapted[0].CompactionInfo() != nil {
		t.Fatalf("directory entries carry no compaction settings")
	}
	info := adapted[1].CompactionInfo()
	if info == nil || info.Auto == nil || *info.Auto {
		t.Fatalf("document info = %+v", info)
	}
}
