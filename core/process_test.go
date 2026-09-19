package core

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestAppProcessTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell only")
	}
	proc := NewAppProcess()
	_, err := proc.Run(context.Background(), "/bin/sh", []string{"-c", "sleep 5"}, RunOptions{
		Timeout: 50 * time.Millisecond,
	})
	if !errors.Is(err, ErrTimedOut) {
		t.Fatalf("expected ErrTimedOut, got %v", err)
	}
}

func TestAppProcessCompletesBeforeTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell only")
	}
	proc := NewAppProcess()
	result, err := proc.Run(context.Background(), "/bin/sh", []string{"-c", "printf ok"}, RunOptions{
		CombineOutput: true,
		Timeout:       5 * time.Second,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if string(result.Output) != "ok" {
		t.Fatalf("unexpected output: %q", result.Output)
	}
}
