//go:build windows

package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/internal/core/util"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

func TestAppContainerRegistered(t *testing.T) {
	if p, err := Resolve("appcontainer"); err != nil || p == nil {
		t.Fatalf("Resolve(appcontainer) = %v, %v", p, err)
	}
}

// TestAppContainerEnforcesWrites runs a real command through the wrapper: a
// write under the run directory succeeds, and a write outside it (the user's
// home) is denied because the AppContainer has no access there.
func TestAppContainerEnforcesWrites(t *testing.T) {
	process, err := Resolve("appcontainer")
	if err != nil || process == nil {
		t.Fatalf("Resolve(appcontainer) = %v, %v", process, err)
	}
	dir := t.TempDir()
	inside, err := process.Run(context.Background(), "cmd",
		[]string{"/c", "echo hi > inside.txt"}, core.RunOptions{Dir: dir})
	if err != nil {
		t.Fatalf("write inside the run dir errored: %v", err)
	}
	if inside.ExitCode != 0 {
		t.Fatalf("write inside the run dir was denied (exit %d): %s", inside.ExitCode, inside.Stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "inside.txt")); err != nil {
		t.Fatalf("inside.txt was not written: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	outside := filepath.Join(home, "agentd-appcontainer-deny-test.txt")
	denied, err := process.Run(context.Background(), "cmd",
		[]string{"/c", "echo bad > " + outside}, core.RunOptions{Dir: dir})
	if err != nil {
		t.Fatalf("denial run errored unexpectedly: %v", err)
	}
	if denied.ExitCode == 0 {
		_ = os.Remove(outside)
		t.Fatalf("write outside the AppContainer was allowed: %s", outside)
	}
}

func TestEnvBlockReplaces(t *testing.T) {
	// A non-nil Env without ExtendEnv replaces the base environment.
	pairs := decodeEnv(envBlock(map[string]string{"ONLY": "1"}, false))
	if len(pairs) != 1 || pairs[0] != "ONLY=1" {
		t.Fatalf("envBlock = %v, want [ONLY=1]", pairs)
	}
}

func TestEnvBlockExtends(t *testing.T) {
	t.Setenv("AGENTD_ENV_TEST", "present")
	pairs := decodeEnv(envBlock(map[string]string{"ONLY": "1"}, true))
	if !containsString(pairs, "ONLY=1") || !containsString(pairs, "AGENTD_ENV_TEST=present") {
		t.Fatalf("extended envBlock missing pairs: %v", pairs)
	}
}

func TestTruncate(t *testing.T) {
	if got, cut := util.Truncate([]byte("abcd"), nil); string(got) != "abcd" || cut {
		t.Fatalf("unbounded truncate = %q, %v", got, cut)
	}
	limit := 2
	if got, cut := util.Truncate([]byte("abcd"), &limit); string(got) != "ab" || !cut {
		t.Fatalf("bounded truncate = %q, %v", got, cut)
	}
}

// decodeEnv splits a UTF-16 double-null-terminated block into its KEY=VALUE
// strings.
func decodeEnv(block []uint16) []string {
	var pairs []string
	start := 0
	for i, unit := range block {
		if unit != 0 {
			continue
		}
		if i > start {
			pairs = append(pairs, syscall.UTF16ToString(block[start:i]))
		}
		start = i + 1
	}
	return pairs
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
