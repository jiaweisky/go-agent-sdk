package systemcontext

import "testing"

func TestRegistryRegisterLoadAndDuplicate(t *testing.T) {
	value := "a"
	available := true
	context := Make(testSource("sys/a", &value, &available))

	registry := NewRegistry()
	unregister, err := registry.Register(Entry{Key: "sys/a", Load: func() (SystemContext, error) { return context, nil }})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	loaded, err := registry.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	generation, err := Initialize(loaded)
	if err != nil || generation.Baseline != "base:a" {
		t.Fatalf("generation = %+v err = %v", generation, err)
	}

	if _, err := registry.Register(Entry{Key: "sys/a", Load: func() (SystemContext, error) { return context, nil }}); err == nil {
		t.Fatalf("expected duplicate key error")
	}

	unregister()
	empty, err := registry.Load()
	if err != nil {
		t.Fatalf("load after unregister: %v", err)
	}
	emptyGeneration, err := Initialize(empty)
	if err != nil || emptyGeneration.Baseline != "" {
		t.Fatalf("empty generation = %+v err = %v", emptyGeneration, err)
	}
}
