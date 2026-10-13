package sandbox

import (
	"context"
	"iter"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

type dummyProcess struct{}

func (dummyProcess) Run(context.Context, string, []string, ...core.RunOptions) (*core.RunResult, error) {
	return &core.RunResult{}, nil
}

func (dummyProcess) RunStream(
	context.Context, string, []string, ...core.RunStreamOptions,
) iter.Seq2[string, error] {
	return func(func(string, error) bool) {}
}

func TestResolveNoneAndEmpty(t *testing.T) {
	for _, name := range []string{"", "none"} {
		process, err := Resolve(name)
		if err != nil {
			t.Fatalf("resolve %q: %v", name, err)
		}
		if process != nil {
			t.Fatalf("resolve %q: expected the SDK default (nil)", name)
		}
	}
}

func TestResolveUnknownErrors(t *testing.T) {
	if _, err := Resolve("no-such-sandbox"); err == nil {
		t.Fatal("expected an unknown sandbox name to be rejected")
	}
}

func TestResolveRegistered(t *testing.T) {
	Register("dummy", func() core.AppProcess { return dummyProcess{} })
	process, err := Resolve("dummy")
	if err != nil {
		t.Fatalf("resolve dummy: %v", err)
	}
	if process == nil {
		t.Fatal("expected a registered sandbox to return a process")
	}
}

func TestResolveProcessNoneAndEmpty(t *testing.T) {
	for _, name := range []string{"", "none"} {
		launcher, err := ResolveProcess(name)
		if err != nil || launcher != nil {
			t.Fatalf("resolve process %q = %v, %v (want nil, nil)", name, launcher, err)
		}
	}
}

func TestResolveProcessUnknownErrors(t *testing.T) {
	if _, err := ResolveProcess("no-such-wrapper"); err == nil {
		t.Fatal("expected an unknown whole-process wrapper to be rejected")
	}
}
