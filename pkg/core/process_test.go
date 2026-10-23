package core

import (
	"context"
	"errors"
	"runtime"
	"slices"
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

func TestAppProcessRunStreamIsIncremental(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell only")
	}
	proc := NewAppProcess()
	var firstAt, secondAt time.Time
	for line, err := range proc.RunStream(context.Background(), "/bin/sh", []string{
		"-c", "printf 'first\\n'; sleep 0.4; printf 'second\\n'",
	}) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		switch line {
		case "first":
			firstAt = time.Now()
		case "second":
			secondAt = time.Now()
		}
	}
	if firstAt.IsZero() || secondAt.IsZero() {
		t.Fatalf("missing lines: first=%v second=%v", firstAt, secondAt)
	}
	if gap := secondAt.Sub(firstAt); gap < 200*time.Millisecond {
		t.Fatalf("RunStream buffered output: lines arrived %v apart", gap)
	}
}

func TestAppProcessRunStreamIncludeStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell only")
	}
	proc := NewAppProcess()
	var lines []string
	for line, err := range proc.RunStream(context.Background(), "/bin/sh", []string{
		"-c", "printf 'out\\n'; printf 'err\\n' 1>&2",
	}, RunStreamOptions{IncludeStderr: true}) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		lines = append(lines, line)
	}
	if !slices.Contains(lines, "out") || !slices.Contains(lines, "err") {
		t.Fatalf("expected merged output, got %v", lines)
	}
}

func TestAppProcessRunStreamStopIsBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell only")
	}
	proc := NewAppProcess()
	done := make(chan struct{})
	go func() {
		// The child ignores SIGTERM, so only the WaitDelay kill can end it.
		for range proc.RunStream(context.Background(), "/bin/sh", []string{
			"-c", "trap '' TERM; printf 'first\\n'; sleep 5",
		}) {
			break
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("RunStream did not return after early stop")
	}
}

func TestAppProcessRunStreamTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell only")
	}
	proc := NewAppProcess()
	var streamErr error
	for _, err := range proc.RunStream(context.Background(), "/bin/sh", []string{
		"-c", "sleep 5",
	}, RunStreamOptions{Timeout: 50 * time.Millisecond}) {
		if err != nil {
			streamErr = err
		}
	}
	if !errors.Is(streamErr, ErrTimedOut) {
		t.Fatalf("expected ErrTimedOut, got %v", streamErr)
	}
}

func TestAppProcessRunStreamRejectsUnexpectedExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell only")
	}
	proc := NewAppProcess()
	var streamErr error
	for _, err := range proc.RunStream(context.Background(), "/bin/sh", []string{
		"-c", "printf 'x\\n'; exit 3",
	}, RunStreamOptions{OkExitCodes: []int{0}}) {
		if err != nil {
			streamErr = err
		}
	}
	if streamErr == nil {
		t.Fatal("expected an exit-code error")
	}
}

func TestBoundedBufferConsumesAll(t *testing.T) {
	limit := 4
	buf := newBoundedBuffer(&limit)
	if n, err := buf.Write([]byte("abcdef")); n != 6 || err != nil {
		t.Fatalf("write = %d, %v; want 6, nil", n, err)
	}
	if got := buf.String(); got != "abcd" {
		t.Fatalf("buffer = %q, want abcd", got)
	}
	if n, err := buf.Write([]byte("gh")); n != 2 || err != nil {
		t.Fatalf("overflow write = %d, %v; want 2, nil", n, err)
	}
	if got := buf.String(); got != "abcd" {
		t.Fatalf("buffer = %q after overflow, want abcd", got)
	}
}
