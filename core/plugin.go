package core

import (
	"fmt"
	"sync"

	"github.com/jiaweisky/go-agent-sdk/core/effect"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// PluginID identifies a plugin.
type PluginID = schema.PluginID

// PluginContext is the narrowed capability surface passed to a plugin: agents,
// skills and references. Catalog, credential and integration transforms are CLI
// infrastructure and intentionally not ported.
type PluginContext struct {
	// Events is the runtime event log when the plugin manager was built with
	// one; nil otherwise. Subscribe with Events.Listen and return the
	// unsubscribe function as the plugin's cleanup.
	Events    Event
	Agents    *Agent
	Skills    *Skill
	Reference *Reference
	Plugin    *Plugin
}

// PluginFunc loads a plugin. It returns an optional cleanup function invoked
// when the plugin is removed or reloaded.
type PluginFunc func(ctx *PluginContext) (cleanup func(), err error)

// Plugin is a per-id lifecycle manager for plugins.
type Plugin struct {
	events Event
	locks  *effect.KeyedMutex[schema.PluginID]

	mu       sync.Mutex
	active   map[schema.PluginID]func()
	loading  map[schema.PluginID]bool
	failures map[schema.PluginID]error
	waiters  map[schema.PluginID][]chan error
	context  *PluginContext
}

// NewPlugin builds a plugin manager. events may be nil (no plugin.added event).
func NewPlugin(events Event, agents *Agent, skills *Skill, reference *Reference) *Plugin {
	plugin := &Plugin{
		events:   events,
		locks:    effect.MakeUnsafe[schema.PluginID](),
		active:   map[schema.PluginID]func(){},
		loading:  map[schema.PluginID]bool{},
		failures: map[schema.PluginID]error{},
		waiters:  map[schema.PluginID][]chan error{},
	}
	plugin.context = &PluginContext{Events: events, Agents: agents, Skills: skills, Reference: reference, Plugin: plugin}
	return plugin
}

// Add loads (or reloads) a plugin, mirroring Plugin.add.
func (p *Plugin) Add(id schema.PluginID, plugin PluginFunc) error {
	var result error
	p.locks.WithLock(id, func() {
		p.mu.Lock()
		if p.loading[id] {
			p.mu.Unlock()
			result = fmt.Errorf("plugin load cycle detected for %s", id)
			return
		}
		p.loading[id] = true
		delete(p.failures, id)
		existing := p.active[id]
		delete(p.active, id)
		p.mu.Unlock()

		if existing != nil {
			existing()
		}

		cleanup, err := plugin(p.context)

		p.mu.Lock()
		delete(p.loading, id)
		waiters := p.waiters[id]
		delete(p.waiters, id)
		if err != nil {
			p.failures[id] = err
		} else if cleanup != nil {
			p.active[id] = cleanup
		}
		p.mu.Unlock()

		if err != nil {
			for _, waiter := range waiters {
				waiter <- err
			}
			result = err
			return
		}
		for _, waiter := range waiters {
			waiter <- nil
		}
		if p.events != nil {
			_, _ = p.events.Publish(schema.PluginEventAdded, map[string]any{"id": string(id)}, nil)
		}
	})
	return result
}

// Remove unloads a plugin, mirroring Plugin.remove.
func (p *Plugin) Remove(id schema.PluginID) error {
	var result error
	p.locks.WithLock(id, func() {
		p.mu.Lock()
		if p.loading[id] {
			p.mu.Unlock()
			result = fmt.Errorf("cannot remove plugin %s while it is loading", id)
			return
		}
		cleanup := p.active[id]
		delete(p.active, id)
		delete(p.failures, id)
		p.mu.Unlock()
		if cleanup != nil {
			cleanup()
		}
	})
	return result
}

// Close unloads every active plugin.
func (p *Plugin) Close() error {
	p.mu.Lock()
	cleanups := make([]func(), 0, len(p.active))
	for _, cleanup := range p.active {
		cleanups = append(cleanups, cleanup)
	}
	p.active = map[schema.PluginID]func(){}
	p.mu.Unlock()
	for _, cleanup := range cleanups {
		if cleanup != nil {
			cleanup()
		}
	}
	return nil
}

// Wait blocks until a plugin is active (nil), has failed, or is already active.
func (p *Plugin) Wait(id schema.PluginID) error {
	var (
		waiter chan error
		result error
	)
	p.locks.WithLock(id, func() {
		p.mu.Lock()
		if _, ok := p.active[id]; ok {
			p.mu.Unlock()
			return
		}
		if err, ok := p.failures[id]; ok {
			p.mu.Unlock()
			result = err
			return
		}
		waiter = make(chan error, 1)
		p.waiters[id] = append(p.waiters[id], waiter)
		p.mu.Unlock()
	})
	if waiter == nil {
		return result
	}
	return <-waiter
}
