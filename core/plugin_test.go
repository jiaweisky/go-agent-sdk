package core

import (
	"errors"
	"testing"
)

func TestPluginLifecycle(t *testing.T) {
	plugin := NewPlugin(nil, NewAgent(), NewSkill(nil))
	loaded := 0
	cleaned := 0

	if err := plugin.Add("a", func(ctx *PluginContext) (func(), error) {
		loaded++
		if ctx.Agents == nil || ctx.Skills == nil || ctx.Plugin != plugin {
			t.Fatalf("unexpected plugin context: %+v", ctx)
		}
		return func() { cleaned++ }, nil
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if loaded != 1 {
		t.Fatalf("loaded = %d", loaded)
	}
	if err := plugin.Wait("a"); err != nil {
		t.Fatalf("wait: %v", err)
	}

	// Reloading closes the previous instance.
	if err := plugin.Add("a", func(*PluginContext) (func(), error) {
		loaded++
		return nil, nil
	}); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded != 2 || cleaned != 1 {
		t.Fatalf("loaded = %d, cleaned = %d", loaded, cleaned)
	}

	if err := plugin.Remove("a"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := plugin.Add("a", func(*PluginContext) (func(), error) {
		loaded++
		return nil, nil
	}); err != nil {
		t.Fatalf("re-add: %v", err)
	}
}

func TestPluginWaitFailure(t *testing.T) {
	plugin := NewPlugin(nil, NewAgent(), NewSkill(nil))
	loadErr := errors.New("boom")
	if err := plugin.Add("bad", func(*PluginContext) (func(), error) {
		return nil, loadErr
	}); !errors.Is(err, loadErr) {
		t.Fatalf("add error = %v", err)
	}
	if err := plugin.Wait("bad"); !errors.Is(err, loadErr) {
		t.Fatalf("wait error = %v", err)
	}
}
