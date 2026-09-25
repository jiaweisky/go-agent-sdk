package agent

import (
	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/core/reference"
	"github.com/user/go-agent-sdk/schema"
)

// noopSnapshots is the snapshotter used when the project has no git backing.
type noopSnapshots struct{}

func (noopSnapshots) Capture() (*core.SnapshotID, error) { return nil, nil }
func (noopSnapshots) Files(core.SnapshotCompareInput) ([]schema.RelativePath, error) {
	return nil, nil
}

// ruleEvaluator adapts core.PermissionEvaluate to skill's PermissionEvaluator.
type ruleEvaluator struct{}

func (ruleEvaluator) Evaluate(action, resource string, rules schema.PermissionRuleset) schema.PermissionRule {
	return core.PermissionEvaluate(action, resource, rules)
}

// referenceLister adapts *core.Reference to reference.Guidance's Lister.
type referenceLister struct{ reference *core.Reference }

func (l referenceLister) List() ([]reference.Info, error) {
	infos := l.reference.List()
	result := make([]reference.Info, 0, len(infos))
	for _, info := range infos {
		result = append(result, reference.Info{Name: info.Name, Path: string(info.Path), Description: info.Description})
	}
	return result, nil
}
