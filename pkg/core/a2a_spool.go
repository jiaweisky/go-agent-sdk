package core

import (
	"os"
	"path/filepath"
	"time"
)

const (
	// A2ASpoolManagedDirectory is the {Root}/data subdirectory that holds spooled
	// A2A inbound attachments, one directory per turn.
	A2ASpoolManagedDirectory = "a2a-spool"
	// A2ASpoolRetention bounds how long a spooled attachment is kept before the
	// periodic cleanup removes it.
	A2ASpoolRetention = 7 * 24 * time.Hour
)

// A2ASpoolDir is the spool root under a data directory.
func A2ASpoolDir(dataDir string) string {
	return filepath.Join(dataDir, A2ASpoolManagedDirectory)
}

// CleanupA2ASpool removes spooled turn directories older than
// A2ASpoolRetention. A missing spool directory is not an error.
func CleanupA2ASpool(dataDir string) error {
	directory := A2ASpoolDir(dataDir)
	entries, err := ReadDir(directory)
	if err != nil {
		return nil
	}
	cutoff := time.Now().Add(-A2ASpoolRetention)
	for _, entry := range entries {
		if entry.Type != FileKindDirectory {
			continue
		}
		path := filepath.Join(directory, entry.Name)
		info, statErr := os.Stat(path)
		if statErr != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = RemoveAll(path)
		}
	}
	return nil
}
