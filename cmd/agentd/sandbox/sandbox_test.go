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
		process, err := Resolve(name, Policy{})
		if err != nil {
			t.Fatalf("resolve %q: %v", name, err)
		}
		if process != nil {
			t.Fatalf("resolve %q: expected the SDK default (nil)", name)
		}
	}
}

func TestResolveUnknownErrors(t *testing.T) {
	if _, err := Resolve("no-such-sandbox", Policy{}); err == nil {
		t.Fatal("expected an unknown sandbox name to be rejected")
	}
}

func TestResolveRegistered(t *testing.T) {
	Register("dummy", func(Policy) core.AppProcess { return dummyProcess{} })
	process, err := Resolve("dummy", Policy{})
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

func TestResolveInvalidViolationMode(t *testing.T) {
	Register("violation-mode-test", func(Policy) core.AppProcess { return dummyProcess{} })
	_, err := Resolve("violation-mode-test", Policy{ViolationMode: "bogus"})
	if err == nil {
		t.Fatal("expected an unknown violation mode to be rejected")
	}
}

func TestRegisterValidation(t *testing.T) {
	factory := func(Policy) core.AppProcess { return dummyProcess{} }
	probe := func() error { return nil }
	for _, name := range []string{"", "none"} {
		t.Run("reserved "+name, func(t *testing.T) {
			mustPanic(t, func() { Register(name, factory) })
		})
	}
	t.Run("nil factory", func(t *testing.T) {
		mustPanic(t, func() { Register("nil-factory-test", nil) })
	})
	t.Run("duplicate", func(t *testing.T) {
		Register("duplicate-register-test", factory)
		mustPanic(t, func() { Register("duplicate-register-test", factory) })
	})
	t.Run("nil probe", func(t *testing.T) {
		mustPanic(t, func() { RegisterAvailable("nil-probe-test", nil) })
	})
	t.Run("duplicate probe", func(t *testing.T) {
		RegisterAvailable("duplicate-probe-test", probe)
		mustPanic(t, func() { RegisterAvailable("duplicate-probe-test", probe) })
	})
}

func TestRegisterProcessValidation(t *testing.T) {
	noop := func(Policy, string, []string) error { return nil }
	for _, name := range []string{"", "none"} {
		t.Run("reserved "+name, func(t *testing.T) {
			mustPanic(t, func() { RegisterProcess(name, noop) })
		})
	}
	t.Run("nil launcher", func(t *testing.T) {
		mustPanic(t, func() { RegisterProcess("nil-launcher-test", nil) })
	})
	t.Run("duplicate", func(t *testing.T) {
		RegisterProcess("duplicate-process-test", noop)
		mustPanic(t, func() { RegisterProcess("duplicate-process-test", noop) })
	})
}

// mustPanic fails the test unless fn panics.
func mustPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic")
		}
	}()
	fn()
}
