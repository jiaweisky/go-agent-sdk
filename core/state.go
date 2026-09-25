package core

import "sync"

// StateOptions describes a state: how to create it, how to draft a transform, how
// to finalize one, and the batch that defers its reloads.
type StateOptions[S any, D any] struct {
	Initial  func() S
	Draft    func(S) D
	Finalize func(D) error
	Batch    *StateBatch
}

type stateTransform[D any] struct {
	run func(D) error
}

// State is a replayable transform stack over a materialized value. Materializing
// applies every registered transform to a fresh value, runs the optional
// finalizer, then publishes the result.
type State[S any, D any] struct {
	mu         sync.Mutex
	options    StateOptions[S, D]
	state      S
	transforms []*stateTransform[D]
}

func NewState[S any, D any](options StateOptions[S, D]) *State[S, D] {
	return &State[S, D]{options: options, state: options.Initial()}
}

func (s *State[S, D]) Get() S {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *State[S, D]) materializeLocked() error {
	next := s.options.Initial()
	api := s.options.Draft(next)
	for _, transform := range s.transforms {
		if err := transform.run(api); err != nil {
			return err
		}
	}
	if s.options.Finalize != nil {
		if err := s.options.Finalize(api); err != nil {
			return err
		}
	}
	s.state = next
	return nil
}

// reloadLocked materializes immediately, or defers to the active batch.
func (s *State[S, D]) reloadLocked() error {
	if batch := s.options.Batch; batch != nil && batch.schedule(s, s.Reload) {
		return nil
	}
	return s.materializeLocked()
}

func (s *State[S, D]) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.materializeLocked()
}

// Transform registers a scoped transform and reloads the materialized state.
// The returned dispose removes the transform and reloads again.
func (s *State[S, D]) Transform(update func(D) error) (func() error, error) {
	s.mu.Lock()
	transform := &stateTransform[D]{run: update}
	s.transforms = append(s.transforms, transform)
	err := s.reloadLocked()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}

	return func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		index := -1
		for i, item := range s.transforms {
			if item == transform {
				index = i
				break
			}
		}
		if index == -1 {
			return nil
		}
		s.transforms = append(s.transforms[:index], s.transforms[index+1:]...)
		return s.reloadLocked()
	}, nil
}

// StateBatch defers the reloads of the states it covers: a batch is an explicit
// object shared by those states, and Run applies each pending reload once
// afterwards, in registration order.
type StateBatch struct {
	mu     sync.Mutex
	active bool
	order  []func() error
	seen   map[any]struct{}
}

func NewStateBatch() *StateBatch {
	return &StateBatch{seen: map[any]struct{}{}}
}

// schedule defers a state reload until the current Run completes. It reports
// whether the reload was accepted by an active batch; the check and the append
// are atomic so a concurrent Run cannot drop a reload.
func (b *StateBatch) schedule(state any, reload func() error) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.active {
		return false
	}
	if _, ok := b.seen[state]; ok {
		return true
	}
	b.seen[state] = struct{}{}
	b.order = append(b.order, reload)
	return true
}

// Run executes fn while deferring reloads, then applies each affected state
// once. Nested Run calls join the outer batch. Reloads run after the batch is
// deactivated, so they observe the final batch state.
func (b *StateBatch) Run(fn func() error) error {
	b.mu.Lock()
	if b.active {
		b.mu.Unlock()
		return fn()
	}
	b.active = true
	b.order = b.order[:0]
	b.seen = map[any]struct{}{}
	b.mu.Unlock()
	defer func() {
		// Reset if fn panicked before the normal reset below.
		b.mu.Lock()
		if b.active {
			b.order = b.order[:0]
			b.seen = map[any]struct{}{}
			b.active = false
		}
		b.mu.Unlock()
	}()

	err := fn()

	b.mu.Lock()
	reloads := append([]func() error(nil), b.order...)
	b.order = b.order[:0]
	b.seen = map[any]struct{}{}
	b.active = false
	b.mu.Unlock()

	if err != nil {
		return err
	}
	for _, reload := range reloads {
		if err := reload(); err != nil {
			return err
		}
	}
	return nil
}
