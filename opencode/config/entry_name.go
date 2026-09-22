package config

import (
	"path"
	"strings"
)

// ConfigEntryNameFromPath mirrors configEntryNameFromPath: strip a known prefix
// from an already-relative path, then drop the extension.
func ConfigEntryNameFromPath(relativePath string, prefixes []string) string {
	normalized := path.Clean(strings.ReplaceAll(relativePath, "\\", "/"))
	candidate, matched := "", false
	for _, prefix := range prefixes {
		if strings.HasPrefix(normalized, prefix) {
			candidate, matched = normalized[len(prefix):], true
			break
		}
	}
	if !matched {
		candidate = path.Base(normalized)
	}
	if ext := path.Ext(candidate); ext != "" {
		candidate = candidate[:len(candidate)-len(ext)]
	}
	return candidate
}
