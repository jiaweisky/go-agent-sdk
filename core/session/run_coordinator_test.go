package session

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCoordinatorRunJoinsActive(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var runs int32
	var once sync.Once
	coordinator := NewCoordinator(func(ctx context.Context, key string, force bool) error {
		atomic.AddInt32(&runs, 1)
		once.Do(func() { close(entered) })
		<-release
		return nil
	})

	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() { done <- coordinator.Run(context.Background(), "k") }()
	}
	<-entered
	time.Sleep(20 * time.Millisecond)
	close(release)
	for i := 0; i < 3; i++ {
		if err := <-done; err != nil {
			t.Fatalf("run: %v", err)
		}
	}
	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("runs = %d, want 1", got)
	}
}

func TestCoordinatorWakeCoalesces(t *testing.T) {
	firstEntered := make(chan struct{})
	release := make(chan struct{})
	var runs int32
	var once sync.Once
	coordinator := NewCoordinator(func(ctx context.Context, key string, force bool) error {
		n := atomic.AddInt32(&runs, 1)
		if n == 1 {
			once.Do(func() { close(firstEntered) })
			<-release
		}
		return nil
	})

	done := make(chan error, 1)
	go func() { done <- coordinator.Run(context.Background(), "k") }()
	<-firstEntered
	coordinator.Wake("k")
	coordinator.Wake("k")
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt32(&runs) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := atomic.LoadInt32(&runs); got != 2 {
		t.Fatalf("runs = %d, want 2 (coalesced)", got)
	}
}

func TestCoordinatorInterrupt(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	coordinator := NewCoordinator(func(ctx context.Context, key string, force bool) error {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return ctx.Err()
	})

	done := make(chan error, 1)
	go func() { done <- coordinator.Run(context.Background(), "k") }()
	<-started
	coordinator.Interrupt("k")
	if err := <-done; err == nil {
		t.Fatalf("expected interrupt error")
	}
	if len(coordinator.Active()) != 0 {
		t.Fatalf("active = %v", coordinator.Active())
	}
}
