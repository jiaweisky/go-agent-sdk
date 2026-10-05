package tool

import (
	"errors"

	"github.com/jiaweisky/go-agent-sdk/core"
)

// failure wraps err as a model-facing Failure, unless err is a user decline
// (permission / question). Declining a prompt must halt the turn, not become
// tool output the model can react to, so a decline propagates unchanged.
func failure(message string, err error) error {
	if decline := userDecline(err); decline != nil {
		return decline
	}
	return &Failure{Message: message}
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
