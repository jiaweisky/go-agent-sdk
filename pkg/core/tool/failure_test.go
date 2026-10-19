package tool

import (
	"errors"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
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

	// A config-denied tool (PermissionBlockedError) is model-facing, not a
	// decline: the model sees the failure and the turn continues.
	blocked := failure("Permission denied: question", &core.PermissionBlockedError{
		Rules: core.PermissionRuleset{
			{Action: "question", Resource: "*", Effect: core.PermissionEffectDeny},
		},
	})
	var denied *Failure
	if !errors.As(blocked, &denied) ||
		!strings.Contains(denied.Message, "question") ||
		!strings.Contains(denied.Message, "denied by the current policy") {
		t.Fatalf("failure() = %v, want a model-facing Failure with the policy reason", blocked)
	}
}
