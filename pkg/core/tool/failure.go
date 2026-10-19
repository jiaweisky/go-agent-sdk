package tool

import (
	"errors"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

// failure wraps err as a model-facing Failure, unless err is a user decline
// (permission / question). Declining a prompt must halt the turn, not become
// tool output the model can react to, so a decline propagates unchanged. The
// cause is kept so the SDK can report a sandbox denial as a structured event.
func failure(message string, err error) error {
	if decline := userDecline(err); decline != nil {
		return decline
	}
	var blocked *core.PermissionBlockedError
	if errors.As(err, &blocked) {
		return &Failure{Message: message + ": " + blocked.Error(), Cause: err}
	}
	return &Failure{Message: sandboxMessage(message, err), Cause: err}
}

// sandboxMessage appends the sandbox reason to a tool failure so the model knows
// why it was blocked instead of retrying the same call.
func sandboxMessage(message string, err error) string {
	var denied *core.PolicyDeniedError
	if errors.As(err, &denied) {
		reason := denied.Violation.Rule
		if reason == "" {
			reason = denied.Violation.Action + " denied"
		}
		return message + ": blocked by sandbox (" + reason + ")"
	}
	return message
}

// userDecline returns err when it is a user decline (permission / question),
// otherwise nil.
func userDecline(err error) error {
	var declined *core.PermissionDeclinedError
	var rejected *core.QuestionRejectedError
	if errors.As(err, &declined) || errors.As(err, &rejected) {
		return err
	}
	return nil
}
