//go:build darwin

package sandbox

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

func init() {
	Register("seatbelt", func() core.AppProcess {
		return sandboxed{inner: core.NewAppProcess(), launch: seatbeltLaunch}
	})
}

// seatbeltLaunch wraps a command in the macOS seatbelt sandbox (`sandbox-exec`):
// the process may write only under the working directory and the system temporary
// directories, may not use the network, and may not read the user's sensitive
// credential directories (see homeReadMasks). It is best-effort — the SDK ships no
// guarantee for it; `sandbox-exec` is deprecated by Apple.
func seatbeltLaunch(dir, command string, args []string) (string, []string) {
	argv := []string{"-p", seatbeltProfile(dir), command}
	return "sandbox-exec", append(argv, args...)
}

// seatbeltProfile renders an SBPL profile: default-allow, then deny the network
// and all file writes, re-allow writes under the writable roots, then deny reads
// of the masked credential directories.
func seatbeltProfile(dir string) string {
	return seatbeltProfileWith(dir, homeReadMasks())
}

// seatbeltProfileWith is seatbeltProfile with an explicit mask list, for testing.
func seatbeltProfileWith(dir string, masks []string) string {
	if dir != "" {
		// SBPL matches the resolved path; the workdir is under a symlink (e.g.
		// /var -> /private/var) on some setups.
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			dir = resolved
		}
	}
	writes := []string{`(subpath "/private/tmp")`, `(subpath "/private/var/folders")`}
	if dir != "" {
		writes = append(writes, fmt.Sprintf("(subpath %q)", dir))
	}
	profile := fmt.Sprintf(
		"(version 1)(allow default)(deny network*)(deny file-write*)(allow file-write* %s)",
		strings.Join(writes, ""),
	)
	for _, mask := range masks {
		profile += fmt.Sprintf("(deny file-read* (subpath %q))", mask)
	}
	return profile
}
