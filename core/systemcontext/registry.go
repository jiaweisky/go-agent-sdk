package systemcontext

import (
	"fmt"
	"sort"
	"sync"
)

// Entry mirrors SystemContextRegistry.Entry.
type Entry struct {
	Key  Key
	Load func() (SystemContext, error)
}

// Registry mirrors the SystemContextRegistry service: an ordered set of named
// context sources that can be registered (scoped) and combined.
type Registry struct {
	mu      sync.Mutex
	entries []registryEntry
}

type registryEntry struct {
	key  Key
	load func() (SystemContext, error)
}

func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds an entry and returns an unregister function. It errors on a
// duplicate key.
func (r *Registry) Register(entry Entry) (func(), error) {
	r.mu.Lock()
	for _, item := range r.entries {
		if item.key == entry.Key {
			r.mu.Unlock()
			return nil, fmt.Errorf("Duplicate system context entry key: %s", entry.Key)
		}
	}
	r.entries = append(r.entries, registryEntry{key: entry.Key, load: entry.Load})
	r.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			for i, item := range r.entries {
				if item.key == entry.Key {
					r.entries = append(r.entries[:i], r.entries[i+1:]...)
					return
				}
			}
		})
	}, nil
}

// Load combines the registered sources ordered by key.
func (r *Registry) Load() (SystemContext, error) {
	r.mu.Lock()
	entries := append([]registryEntry(nil), r.entries...)
	r.mu.Unlock()

	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	values := make([]SystemContext, 0, len(entries))
	for _, entry := range entries {
		value, err := entry.load()
		if err != nil {
			return SystemContext{}, err
		}
		values = append(values, value)
	}
	return Combine(values...), nil
}
