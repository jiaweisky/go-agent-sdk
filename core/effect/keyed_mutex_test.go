package effect

import (
	"sync"
	"testing"
	"time"
)

func requireSize[K comparable](t *testing.T, km *KeyedMutex[K], want int) {
	t.Helper()
	if got := km.Size(); got != want {
		t.Fatalf("Size() = %d, want %d", got, want)
	}
}

func TestKeyedMutexSerializesPerKey(t *testing.T) {
	km := MakeUnsafe[string]()

	var counter int
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			km.WithLock("shared", func() { counter++ })
		}()
	}
	wg.Wait()

	if counter != 100 {
		t.Fatalf("counter = %d, want 100", counter)
	}
	requireSize(t, km, 0)
}

func TestKeyedMutexReleasesEntry(t *testing.T) {
	km := MakeUnsafe[int]()

	var inside int
	km.WithLock(1, func() { inside = km.Size() })
	if inside != 1 {
		t.Fatalf("Size() inside lock = %d, want 1", inside)
	}
	requireSize(t, km, 0)

	// A re-used key leaves no residue.
	km.WithLock(1, func() {})
	km.WithLock(2, func() {})
	requireSize(t, km, 0)
}

func TestKeyedMutexDifferentKeysIndependent(t *testing.T) {
	km := MakeUnsafe[int]()

	// Hold key 1, then show key 2 can still be entered while key 1 is held.
	held := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan struct{})
	go func() {
		km.WithLock(1, func() {
			close(held)
			<-release
		})
		close(firstDone)
	}()
	<-held

	other := make(chan struct{})
	go func() {
		km.WithLock(2, func() { close(other) })
	}()
	select {
	case <-other:
	case <-time.After(5 * time.Second):
		t.Fatal("key 2 blocked while key 1 held")
	}

	close(release)
	<-firstDone
}
