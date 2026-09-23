package tool

import (
	"errors"
	"testing"

	"github.com/user/go-agent-sdk/core"
)

func TestFailurePropagatesUserDecline(t *testing.T) {
	declined := &core.PermissionDeclinedError{}
	if got := failure("Unable to do the thing", declined); got != error(declined) {
		t.Fatalf("failure() = %v, want the decline to propagate", got)
	}
	rejected := &core.QuestionRejectedError{}
	if got := failure("Unable to do the thing", rejected); got != error(rejected) {
		t.Fatalf("failure() = %v, want the rejection to propagate", got)
	}

	// A wrapped decline still propagates.
	wrapped := &Failure{Message: "outer", Cause: declined}
	if got := failure("Unable to do the thing", wrapped); !errors.Is(got, error(declined)) {
		t.Fatalf("failure() = %v, want the wrapped decline", got)
	}

	// Any other error becomes a model-facing Failure.
	got := failure("Unable to do the thing", errors.New("boom"))
	var failed *Failure
	if !errors.As(got, &failed) || failed.Message != "Unable to do the thing" {
		t.Fatalf("failure() = %v, want a Failure", got)
	}
}
