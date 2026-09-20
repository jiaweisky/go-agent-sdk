package core

import (
	"reflect"
	"sync"
	"testing"
)

type stateTestData struct {
	values []string
}

type stateTestDraft struct {
	add func(string)
}

func newStateTest(initial func() *stateTestData, finalize func(stateTestDraft) error) *State[*stateTestData, stateTestDraft] {
	return NewState(StateOptions[*stateTestData, stateTestDraft]{
		Initial: initial,
		Draft: func(data *stateTestData) stateTestDraft {
			return stateTestDraft{add: func(item string) { data.values = append(data.values, item) }}
		},
		Finalize: finalize,
	})
}

func TestStateTransformAndReload(t *testing.T) {
	value := "first"
	state := newStateTest(func() *stateTestData { return &stateTestData{} }, nil)

	if _, err := state.Transform(func(draft stateTestDraft) error {
		draft.add(value)
		return nil
	}); err != nil {
		t.Fatalf("transform: %v", err)
	}
	if got := state.Get().values; !reflect.DeepEqual(got, []string{"first"}) {
		t.Fatalf("after transform = %v", got)
	}

	value = "second"
	if err := state.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := state.Get().values; !reflect.DeepEqual(got, []string{"second"}) {
		t.Fatalf("after reload = %v", got)
	}
}

func TestStateDisposeIsIdempotent(t *testing.T) {
	state := newStateTest(func() *stateTestData { return &stateTestData{} }, nil)

	if _, err := state.Transform(func(draft stateTestDraft) error { draft.add("first"); return nil }); err != nil {
		t.Fatalf("transform first: %v", err)
	}
	dispose, err := state.Transform(func(draft stateTestDraft) error { draft.add("second"); return nil })
	if err != nil {
		t.Fatalf("transform second: %v", err)
	}
	if got := state.Get().values; !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatalf("before dispose = %v", got)
	}
	if err := dispose(); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	if got := state.Get().values; !reflect.DeepEqual(got, []string{"first"}) {
		t.Fatalf("after dispose = %v", got)
	}
	if err := dispose(); err != nil {
		t.Fatalf("second dispose: %v", err)
	}
	if got := state.Get().values; !reflect.DeepEqual(got, []string{"first"}) {
		t.Fatalf("after second dispose = %v", got)
	}
}

func TestStateBatchDefersReloads(t *testing.T) {
	finalized := 0
	batch := NewStateBatch()
	newState := func() *State[*stateTestData, stateTestDraft] {
		return NewState(StateOptions[*stateTestData, stateTestDraft]{
			Initial: func() *stateTestData { return &stateTestData{} },
			Draft: func(data *stateTestData) stateTestDraft {
				return stateTestDraft{add: func(item string) { data.values = append(data.values, item) }}
			},
			Finalize: func(stateTestDraft) error { finalized++; return nil },
			Batch:    batch,
		})
	}
	first := newState()
	second := newState()

	err := batch.Run(func() error {
		if _, err := first.Transform(func(draft stateTestDraft) error { draft.add("first"); return nil }); err != nil {
			return err
		}
		if _, err := first.Transform(func(draft stateTestDraft) error { draft.add("second"); return nil }); err != nil {
			return err
		}
		if _, err := second.Transform(func(draft stateTestDraft) error { draft.add("third"); return nil }); err != nil {
			return err
		}
		if finalized != 0 {
			t.Fatalf("finalize ran inside batch: %d", finalized)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if got := first.Get().values; !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatalf("first after batch = %v", got)
	}
	if got := second.Get().values; !reflect.DeepEqual(got, []string{"third"}) {
		t.Fatalf("second after batch = %v", got)
	}
	if finalized != 2 {
		t.Fatalf("finalized = %d, want 2", finalized)
	}
}

func TestStateBatchConcurrentNoLostReload(t *testing.T) {
	batch := NewStateBatch()
	state := NewState(StateOptions[*stateTestData, stateTestDraft]{
		Initial: func() *stateTestData { return &stateTestData{} },
		Draft: func(data *stateTestData) stateTestDraft {
			return stateTestDraft{add: func(item string) { data.values = append(data.values, item) }}
		},
		Batch: batch,
	})

	const total = 200
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < total/2; i++ {
			_ = batch.Run(func() error {
				_, err := state.Transform(func(draft stateTestDraft) error { draft.add("batch"); return nil })
				return err
			})
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < total/2; i++ {
			_, _ = state.Transform(func(draft stateTestDraft) error { draft.add("direct"); return nil })
		}
	}()
	wg.Wait()

	if got := len(state.Get().values); got != total {
		t.Fatalf("values = %d, want %d (reload dropped)", got, total)
	}
}
