package effect

import "sync"

type KeyedMutex[K comparable] struct {
	mu    sync.Mutex
	locks map[K]*keyEntry
}

type keyEntry struct {
	mu    sync.Mutex
	users int
}

func MakeUnsafe[K comparable]() *KeyedMutex[K] {
	return &KeyedMutex[K]{locks: make(map[K]*keyEntry)}
}

func (km *KeyedMutex[K]) WithLock(key K, fn func()) {
	km.mu.Lock()
	entry, ok := km.locks[key]
	if !ok {
		entry = &keyEntry{}
		km.locks[key] = entry
	}
	entry.users++
	km.mu.Unlock()

	entry.mu.Lock()
	fn()
	entry.mu.Unlock()

	km.mu.Lock()
	entry.users--
	if entry.users == 0 {
		delete(km.locks, key)
	}
	km.mu.Unlock()
}

func (km *KeyedMutex[K]) Size() int {
	km.mu.Lock()
	defer km.mu.Unlock()
	return len(km.locks)
}
