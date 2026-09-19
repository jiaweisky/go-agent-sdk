package config

import "github.com/user/go-agent-sdk/schema"

// ExperimentalPolicy mirrors ConfigExperimental.Policy.
type ExperimentalPolicy = schema.PolicyInfo

// ExperimentalInfo mirrors ConfigExperimental.Experimental.
type ExperimentalInfo struct {
	Policies []ExperimentalPolicy `json:"policies,omitempty"`
}
