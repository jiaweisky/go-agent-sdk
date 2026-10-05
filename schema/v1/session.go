package v1

import "github.com/jiaweisky/go-agent-sdk/schema"

type MessageID string

func NewMessageID() MessageID {
	return MessageID("msg_" + schema.Ascending())
}

type PartID string

func NewPartID() PartID {
	return PartID("prt_" + schema.Ascending())
}

// V1SessionSummary is the v1 session summary: its diff statistics.
type V1SessionSummary struct {
	Additions float64               `json:"additions"`
	Deletions float64               `json:"deletions"`
	Files     float64               `json:"files"`
	Diffs     []schema.FileDiffInfo `json:"diffs,omitempty"`
}

type V1SessionCache struct {
	Read  float64 `json:"read"`
	Write float64 `json:"write"`
}

// V1SessionTokens is the v1 token counters.
type V1SessionTokens struct {
	Input     float64        `json:"input"`
	Output    float64        `json:"output"`
	Reasoning float64        `json:"reasoning"`
	Cache     V1SessionCache `json:"cache"`
}

type V1SessionShare struct {
	URL string `json:"url"`
}

// V1SessionRevert is the v1 revert pointer.
type V1SessionRevert struct {
	MessageID MessageID `json:"messageID"`
	PartID    *PartID   `json:"partID,omitempty"`
	Snapshot  *string   `json:"snapshot,omitempty"`
	Diff      *string   `json:"diff,omitempty"`
}

// V1SessionModel is the v1 model reference.
type V1SessionModel struct {
	ID         schema.ModelID    `json:"id"`
	ProviderID schema.ProviderID `json:"providerID"`
	Variant    *string           `json:"variant,omitempty"`
}

// V1SessionTime is the v1 session timestamps.
type V1SessionTime struct {
	Created    schema.NonNegativeInt  `json:"created"`
	Updated    schema.NonNegativeInt  `json:"updated"`
	Compacting *schema.NonNegativeInt `json:"compacting,omitempty"`
	Archived   *float64               `json:"archived,omitempty"`
}

// V1SessionInfo is a v1 session record.
type V1SessionInfo struct {
	ID          schema.SessionID    `json:"id"`
	Slug        string              `json:"slug"`
	ProjectID   schema.ProjectID    `json:"projectID"`
	WorkspaceID *schema.WorkspaceID `json:"workspaceID,omitempty"`
	Directory   string              `json:"directory"`
	Path        *string             `json:"path,omitempty"`
	ParentID    *schema.SessionID   `json:"parentID,omitempty"`
	Summary     *V1SessionSummary   `json:"summary,omitempty"`
	Cost        *float64            `json:"cost,omitempty"`
	Tokens      *V1SessionTokens    `json:"tokens,omitempty"`
	Share       *V1SessionShare     `json:"share,omitempty"`
	Title       string              `json:"title"`
	Agent       *string             `json:"agent,omitempty"`
	Model       *V1SessionModel     `json:"model,omitempty"`
	Version     string              `json:"version"`
	Metadata    map[string]any      `json:"metadata,omitempty"`
	Time        V1SessionTime       `json:"time"`
	Permission  *PermissionRuleset  `json:"permission,omitempty"`
	Revert      *V1SessionRevert    `json:"revert,omitempty"`
}

var v1SessionDurable = &schema.EventDurable{Aggregate: "sessionID", Version: 1}

func v1SessionEvent(eventType string) schema.EventDefinition {
	return schema.Define(schema.DefineInput{
		Type:    eventType,
		Durable: v1SessionDurable,
		Schema:  map[string]any{"sessionID": nil, "info": nil},
	})
}

var (
	V1EventCreated        = v1SessionEvent("session.created")
	V1EventUpdated        = v1SessionEvent("session.updated")
	V1EventDeleted        = v1SessionEvent("session.deleted")
	V1EventMessageUpdated = v1SessionEvent("message.updated")
)

var V1EventMessageRemoved = schema.Define(schema.DefineInput{
	Type:    "message.removed",
	Durable: v1SessionDurable,
	Schema:  map[string]any{"sessionID": nil, "messageID": nil},
})

var V1EventPartUpdated = schema.Define(schema.DefineInput{
	Type:    "message.part.updated",
	Durable: v1SessionDurable,
	Schema:  map[string]any{"sessionID": nil, "part": nil, "time": nil},
})

var V1EventPartRemoved = schema.Define(schema.DefineInput{
	Type:    "message.part.removed",
	Durable: v1SessionDurable,
	Schema:  map[string]any{"sessionID": nil, "messageID": nil, "partID": nil},
})

// V1SessionCreatedData is the payload of the v1 session created, updated and
// deleted events.
type V1SessionCreatedData struct {
	SessionID schema.SessionID `json:"sessionID"`
	Info      V1SessionInfo    `json:"info"`
}

// V1MessageUpdatedData is the payload of the v1 message.updated event. The message
// body is kept as an opaque JSON object (the v1 Message union is not ported yet).
type V1MessageUpdatedData struct {
	SessionID schema.SessionID `json:"sessionID"`
	Info      map[string]any   `json:"info"`
}

type V1MessageRemovedData struct {
	SessionID schema.SessionID `json:"sessionID"`
	MessageID MessageID        `json:"messageID"`
}

type V1PartUpdatedData struct {
	SessionID schema.SessionID `json:"sessionID"`
	Part      map[string]any   `json:"part"`
	Time      float64          `json:"time"`
}

type V1PartRemovedData struct {
	SessionID schema.SessionID `json:"sessionID"`
	MessageID MessageID        `json:"messageID"`
	PartID    PartID           `json:"partID"`
}
