package schema

// SessionInputAdmitted is published when a prompt is admitted.
type SessionInputAdmitted struct {
	AdmittedSeq NonNegativeInt  `json:"admittedSeq"`
	ID          MessageID       `json:"id"`
	SessionID   SessionID       `json:"sessionID"`
	Prompt      Prompt          `json:"prompt"`
	Delivery    SessionDelivery `json:"delivery"`
	TimeCreated NonNegativeInt  `json:"timeCreated"`
	PromotedSeq *NonNegativeInt `json:"promotedSeq,omitempty"`
}
