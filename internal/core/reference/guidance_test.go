package reference

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"golang.org/x/text/language"

	"github.com/jiaweisky/go-agent-sdk/internal/core/systemcontext"
)

type stubLister struct {
	info []Info
	err  error
}

func (s stubLister) List() ([]Info, error) { return s.info, s.err }

func mustLoad(t *testing.T, g *Guidance) systemcontext.SystemContext {
	t.Helper()
	ctx, err := g.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return ctx
}

func baseline(t *testing.T, g *Guidance) string {
	t.Helper()
	generation, err := systemcontext.Initialize(mustLoad(t, g))
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return generation.Baseline
}

func reconcile(t *testing.T, g *Guidance, previous systemcontext.Snapshot) systemcontext.ReconcileResult {
	t.Helper()
	result, err := systemcontext.Reconcile(mustLoad(t, g), previous)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	return result
}

func requireContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("output missing %q:\n%s", want, got)
	}
}

func requireNotContains(t *testing.T, got, want string) {
	t.Helper()
	if strings.Contains(got, want) {
		t.Fatalf("output unexpectedly contains %q:\n%s", want, got)
	}
}

func TestGuidanceLoadEmpty(t *testing.T) {
	got := baseline(t, NewGuidance(stubLister{}, language.English))
	if got != "" {
		t.Fatalf("baseline = %q, want empty", got)
	}
}

func TestGuidanceLoadRendersReferences(t *testing.T) {
	description := "project docs"
	g := NewGuidance(stubLister{info: []Info{
		{Name: "b", Path: "/b", Description: &description},
		{Name: "a", Path: "/a", Description: &description},
		{Name: "hidden", Path: "/h"}, // no description: skipped
	}}, language.English)

	got := baseline(t, g)
	requireContains(t, got, "<available_references>")
	requireContains(t, got, "</available_references>")
	requireContains(t, got, "<name>a</name>")
	requireContains(t, got, "<path>/a</path>")
	requireContains(t, got, "<description>project docs</description>")
	requireNotContains(t, got, "hidden")
	if strings.Index(got, "<name>a</name>") > strings.Index(got, "<name>b</name>") {
		t.Fatalf("references not locale-sorted:\n%s", got)
	}
}

func TestGuidanceLoadError(t *testing.T) {
	g := NewGuidance(stubLister{err: errors.New("boom")}, language.English)
	if _, err := g.Load(); err == nil {
		t.Fatal("expected List error to propagate")
	}
}

func TestGuidanceReconcile(t *testing.T) {
	description := "project docs"
	lister := &stubLister{info: []Info{{Name: "a", Path: "/a", Description: &description}}}
	g := NewGuidance(lister, language.English)

	generation, err := systemcontext.Initialize(mustLoad(t, g))
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}

	// The reference list changes: reconcile reports the superseding update.
	lister.info = []Info{{Name: "b", Path: "/b", Description: &description}}
	updated := reconcile(t, g, generation.Snapshot)
	if updated.Tag != "Updated" {
		t.Fatalf("updated = %+v", updated)
	}
	requireContains(t, updated.Text, "supersedes the previous reference list")
	requireContains(t, updated.Text, "<name>b</name>")

	// Every reference disappears: reconcile reports removal.
	lister.info = nil
	removal := reconcile(t, g, updated.Snapshot)
	if removal.Tag != "Updated" {
		t.Fatalf("removal = %+v", removal)
	}
	requireContains(t, removal.Text, "no longer available")
}

func TestSummaryJSONOmitsNilDescription(t *testing.T) {
	data, err := json.Marshal(Summary{Name: "a", Path: "/a"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	requireNotContains(t, string(data), "description")
}
