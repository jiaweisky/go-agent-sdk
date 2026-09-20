// Package systemcontext ports core/system-context/index.ts: privileged system
// context modelled as independently refreshable typed sources.
package systemcontext

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// Key is a stable namespaced identity for one independently refreshable source.
type Key string

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._/-]*$`)

func (k Key) Valid() bool {
	return keyPattern.MatchString(string(k))
}

// Source defines one typed source before its value type is hidden by Make.
// Load reports whether the source is available; a false second return means the
// source is temporarily unavailable.
type Source[A any] struct {
	Key      Key
	Load     func() (A, bool, error)
	Baseline func(current A) string
	Update   func(previous, current A) string
	Removed  func(previous A) string
}

// SourceSnapshot mirrors SystemContext.SourceSnapshot.
type SourceSnapshot struct {
	Value   json.RawMessage `json:"value"`
	Removed *string         `json:"removed,omitempty"`
}

// Snapshot mirrors SystemContext.Snapshot.
type Snapshot map[Key]SourceSnapshot

// DecodeSnapshot mirrors decoding SystemContext.Snapshot: it parses the stored
// JSON and validates each source key and source snapshot shape.
func DecodeSnapshot(raw []byte) (Snapshot, error) {
	var snapshot Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, fmt.Errorf("system context snapshot must be an object")
	}
	for key, source := range snapshot {
		if !key.Valid() {
			return nil, fmt.Errorf("invalid system context key: %s", key)
		}
		if len(source.Value) == 0 {
			return nil, fmt.Errorf("system context source %s is missing value", key)
		}
		if source.Removed != nil && *source.Removed == "" {
			return nil, fmt.Errorf("system context source %s has an empty removal", key)
		}
	}
	return snapshot, nil
}

// Generation mirrors SystemContext.Generation.
type Generation struct {
	Baseline string
	Snapshot Snapshot
}

// InitializationBlocked mirrors SystemContext.InitializationBlocked.
type InitializationBlocked struct {
	Keys []Key
}

func (e *InitializationBlocked) Error() string {
	return fmt.Sprintf("System context initialization blocked by unavailable sources: %v", e.Keys)
}

// DuplicateKeyError mirrors SystemContext.DuplicateKeyError.
type DuplicateKeyError struct {
	Key Key
}

func (e *DuplicateKeyError) Error() string {
	return "Duplicate system context key: " + string(e.Key)
}

// ReconcileResult mirrors the tagged SystemContext.ReconcileResult union.
type ReconcileResult struct {
	Tag        string // "Unchanged" | "Updated" | "ReplacementReady" | "ReplacementBlocked"
	Text       string
	Snapshot   Snapshot
	Generation *Generation
}

type rendered struct {
	text     string
	snapshot SourceSnapshot
}

type compared struct {
	tag    string // "Incompatible" | "Unchanged" | "Updated"
	render func() (rendered, error)
}

type loaded struct {
	unavailable bool
	baseline    func() (rendered, error)
	compare     func(previous json.RawMessage) (compared, error)
}

type packedSource struct {
	key  Key
	load func() (loaded, error)
}

// SystemContext is the opaque carrier for composable context sources.
type SystemContext struct {
	sources []packedSource
}

// Empty is the identity context.
var Empty = SystemContext{}

func context(sources []packedSource) SystemContext {
	return SystemContext{sources: sources}
}

// Make closes a typed source into a context that composes with differently typed
// sources.
func Make[A any](source Source[A]) SystemContext {
	return context([]packedSource{{
		key: source.Key,
		load: func() (loaded, error) {
			value, available, err := source.Load()
			if err != nil {
				return loaded{}, err
			}
			if !available {
				return loaded{unavailable: true}, nil
			}
			snapshot := func() (SourceSnapshot, error) {
				encoded, err := json.Marshal(value)
				if err != nil {
					return SourceSnapshot{}, err
				}
				result := SourceSnapshot{Value: encoded}
				if source.Removed != nil {
					removed, err := requireText(source.Key, "removal", source.Removed(value))
					if err != nil {
						return SourceSnapshot{}, err
					}
					result.Removed = &removed
				}
				return result, nil
			}
			return loaded{
				baseline: func() (rendered, error) {
					text, err := requireText(source.Key, "baseline", source.Baseline(value))
					if err != nil {
						return rendered{}, err
					}
					renderedSnapshot, err := snapshot()
					if err != nil {
						return rendered{}, err
					}
					return rendered{text: text, snapshot: renderedSnapshot}, nil
				},
				compare: func(previous json.RawMessage) (compared, error) {
					var decoded A
					if err := json.Unmarshal(previous, &decoded); err != nil {
						return compared{tag: "Incompatible"}, nil
					}
					if reflect.DeepEqual(decoded, value) {
						return compared{tag: "Unchanged"}, nil
					}
					return compared{tag: "Updated", render: func() (rendered, error) {
						text, err := requireText(source.Key, "update", source.Update(decoded, value))
						if err != nil {
							return rendered{}, err
						}
						renderedSnapshot, err := snapshot()
						if err != nil {
							return rendered{}, err
						}
						return rendered{text: text, snapshot: renderedSnapshot}, nil
					}}, nil
				},
			}, nil
		},
	}})
}

// Combine combines contexts in order and rejects duplicate source keys.
func Combine(values ...SystemContext) SystemContext {
	var sources []packedSource
	for _, value := range values {
		sources = append(sources, value.sources...)
	}
	if err := assertUniqueKeys(sources); err != nil {
		panic(err)
	}
	return context(sources)
}

type entry struct {
	key       Key
	available bool
	baseline  func() (rendered, error)
	compare   func(previous json.RawMessage) (compared, error)
}

func observe(value SystemContext) ([]entry, error) {
	entries := make([]entry, 0, len(value.sources))
	for _, source := range value.sources {
		loaded, err := source.load()
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry{
			key:       source.key,
			available: !loaded.unavailable,
			baseline:  loaded.baseline,
			compare:   loaded.compare,
		})
	}
	return entries, nil
}

// Initialize creates the immutable baseline and durable snapshot for a new
// generation.
func Initialize(value SystemContext) (Generation, error) {
	entries, err := observe(value)
	if err != nil {
		return Generation{}, err
	}
	var unavailableKeys []Key
	for _, entry := range entries {
		if !entry.available {
			unavailableKeys = append(unavailableKeys, entry.key)
		}
	}
	if len(unavailableKeys) > 0 {
		return Generation{}, &InitializationBlocked{Keys: unavailableKeys}
	}
	return initializeObservation(entries)
}

func initializeObservation(entries []entry) (Generation, error) {
	var parts []string
	snapshot := Snapshot{}
	for _, entry := range entries {
		if !entry.available {
			continue
		}
		result, err := entry.baseline()
		if err != nil {
			return Generation{}, err
		}
		parts = append(parts, result.text)
		snapshot[entry.key] = result.snapshot
	}
	return Generation{Baseline: render(parts), Snapshot: snapshot}, nil
}

// Reconcile reconciles current source values with one active generation.
func Reconcile(value SystemContext, previous Snapshot) (ReconcileResult, error) {
	entries, err := observe(value)
	if err != nil {
		return ReconcileResult{}, err
	}
	result, err := reconcileObservation(entries, previous)
	if err != nil {
		return ReconcileResult{}, err
	}
	if result.Tag == "Unchanged" || result.Tag == "Updated" {
		return result, nil
	}
	return replaceObservation(entries, previous)
}

func reconcileObservation(entries []entry, previous Snapshot) (ReconcileResult, error) {
	keys := map[Key]bool{}
	comparisons := map[Key]compared{}
	for _, entry := range entries {
		keys[entry.key] = true
		if !entry.available {
			continue
		}
		stored, ok := getSnapshot(previous, entry.key)
		if !ok {
			continue
		}
		compared, err := entry.compare(stored.Value)
		if err != nil {
			return ReconcileResult{}, err
		}
		if compared.tag == "Incompatible" {
			return replaceObservation(entries, previous)
		}
		comparisons[entry.key] = compared
	}
	for _, key := range sortedKeys(previous) {
		if keys[key] {
			continue
		}
		if previous[key].Removed == nil {
			return replaceObservation(entries, previous)
		}
	}

	snapshot := Snapshot{}
	var updates []string
	for _, entry := range entries {
		stored, ok := getSnapshot(previous, entry.key)
		if !entry.available {
			if ok {
				snapshot[entry.key] = stored
			}
			continue
		}
		if !ok {
			result, err := entry.baseline()
			if err != nil {
				return ReconcileResult{}, err
			}
			updates = append(updates, result.text)
			snapshot[entry.key] = result.snapshot
			continue
		}
		compared, ok := comparisons[entry.key]
		if !ok || compared.tag == "Incompatible" {
			return ReconcileResult{}, fmt.Errorf("Missing comparison for system context source %s", entry.key)
		}
		if compared.tag == "Unchanged" {
			snapshot[entry.key] = stored
			continue
		}
		result, err := compared.render()
		if err != nil {
			return ReconcileResult{}, err
		}
		updates = append(updates, result.text)
		snapshot[entry.key] = result.snapshot
	}
	for _, key := range sortedKeys(previous) {
		if keys[key] {
			continue
		}
		removed := previous[key].Removed
		if removed == nil {
			return ReconcileResult{}, fmt.Errorf("Missing removal rendering for system context source %s", key)
		}
		updates = append(updates, *removed)
	}
	if len(updates) == 0 {
		return ReconcileResult{Tag: "Unchanged"}, nil
	}
	return ReconcileResult{Tag: "Updated", Text: render(updates), Snapshot: snapshot}, nil
}

// Replace creates a complete replacement generation or blocks while admitted
// context is unavailable.
func Replace(value SystemContext, previous Snapshot) (ReconcileResult, error) {
	entries, err := observe(value)
	if err != nil {
		return ReconcileResult{}, err
	}
	return replaceObservation(entries, previous)
}

func replaceObservation(entries []entry, previous Snapshot) (ReconcileResult, error) {
	for _, entry := range entries {
		if !entry.available {
			if _, ok := getSnapshot(previous, entry.key); ok {
				return ReconcileResult{Tag: "ReplacementBlocked"}, nil
			}
		}
	}
	generation, err := initializeObservation(entries)
	if err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Tag: "ReplacementReady", Generation: &generation}, nil
}

func render(parts []string) string {
	return strings.Join(parts, "\n\n")
}

func getSnapshot(snapshot Snapshot, key Key) (SourceSnapshot, bool) {
	value, ok := snapshot[key]
	return value, ok
}

func requireText(key Key, kind, text string) (string, error) {
	if text == "" {
		return "", fmt.Errorf("System context source %s rendered an empty %s", key, kind)
	}
	return text, nil
}

func assertUniqueKeys(sources []packedSource) error {
	keys := map[Key]bool{}
	for _, source := range sources {
		if keys[source.key] {
			return &DuplicateKeyError{Key: source.key}
		}
		keys[source.key] = true
	}
	return nil
}

func sortedKeys(snapshot Snapshot) []Key {
	keys := make([]Key, 0, len(snapshot))
	for key := range snapshot {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
