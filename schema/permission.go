package schema

type PermissionID string

func (id PermissionID) String() string {
	return string(id)
}

func NewPermissionID() PermissionID {
	return PermissionID(generateID("per", false))
}

// ParsePermissionID validates an existing permission id.
func ParsePermissionID(id string) (PermissionID, error) {
	if err := validateID("per", id); err != nil {
		return "", err
	}
	return PermissionID(id), nil
}

type PermissionSource struct {
	Type      string `json:"type"`
	MessageID string `json:"messageID"`
	CallID    string `json:"callID"`
}

const PermissionSourceTypeTool = "tool"

type PermissionRequest struct {
	ID        PermissionID      `json:"id"`
	SessionID string            `json:"sessionID"`
	Action    string            `json:"action"`
	Resources []string          `json:"resources"`
	Save      []string          `json:"save,omitempty"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
	Source    *PermissionSource `json:"source,omitempty"`
}

type PermissionReply string

const (
	PermissionReplyOnce   PermissionReply = "once"
	PermissionReplyAlways PermissionReply = "always"
	PermissionReplyReject PermissionReply = "reject"
)

type PermissionEffect string

const (
	PermissionEffectAllow PermissionEffect = "allow"
	PermissionEffectDeny  PermissionEffect = "deny"
	PermissionEffectAsk   PermissionEffect = "ask"
)

type PermissionRule struct {
	Action   string           `json:"action"`
	Resource string           `json:"resource"`
	Effect   PermissionEffect `json:"effect"`
}

type PermissionRuleset []PermissionRule

var PermissionEventAsked = Define(DefineInput{
	Type: "permission.v2.asked",
	Schema: map[string]any{
		"id":        nil,
		"sessionID": nil,
		"action":    nil,
		"resources": nil,
		"save":      nil,
		"metadata":  nil,
		"source":    nil,
	},
})

var PermissionEventReplied = Define(DefineInput{
	Type: "permission.v2.replied",
	Schema: map[string]any{
		"sessionID": nil,
		"requestID": nil,
		"reply":     nil,
	},
})

var PermissionEvent = struct {
	Asked       EventDefinition
	Replied     EventDefinition
	Definitions []EventDefinition
}{
	Asked:       PermissionEventAsked,
	Replied:     PermissionEventReplied,
	Definitions: Inventory(PermissionEventAsked, PermissionEventReplied),
}
