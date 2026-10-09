package v1

import "github.com/jiaweisky/go-agent-sdk/pkg/schema"

type PermissionID string

func NewPermissionID() PermissionID {
	return PermissionID(schema.NewPermissionID())
}

type PermissionAction string

const (
	PermissionActionAllow PermissionAction = "allow"
	PermissionActionDeny  PermissionAction = "deny"
	PermissionActionAsk   PermissionAction = "ask"
)

type PermissionRule struct {
	Permission string           `json:"permission"`
	Pattern    string           `json:"pattern"`
	Action     PermissionAction `json:"action"`
}

type PermissionRuleset []PermissionRule

type PermissionTool struct {
	MessageID string `json:"messageID"`
	CallID    string `json:"callID"`
}

type PermissionRequest struct {
	ID         PermissionID     `json:"id"`
	SessionID  schema.SessionID `json:"sessionID"`
	Permission string           `json:"permission"`
	Patterns   []string         `json:"patterns"`
	Metadata   map[string]any   `json:"metadata"`
	Always     []string         `json:"always"`
	Tool       *PermissionTool  `json:"tool,omitempty"`
}

type PermissionReply string

const (
	PermissionReplyOnce   PermissionReply = "once"
	PermissionReplyAlways PermissionReply = "always"
	PermissionReplyReject PermissionReply = "reject"
)

type PermissionReplyBody struct {
	Reply   PermissionReply `json:"reply"`
	Message *string         `json:"message,omitempty"`
}

type PermissionApproval struct {
	ProjectID schema.ProjectID `json:"projectID"`
	Patterns  []string         `json:"patterns"`
}

type PermissionAskInput struct {
	ID         *PermissionID     `json:"id,omitempty"`
	SessionID  schema.SessionID  `json:"sessionID"`
	Permission string            `json:"permission"`
	Patterns   []string          `json:"patterns"`
	Metadata   map[string]any    `json:"metadata"`
	Always     []string          `json:"always"`
	Tool       *PermissionTool   `json:"tool,omitempty"`
	Ruleset    PermissionRuleset `json:"ruleset"`
}

type PermissionReplyInput struct {
	RequestID PermissionID `json:"requestID"`
	PermissionReplyBody
}

var PermissionEventAsked = schema.Define(schema.DefineInput{
	Type: "permission.asked",
	Schema: map[string]any{
		"id": nil, "sessionID": nil, "permission": nil, "patterns": nil,
		"metadata": nil, "always": nil, "tool": nil,
	},
})

var PermissionEventReplied = schema.Define(schema.DefineInput{
	Type: "permission.replied",
	Schema: map[string]any{
		"sessionID": nil, "requestID": nil, "reply": nil,
	},
})

var PermissionEvent = struct {
	Asked       schema.EventDefinition
	Replied     schema.EventDefinition
	Definitions []schema.EventDefinition
}{
	Asked:       PermissionEventAsked,
	Replied:     PermissionEventReplied,
	Definitions: schema.Inventory(PermissionEventAsked, PermissionEventReplied),
}
