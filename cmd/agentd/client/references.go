package client

import (
	"fmt"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

// ReferenceSources converts configured references into SDK reference sources.
// It validates the configuration first.
func ReferenceSources(refs []config.Reference) (map[string]core.ReferenceSource, error) {
	if err := config.ValidateReferences(refs); err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, nil
	}
	out := make(map[string]core.ReferenceSource, len(refs))
	for _, ref := range refs {
		switch ref.Type {
		case config.ReferenceLocal:
			out[ref.Name] = schema.ReferenceLocalSource{
				Type:        string(config.ReferenceLocal),
				Path:        schema.AbsolutePath(ref.Path),
				Description: optionalString(ref.Description),
				Hidden:      ref.Hidden,
			}
		case config.ReferenceGit:
			source := schema.ReferenceGitSource{
				Type:        string(config.ReferenceGit),
				Repository:  ref.Repository,
				Description: optionalString(ref.Description),
				Hidden:      ref.Hidden,
			}
			if ref.Branch != "" {
				branch := ref.Branch
				source.Branch = &branch
			}
			out[ref.Name] = source
		default:
			return nil, fmt.Errorf("reference %s: type must be local or git, got %q", ref.Name, ref.Type)
		}
	}
	return out, nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
