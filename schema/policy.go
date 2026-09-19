package schema

// PolicyEffect mirrors Policy.Effect.
type PolicyEffect string

const (
	PolicyEffectAllow PolicyEffect = "allow"
	PolicyEffectDeny  PolicyEffect = "deny"
)

// PolicyInfo mirrors Policy.Info.
type PolicyInfo struct {
	Action   string       `json:"action"`
	Effect   PolicyEffect `json:"effect"`
	Resource string       `json:"resource"`
}
