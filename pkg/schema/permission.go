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

// PermissionRule is one permission rule. Evaluation takes the LAST matching
// rule across the merged rulesets, so rule order matters: a later rule wins on
// conflict.
//
// The deny effect carries two distinct behaviors depending on the resource
// shape:
//
//   - deny with resource "*" removes the matching tool from the model-facing
//     toolset entirely at materialization time (Registry.Materialize's
//     wholly-disabled check): the model never sees it, and a call to it fails
//     as an unknown tool. To delete a tool from an agent, prefer the explicit
//     AgentInfo.Tools allowlist over this form; this rule shape remains for
//     dynamic cases (session-level narrowing, config-driven rulesets).
//   - deny with a narrower resource keeps the tool listed and only rejects the
//     matching calls at execution time.
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
