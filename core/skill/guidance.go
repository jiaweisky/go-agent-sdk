package skill

import (
	"sort"
	"strings"

	"golang.org/x/text/language"

	"github.com/user/go-agent-sdk/core/systemcontext"
	"github.com/user/go-agent-sdk/core/util"
	"github.com/user/go-agent-sdk/schema"
)

// Summary is a skill's name and description, for the system context.
type Summary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Lister is the narrow SkillV2 surface guidance needs.
type Lister interface {
	List() ([]schema.SkillInfo, error)
}

// PermissionEvaluator is the narrow PermissionV2 surface guidance needs.
type PermissionEvaluator interface {
	Evaluate(action, resource string, rules schema.PermissionRuleset) schema.PermissionRule
}

// Guidance renders the available skills into the system context.
type Guidance struct {
	skills      Lister
	permissions PermissionEvaluator
	locale      language.Tag
}

func NewGuidance(skills Lister, permissions PermissionEvaluator, locale language.Tag) *Guidance {
	return &Guidance{skills: skills, permissions: permissions, locale: locale}
}

// Load renders the skills the agent may use.
func (g *Guidance) Load(agent *schema.AgentInfo) (systemcontext.SystemContext, error) {
	if agent == nil {
		return systemcontext.Empty, nil
	}
	all, err := g.skills.List()
	if err != nil {
		return systemcontext.SystemContext{}, err
	}
	var permitted []schema.SkillInfo
	for _, item := range all {
		if g.permissions.Evaluate("skill", item.Name, agent.Permissions).Effect != schema.PermissionEffectDeny {
			permitted = append(permitted, item)
		}
	}
	if len(permitted) == 0 &&
		g.permissions.Evaluate("skill", "*", agent.Permissions).Effect == schema.PermissionEffectDeny {
		return systemcontext.Empty, nil
	}
	available := make([]Summary, 0, len(permitted))
	for _, item := range permitted {
		if item.Description == nil {
			continue
		}
		available = append(available, Summary{Name: item.Name, Description: *item.Description})
	}
	sort.Slice(available, func(i, j int) bool { return util.CompareLocale(available[i].Name, available[j].Name, g.locale) < 0 })

	return systemcontext.Make(systemcontext.Source[[]Summary]{
		Key:      "core/skill-guidance",
		Load:     func() ([]Summary, bool, error) { return available, true, nil },
		Baseline: renderSkillGuidance,
		Update: func(_, current []Summary) string {
			return strings.Join([]string{
				"The available skills have changed. This list supersedes the previous available skills list.",
				renderSkillGuidance(current),
			}, "\n")
		},
		Removed: func(_ []Summary) string {
			return "Skill guidance is no longer available. Do not use any previously listed skill."
		},
	}), nil
}

func renderSkillGuidance(skills []Summary) string {
	lines := []string{
		"Skills provide specialized instructions and workflows for specific tasks.",
		"Use the skill tool to load a skill when a task matches its description.",
	}
	if len(skills) == 0 {
		lines = append(lines, "No skills are currently available.")
	} else {
		lines = append(lines, "<available_skills>")
		for _, skill := range skills {
			lines = append(lines,
				"  <skill>",
				"    <name>"+skill.Name+"</name>",
				"    <description>"+skill.Description+"</description>",
				"  </skill>",
			)
		}
		lines = append(lines, "</available_skills>")
	}
	return strings.Join(lines, "\n")
}
