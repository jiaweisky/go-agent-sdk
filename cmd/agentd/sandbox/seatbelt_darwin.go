//go:build darwin

package sandbox

import (
	"fmt"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

func init() {
	Register("seatbelt", func() core.AppProcess {
		return sandboxed{inner: core.NewAppProcess(), launch: seatbeltLaunch}
	})
}

// seatbeltLaunch wraps a command in the macOS seatbelt sandbox (`sandbox-exec`):
// the process may read anywhere but may write only under the working directory
// and the system temporary directories, and may not use the network. It is
// best-effort — the SDK ships no guarantee for it; `sandbox-exec` is deprecated
// by Apple.
func seatbeltLaunch(dir, command string, args []string) (string, []string) {
	argv := []string{"-p", seatbeltProfile(dir), command}
	return "sandbox-exec", append(argv, args...)
}

// seatbeltProfile renders an SBPL profile: default-allow (reads are NOT
// restricted), then deny the network and all file writes, then re-allow writes
// under the writable roots.
func seatbeltProfile(dir string) string {
	writes := []string{`(subpath "/private/tmp")`, `(subpath "/private/var/folders")`}
	if dir != "" {
		writes = append(writes, fmt.Sprintf("(subpath %q)", dir))
	}
	return fmt.Sprintf(
		"(version 1)(allow default)(deny network*)(deny file-write*)(allow file-write* %s)",
		strings.Join(writes, ""),
	)
}
