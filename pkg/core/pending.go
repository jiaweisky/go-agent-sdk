package core

import "sync"

// pendingEntry is one in-flight request awaiting a reply. meta carries
// service-specific context that is not part of the request (for example the
// agent a permission request was evaluated against).
type pendingEntry[K comparable, R any, S any, M any] struct {
	id      K
	request R
	meta    M
	done    chan S
	settled bool
}

// pendingRegistry tracks in-flight requests by id. A reply reserves an entry so
// a concurrent reply or close can settle it at most once.
type pendingRegistry[K comparable, R any, S any, M any] struct {
	mu      sync.Mutex
	entries map[K]*pendingEntry[K, R, S, M]
	closed  bool
}

func newPendingRegistry[K comparable, R any, S any, M any]() *pendingRegistry[K, R, S, M] {
	return &pendingRegistry[K, R, S, M]{entries: map[K]*pendingEntry[K, R, S, M]{}}
}

// add registers a new entry. It reports closed when the registry is already
// closed, or duplicate when id is already pending.
func (r *pendingRegistry[K, R, S, M]) add(id K, request R, meta M) (*pendingEntry[K, R, S, M], bool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, true, false
	}
	if _, exists := r.entries[id]; exists {
		return nil, false, true
	}
	entry := &pendingEntry[K, R, S, M]{id: id, request: request, meta: meta, done: make(chan S, 1)}
	r.entries[id] = entry
	return entry, false, false
}

// reserve atomically claims an entry so a concurrent reply cannot settle it
// twice.
func (r *pendingRegistry[K, R, S, M]) reserve(id K) (*pendingEntry[K, R, S, M], bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[id]
	if !ok || entry.settled {
		return nil, false
	}
	entry.settled = true
	return entry, true
}

// release returns a reserved entry to the pending set. It reports true when the
// registry closed meanwhile, having removed the entry, so the caller can send
// its rejection.
func (r *pendingRegistry[K, R, S, M]) release(entry *pendingEntry[K, R, S, M]) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		delete(r.entries, entry.id)
		return true
	}
	entry.settled = false
	return false
}

func (r *pendingRegistry[K, R, S, M]) finish(id K) {
	r.mu.Lock()
	delete(r.entries, id)
	r.mu.Unlock()
}

// close marks the registry closed and returns its unsettled entries, removed.
func (r *pendingRegistry[K, R, S, M]) close() []*pendingEntry[K, R, S, M] {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	victims := make([]*pendingEntry[K, R, S, M], 0, len(r.entries))
	for id, entry := range r.entries {
		if entry.settled {
			continue
		}
		entry.settled = true
		victims = append(victims, entry)
		delete(r.entries, id)
	}
	return victims
}

// takeUnsettled removes and returns the unsettled entries matching keep.
func (r *pendingRegistry[K, R, S, M]) takeUnsettled(
	keep func(*pendingEntry[K, R, S, M]) bool,
) []*pendingEntry[K, R, S, M] {
	r.mu.Lock()
	defer r.mu.Unlock()
	var taken []*pendingEntry[K, R, S, M]
	for id, entry := range r.entries {
		if entry.settled || !keep(entry) {
			continue
		}
		entry.settled = true
		taken = append(taken, entry)
		delete(r.entries, id)
	}
	return taken
}

// listWhere returns the requests of the entries matching keep.
func (r *pendingRegistry[K, R, S, M]) listWhere(keep func(*pendingEntry[K, R, S, M]) bool) []R {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]R, 0, len(r.entries))
	for _, entry := range r.entries {
		if keep(entry) {
			result = append(result, entry.request)
		}
	}
	return result
}

// get returns the request registered for id.
func (r *pendingRegistry[K, R, S, M]) get(id K) (R, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[id]
	if !ok {
		var zero R
		return zero, false
	}
	return entry.request, true
}
