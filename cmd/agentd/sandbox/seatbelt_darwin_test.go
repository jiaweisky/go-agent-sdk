//go:build darwin

package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

func TestSeatbeltLaunch(t *testing.T) {
	name, argv := seatbeltLaunch("/work", "bash", []string{"-c", "echo hi"})
	if name != "sandbox-exec" {
		t.Fatalf("name = %q, want sandbox-exec", name)
	}
	if len(argv) != 5 || argv[0] != "-p" || argv[2] != "bash" || argv[3] != "-c" || argv[4] != "echo hi" {
		t.Fatalf("argv = %v", argv)
	}
	if !strings.Contains(argv[1], `(subpath "/work")`) {
		t.Fatalf("profile missing the workdir subpath: %s", argv[1])
	}
}

func TestSeatbeltProfile(t *testing.T) {
	profile := seatbeltProfile("")
	for _, want := range []string{
		"(version 1)", "(allow default)", "(deny network*)", "(deny file-write*)",
		`(subpath "/private/tmp")`, `(subpath "/private/var/folders")`,
	} {
		if !strings.Contains(profile, want) {
			t.Errorf("profile missing %q: %s", want, profile)
		}
	}
	if strings.Contains(profile, `(subpath "")`) {
		t.Errorf("empty dir produced an empty subpath: %s", profile)
	}
}

func TestSeatbeltProfileMasks(t *testing.T) {
	profile := seatbeltProfileWith("/work", []string{"/home/u/.ssh"})
	if !strings.Contains(profile, `(deny file-read* (subpath "/home/u/.ssh"))`) {
		t.Fatalf("profile missing the read mask: %s", profile)
	}
}

// TestSeatbeltReadMask verifies a masked credential directory cannot be read,
// while the working directory stays usable.
func TestSeatbeltReadMask(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	ssh := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(ssh, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ssh, "id_rsa"), []byte("SECRET"), 0600); err != nil {
		t.Fatal(err)
	}

	process, err := Resolve("seatbelt")
	if err != nil || process == nil {
		t.Fatalf("Resolve(seatbelt) = %v, %v", process, err)
	}
	dir := t.TempDir()
	blocked, err := process.Run(context.Background(), "/bin/sh",
		[]string{"-c", "cat " + filepath.Join(ssh, "id_rsa")}, core.RunOptions{Dir: dir})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if blocked.ExitCode == 0 {
		t.Fatalf("reading the masked key succeeded (expected a denial): %q", blocked.Stdout)
	}

	// The working directory is still readable.
	ok, err := process.Run(context.Background(), "/bin/sh",
		[]string{"-c", "echo hi"}, core.RunOptions{Dir: dir})
	if err != nil || ok.ExitCode != 0 {
		t.Fatalf("the workdir became unusable: %v (exit %d)", err, ok.ExitCode)
	}
}

func TestSeatbeltRegistered(t *testing.T) {
	if p, err := Resolve("seatbelt"); err != nil || p == nil {
		t.Fatalf("Resolve(seatbelt) = %v, %v", p, err)
	}
}

// TestSeatbeltEnforcesWrites runs a real command through the wrapper: a write
// under the working directory succeeds, and a write outside the allowed roots
// (the user's home) is denied.
func TestSeatbeltEnforcesWrites(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available")
	}
	process, err := Resolve("seatbelt")
	if err != nil || process == nil {
		t.Fatalf("Resolve(seatbelt) = %v, %v", process, err)
	}
	dir := t.TempDir()

	inside, err := process.Run(context.Background(), "/bin/sh",
		[]string{"-c", "echo hi > inside.txt"}, core.RunOptions{Dir: dir})
	if err != nil {
		t.Fatalf("write inside the workdir errored: %v", err)
	}
	if inside.ExitCode != 0 {
		t.Fatalf("write inside the workdir was denied (exit %d, stderr %s)", inside.ExitCode, inside.Stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "inside.txt")); err != nil {
		t.Fatalf("inside.txt was not written: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	outside := filepath.Join(home, ".agentd-seatbelt-deny-test")
	denied, err := process.Run(context.Background(), "/bin/sh",
		[]string{"-c", "echo bad > " + outside}, core.RunOptions{Dir: dir})
	if err != nil {
		t.Fatalf("denial run errored unexpectedly: %v", err)
	}
	if denied.ExitCode == 0 {
		_ = os.Remove(outside)
		t.Fatalf("write outside the sandbox was allowed (expected a denial): %s", outside)
	}
}

// TestSeatbeltStreams runs a real command through the wrapper's streaming path.
func TestSeatbeltStreams(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available")
	}
	process, err := Resolve("seatbelt")
	if err != nil || process == nil {
		t.Fatalf("Resolve(seatbelt) = %v, %v", process, err)
	}
	var lines []string
	for line, err := range process.RunStream(context.Background(), "/bin/sh",
		[]string{"-c", "echo one; echo two"}, core.RunStreamOptions{Dir: t.TempDir()}) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		lines = append(lines, line)
	}
	if !reflect.DeepEqual(lines, []string{"one", "two"}) {
		t.Fatalf("lines = %v, want [one two]", lines)
	}
}
