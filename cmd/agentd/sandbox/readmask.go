package sandbox

import (
	"os"
	"path/filepath"
)

// sensitiveReadPaths are directories under the user's home that hold credentials
// and keys a sandboxed command should not read. They are masked read-only
// (hidden), not deleted: a host that needs a specific one can extend the list.
var sensitiveReadPaths = []string{
	".ssh",
	".aws",
	".gnupg",
	".docker",
	".kube",
	".config/gcloud",
	".config/gh",
}

// homeReadMasks returns the absolute paths of the sensitive home directories
// that exist, for a sandbox to hide. Only directories are returned (they can be
// masked with an empty mount); a missing directory is skipped, so the sandbox
// never references a path that does not exist.
func homeReadMasks() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	var masks []string
	for _, rel := range sensitiveReadPaths {
		path := filepath.Join(home, rel)
		info, statErr := os.Stat(path)
		if statErr != nil || !info.IsDir() {
			continue
		}
		// Resolve symlinks: macOS `/var` -> `/private/var` etc., and the sandbox
		// (seatbelt) matches the resolved path.
		if resolved, resolveErr := filepath.EvalSymlinks(path); resolveErr == nil {
			path = resolved
		}
		masks = append(masks, path)
	}
	return masks
}
