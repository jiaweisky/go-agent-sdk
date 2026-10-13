//go:build windows

package sandbox

import (
	"syscall"
	"testing"
)

func TestAppContainerRegistered(t *testing.T) {
	if p, err := Resolve("appcontainer"); err != nil || p == nil {
		t.Fatalf("Resolve(appcontainer) = %v, %v", p, err)
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
	if got, cut := truncate([]byte("abcd"), nil); string(got) != "abcd" || cut {
		t.Fatalf("unbounded truncate = %q, %v", got, cut)
	}
	limit := 2
	if got, cut := truncate([]byte("abcd"), &limit); string(got) != "ab" || !cut {
		t.Fatalf("bounded truncate = %q, %v", got, cut)
	}
}

func TestDescribeAndContains(t *testing.T) {
	if describeCommand("ls", nil) != "ls" || describeCommand("ls", []string{"-l"}) != "ls -l" {
		t.Fatal("describeCommand is wrong")
	}
	if !containsInt([]int{1, 2}, 2) || containsInt([]int{1, 2}, 3) {
		t.Fatal("containsInt is wrong")
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
