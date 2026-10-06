package server

import (
	agent "github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// sessionInfoDTO is the wire shape for a session (root SessionInfo lacks json
// tags and the model; schema.SessionInfo carries too much).
type sessionInfoDTO struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Agent       string `json:"agent,omitempty"`
	Model       any    `json:"model,omitempty"`
	Directory   string `json:"directory,omitempty"`
	ParentID    string `json:"parentID,omitempty"`
	TimeCreated int64  `json:"timeCreated"`
	TimeUpdated int64  `json:"timeUpdated"`
}

func fromRootSessionInfo(info agent.SessionInfo) sessionInfoDTO {
	return sessionInfoDTO{
		ID: info.ID, Title: info.Title, Agent: info.Agent, Directory: info.Directory,
		ParentID: info.ParentID, TimeCreated: info.TimeCreated, TimeUpdated: info.TimeUpdated,
	}
}

func fromSchemaSessionInfo(info schema.SessionInfo) sessionInfoDTO {
	dto := sessionInfoDTO{
		ID: string(info.ID), Title: info.Title, Directory: string(info.Location.Directory),
		TimeCreated: int64(info.Time.Created), TimeUpdated: int64(info.Time.Updated),
	}
	if info.Agent != nil {
		dto.Agent = string(*info.Agent)
	}
	if info.Model != nil {
		dto.Model = info.Model
	}
	if info.ParentID != nil {
		dto.ParentID = string(*info.ParentID)
	}
	return dto
}

type agentInfoDTO struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	Mode        string `json:"mode"`
	Hidden      bool   `json:"hidden"`
	Model       any    `json:"model,omitempty"`
	Backend     any    `json:"backend,omitempty"`
}

type skillInfoDTO struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type toolInfoDTO struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type permissionRequestDTO struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionID"`
	Agent     string         `json:"agent,omitempty"`
	Action    string         `json:"action"`
	Resources []string       `json:"resources"`
	Save      []string       `json:"save,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

func fromPermissionRequest(req agent.PermissionRequest) permissionRequestDTO {
	return permissionRequestDTO{
		ID: req.ID, SessionID: req.SessionID, Agent: req.Agent, Action: req.Action,
		Resources: req.Resources, Save: req.Save, Metadata: req.Metadata,
	}
}

type questionRequestDTO struct {
	ID        string                `json:"id"`
	SessionID string                `json:"sessionID"`
	Questions []schema.QuestionInfo `json:"questions"`
	Tool      any                   `json:"tool,omitempty"`
}

func fromQuestionRequest(req agent.QuestionRequest) questionRequestDTO {
	return questionRequestDTO{ID: req.ID, SessionID: req.SessionID, Questions: req.Questions, Tool: req.Tool}
}
