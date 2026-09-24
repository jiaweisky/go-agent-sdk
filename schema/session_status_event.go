package schema

// SessionStatusRetryAction mirrors the retry action struct in SessionStatusEvent.Info.
type SessionStatusRetryAction struct {
	Reason   string  `json:"reason"`
	Provider string  `json:"provider"`
	Title    string  `json:"title"`
	Message  string  `json:"message"`
	Label    string  `json:"label"`
	Link     *string `json:"link,omitempty"`
}

type SessionStatusIdle struct {
	Type string `json:"type"`
}

func (SessionStatusIdle) SessionStatusInfoType() string { return "idle" }

type SessionStatusBusy struct {
	Type string `json:"type"`
}

func (SessionStatusBusy) SessionStatusInfoType() string { return "busy" }

type SessionStatusRetry struct {
	Type    string                    `json:"type"`
	Attempt NonNegativeInt            `json:"attempt"`
	Message string                    `json:"message"`
	Action  *SessionStatusRetryAction `json:"action,omitempty"`
	Next    NonNegativeInt            `json:"next"`
}

func (SessionStatusRetry) SessionStatusInfoType() string { return "retry" }

// SessionStatusInfo mirrors the tagged SessionStatusEvent.Info union.
type SessionStatusInfo interface {
	SessionStatusInfoType() string
}

var SessionStatusEventStatus = Define(DefineInput{
	Type:   "session.status",
	Schema: map[string]any{"sessionID": nil, "status": nil},
})

// SessionStatusEventIdle is deprecated: use SessionStatusEventStatus.
var SessionStatusEventIdle = Define(DefineInput{
	Type:   "session.idle",
	Schema: map[string]any{"sessionID": nil},
})

var SessionStatusEvent = struct {
	Status      EventDefinition
	Idle        EventDefinition
	Definitions []EventDefinition
}{
	Status:      SessionStatusEventStatus,
	Idle:        SessionStatusEventIdle,
	Definitions: Inventory(SessionStatusEventStatus, SessionStatusEventIdle),
}
