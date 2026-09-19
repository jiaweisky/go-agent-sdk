package core

import (
	"github.com/user/go-agent-sdk/core/util"
	"github.com/user/go-agent-sdk/schema"
)

type (
	PolicyEffect = schema.PolicyEffect
	PolicyInfo   = schema.PolicyInfo
)

const (
	PolicyEffectAllow = schema.PolicyEffectAllow
	PolicyEffectDeny  = schema.PolicyEffectDeny
)

// Policy mirrors core/policy.ts: an ordered allow/deny statement list where the
// last matching statement wins.
type Policy interface {
	Load(statements []PolicyInfo)
	Evaluate(action, resource string, fallback PolicyEffect) PolicyEffect
	HasStatements() bool
}

type policyService struct {
	statements []PolicyInfo
}

func NewPolicy() Policy {
	return &policyService{}
}

func (p *policyService) Load(statements []PolicyInfo) {
	p.statements = statements
}

func (p *policyService) HasStatements() bool {
	return len(p.statements) > 0
}

func (p *policyService) Evaluate(action, resource string, fallback PolicyEffect) PolicyEffect {
	for i := len(p.statements) - 1; i >= 0; i-- {
		statement := p.statements[i]
		if util.Match(action, statement.Action) && util.Match(resource, statement.Resource) {
			return statement.Effect
		}
	}
	return fallback
}
