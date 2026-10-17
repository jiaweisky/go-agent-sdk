package core

import "fmt"

// PolicyViolation is a sandbox denial observed by the SDK. Certainty is
// "enforced" for an in-process check and "suspected" for a command whose stderr
// matched a platform denial pattern.
type PolicyViolation struct {
	// Action is read, write, file or network.
	Action string
	// Path is the denied file or socket, when known.
	Path string
	// Host is the denied network host, when known.
	Host string
	Rule string
	// Surface is "tool" for an in-process tool and "command" for a spawned
	// process.
	Surface string
	// Certainty is "enforced" or "suspected".
	Certainty string
}

// PolicyDeniedError is returned by a guard when a path is outside the sandbox
// policy. Tools wrap it in a model-facing Failure; the SDK reports the violation
// through the session event stream.
type PolicyDeniedError struct {
	Violation PolicyViolation
}

// Error implements error.
func (e *PolicyDeniedError) Error() string {
	if e.Violation.Rule != "" {
		return fmt.Sprintf("%s denied (%s)", e.Violation.Action, e.Violation.Rule)
	}
	return fmt.Sprintf("%s denied", e.Violation.Action)
}
