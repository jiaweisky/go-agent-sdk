package session

import (
	"context"
	"runtime"
	"sync"
)

// Coordinator serializes execution per key while allowing different keys to run
// concurrently.
type Coordinator[K comparable] struct {
	mu     sync.Mutex
	active map[K]*coordinatorEntry
	drain  func(ctx context.Context, key K, force bool) error
}

type coordinatorEntry struct {
	done        chan struct{}
	err         error
	pendingWake bool
	stopping    bool
	cancel      context.CancelFunc
}

func NewCoordinator[K comparable](drain func(ctx context.Context, key K, force bool) error) *Coordinator[K] {
	return &Coordinator[K]{active: map[K]*coordinatorEntry{}, drain: drain}
}

// Active snapshots the keys with an execution owned by this coordinator.
func (c *Coordinator[K]) Active() []K {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]K, 0, len(c.active))
	for key := range c.active {
		keys = append(keys, key)
	}
	return keys
}

// Run starts execution while idle or joins the active execution.
func (c *Coordinator[K]) Run(ctx context.Context, key K) error {
	c.mu.Lock()
	entry, ok := c.active[key]
	if ok {
		if entry.stopping {
			done := entry.done
			c.mu.Unlock()
			if err := waitCoordinator(ctx, done, entry); err != nil {
				return err
			}
			return c.Run(ctx, key)
		}
		done := entry.done
		c.mu.Unlock()
		return waitCoordinator(ctx, done, entry)
	}
	next := &coordinatorEntry{done: make(chan struct{})}
	c.active[key] = next
	c.startLocked(key, next, true, false)
	c.mu.Unlock()
	return waitCoordinator(ctx, next.done, next)
}

// Wake registers one coalesced follow-up after newly recorded work.
func (c *Coordinator[K]) Wake(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.active[key]; ok {
		entry.pendingWake = true
		return
	}
	next := &coordinatorEntry{done: make(chan struct{})}
	c.active[key] = next
	c.startLocked(key, next, false, false)
}

// Interrupt stops active execution. Idle interruption is a no-op.
func (c *Coordinator[K]) Interrupt(key K) {
	c.mu.Lock()
	entry, ok := c.active[key]
	if !ok || entry.cancel == nil {
		c.mu.Unlock()
		return
	}
	entry.stopping = true
	entry.pendingWake = false
	cancel := entry.cancel
	c.mu.Unlock()
	cancel()
}

func waitCoordinator(ctx context.Context, done <-chan struct{}, entry *coordinatorEntry) error {
	select {
	case <-done:
		return entry.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// startLocked must be called with c.mu held.
func (c *Coordinator[K]) startLocked(key K, entry *coordinatorEntry, force, successor bool) {
	ctx, cancel := context.WithCancel(context.Background())
	entry.cancel = cancel
	go func() {
		if successor {
			runtime.Gosched()
		}
		err := ctx.Err()
		if err == nil {
			err = c.drain(ctx, key, force)
		}
		c.settle(key, entry, err)
	}()
}

func (c *Coordinator[K]) settle(key K, entry *coordinatorEntry, err error) {
	c.mu.Lock()
	if err == nil && !entry.stopping && entry.pendingWake {
		entry.pendingWake = false
		c.startLocked(key, entry, false, true)
		c.mu.Unlock()
		return
	}

	var successor *coordinatorEntry
	if entry.pendingWake {
		successor = &coordinatorEntry{done: make(chan struct{})}
	}
	if successor == nil {
		delete(c.active, key)
	} else {
		c.active[key] = successor
	}
	entry.err = err
	close(entry.done)
	if successor != nil {
		c.startLocked(key, successor, false, true)
	}
	c.mu.Unlock()
}
