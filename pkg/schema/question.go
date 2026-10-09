package schema

type QuestionID string

func (id QuestionID) String() string {
	return string(id)
}

func NewQuestionID() QuestionID {
	return QuestionID(generateID("que", false))
}

// ParseQuestionID validates an existing question id.
func ParseQuestionID(id string) (QuestionID, error) {
	if err := validateID("que", id); err != nil {
		return "", err
	}
	return QuestionID(id), nil
}

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type QuestionInfo struct {
	Question string           `json:"question"`
	Header   string           `json:"header"`
	Options  []QuestionOption `json:"options"`
	Multiple *bool            `json:"multiple,omitempty"`
	Custom   *bool            `json:"custom,omitempty"`
}

type QuestionPrompt struct {
	Question string           `json:"question"`
	Header   string           `json:"header"`
	Options  []QuestionOption `json:"options"`
	Multiple *bool            `json:"multiple,omitempty"`
}

type QuestionTool struct {
	MessageID string `json:"messageID"`
	CallID    string `json:"callID"`
}

type QuestionRequest struct {
	ID        QuestionID     `json:"id"`
	SessionID string         `json:"sessionID"`
	Questions []QuestionInfo `json:"questions"`
	Tool      *QuestionTool  `json:"tool,omitempty"`
}

type QuestionAnswer []string

type QuestionReply struct {
	Answers []QuestionAnswer `json:"answers"`
}

var QuestionEventAsked = Define(DefineInput{
	Type: "question.v2.asked",
	Schema: map[string]any{
		"id":        nil,
		"sessionID": nil,
		"questions": nil,
		"tool":      nil,
	},
})

var QuestionEventReplied = Define(DefineInput{
	Type: "question.v2.replied",
	Schema: map[string]any{
		"sessionID": nil,
		"requestID": nil,
		"answers":   nil,
	},
})

var QuestionEventRejected = Define(DefineInput{
	Type: "question.v2.rejected",
	Schema: map[string]any{
		"sessionID": nil,
		"requestID": nil,
	},
})

var QuestionEvent = struct {
	Asked       EventDefinition
	Replied     EventDefinition
	Rejected    EventDefinition
	Definitions []EventDefinition
}{
	Asked:       QuestionEventAsked,
	Replied:     QuestionEventReplied,
	Rejected:    QuestionEventRejected,
	Definitions: Inventory(QuestionEventAsked, QuestionEventReplied, QuestionEventRejected),
}
