package schema

type SessionTokens struct {
	Input     float64             `json:"input"`
	Output    float64             `json:"output"`
	Reasoning float64             `json:"reasoning"`
	Cache     SessionMessageCache `json:"cache"`
}

type SessionTime struct {
	Created  NonNegativeInt  `json:"created"`
	Updated  NonNegativeInt  `json:"updated"`
	Archived *NonNegativeInt `json:"archived,omitempty"`
}

// SessionInfo mirrors SessionV2.Info.
type SessionInfo struct {
	ID        SessionID     `json:"id"`
	ParentID  *SessionID    `json:"parentID,omitempty"`
	ProjectID ProjectID     `json:"projectID"`
	Agent     *AgentID      `json:"agent,omitempty"`
	Model     *ModelRef     `json:"model,omitempty"`
	Cost      float64       `json:"cost"`
	Tokens    SessionTokens `json:"tokens"`
	Time      SessionTime   `json:"time"`
	Title     string        `json:"title"`
	Location  LocationRef   `json:"location"`
	Subpath   *RelativePath `json:"subpath,omitempty"`
	Revert    *RevertState  `json:"revert,omitempty"`
}

// SessionListAnchor mirrors Session.ListAnchor.
type SessionListAnchor struct {
	ID        SessionID `json:"id"`
	Time      float64   `json:"time"`
	Direction string    `json:"direction"`
}
