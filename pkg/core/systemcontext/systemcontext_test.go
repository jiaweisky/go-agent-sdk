package systemcontext

import (
	"encoding/json"
	"errors"
	"testing"
)

func testSource(key Key, value *string, available *bool) Source[string] {
	return Source[string]{
		Key:      key,
		Load:     func() (string, bool, error) { return *value, *available, nil },
		Baseline: func(current string) string { return "base:" + current },
		Update:   func(previous, current string) string { return "update:" + previous + "->" + current },
		Removed:  func(previous string) string { return "removed:" + previous },
	}
}

func strPtr(value string) *string { return &value }

func TestInitializeAndReconcile(t *testing.T) {
	value := "a"
	available := true
	ctx := Make(testSource("sys/one", &value, &available))

	generation, err := Initialize(ctx)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if generation.Baseline != "base:a" || len(generation.Snapshot) != 1 {
		t.Fatalf("generation = %+v", generation)
	}

	unchanged, err := Reconcile(ctx, generation.Snapshot)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if unchanged.Tag != "Unchanged" {
		t.Fatalf("expected unchanged, got %+v", unchanged)
	}

	value = "b"
	updated, err := Reconcile(ctx, generation.Snapshot)
	if err != nil {
		t.Fatalf("reconcile update: %v", err)
	}
	if updated.Tag != "Updated" || updated.Text != "update:a->b" {
		t.Fatalf("updated = %+v", updated)
	}
}

func TestInitializeBlockedByUnavailable(t *testing.T) {
	value := "a"
	available := false
	ctx := Make(testSource("sys/one", &value, &available))

	_, err := Initialize(ctx)
	var blocked *InitializationBlocked
	if !errors.As(err, &blocked) {
		t.Fatalf("expected initialization blocked, got %v", err)
	}
}

func TestReplaceBlockedAndReady(t *testing.T) {
	value := "a"
	available := false
	ctx := Make(testSource("sys/one", &value, &available))
	previous := Snapshot{"sys/one": {Value: json.RawMessage(`"a"`)}, "sys/gone": {Value: json.RawMessage(`"x"`)}}

	blocked, err := Replace(ctx, previous)
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if blocked.Tag != "ReplacementBlocked" {
		t.Fatalf("blocked = %+v", blocked)
	}

	available = true
	value = "c"
	ready, err := Replace(ctx, previous)
	if err != nil {
		t.Fatalf("replace ready: %v", err)
	}
	if ready.Tag != "ReplacementReady" || ready.Generation == nil || ready.Generation.Baseline != "base:c" {
		t.Fatalf("ready = %+v", ready)
	}
}

func TestReconcileRemoval(t *testing.T) {
	previous := Snapshot{"sys/gone": {Value: json.RawMessage(`"x"`), Removed: strPtr("removed:x")}}
	result, err := Reconcile(Empty, previous)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.Tag != "Updated" || result.Text != "removed:x" {
		t.Fatalf("result = %+v", result)
	}
}

func TestDecodeSnapshotValidatesKeys(t *testing.T) {
	if _, err := DecodeSnapshot([]byte(`{"invalid":{"value":"bad"}}`)); err == nil {
		t.Fatalf("expected invalid key error")
	}
	snapshot, err := DecodeSnapshot([]byte(`{"sys/one":{"value":"a"}}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(snapshot["sys/one"].Value) != `"a"` {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if _, err := DecodeSnapshot([]byte(`{"sys/one":{}}`)); err == nil {
		t.Fatalf("expected missing value error")
	}
}

func TestMakeRejectsEmptyRemoval(t *testing.T) {
	value := "a"
	available := true
	ctx := Make(Source[string]{
		Key:      "sys/one",
		Load:     func() (string, bool, error) { return value, available, nil },
		Baseline: func(current string) string { return "base:" + current },
		Update:   func(previous, current string) string { return "update" },
		Removed:  func(previous string) string { return "" },
	})
	if _, err := Initialize(ctx); err == nil {
		t.Fatalf("expected empty removal error")
	}
}

func TestCombineRejectsDuplicateKeys(t *testing.T) {
	value := "a"
	available := true
	ctx := Make(testSource("sys/one", &value, &available))

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatalf("expected panic on duplicate key")
		}
	}()
	Combine(ctx, ctx)
}
