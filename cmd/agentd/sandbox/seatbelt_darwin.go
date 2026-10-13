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
	RegisterProcess("seatbelt", seatbeltProcessLaunch)
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

// seatbeltProcessLaunch wraps the whole process in seatbelt: writes are allowed
// only under roots (and the system temporary directories) and reads of the
// sensitive home directories are denied; the network stays up, because the
// process serves listeners and calls the model.
func seatbeltProcessLaunch(roots []string, command string, args []string) (string, []string) {
	argv := []string{"-p", seatbeltProcessProfile(roots), command}
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
	var roots []string
	if dir != "" {
		roots = append(roots, dir)
	}
	return writeSeatbeltProfile(roots, true, masks)
}

// seatbeltProcessProfile is seatbeltProcessProfileWith with the host's masks.
func seatbeltProcessProfile(roots []string) string {
	return seatbeltProcessProfileWith(roots, homeReadMasks())
}

// seatbeltProcessProfileWith is seatbeltProcessProfile with an explicit mask
// list, for testing. Unlike the per-command profile it keeps the network up.
func seatbeltProcessProfileWith(roots []string, masks []string) string {
	return writeSeatbeltProfile(roots, false, masks)
}

// writeSeatbeltProfile renders the profile shared by the per-command and
// whole-process wrappers; denyNetwork adds the network denial.
func writeSeatbeltProfile(roots []string, denyNetwork bool, masks []string) string {
	clauses := []string{`(subpath "/private/tmp")`, `(subpath "/private/var/folders")`}
	for _, root := range roots {
		// SBPL matches the resolved path; the workdir is under a symlink (e.g.
		// /var -> /private/var) on some setups.
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = resolved
		}
		clauses = append(clauses, fmt.Sprintf("(subpath %q)", root))
	}
	network := ""
	if denyNetwork {
		network = "(deny network*)"
	}
	profile := fmt.Sprintf(
		"(version 1)(allow default)%s(deny file-write*)(allow file-write* %s)",
		network,
		strings.Join(clauses, ""),
	)
	for _, mask := range masks {
		profile += fmt.Sprintf("(deny file-read* (subpath %q))", mask)
	}
	return profile
}
