package tool

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/core/util"
	"github.com/jiaweisky/go-agent-sdk/jsonschema"
)

const SkillName = "skill"

const skillFileLimit = 10

const SkillDescription = "Load a specialized skill when the task at hand matches one of the available skills " +
	"in the system context.\n" +
	"\n" +
	"Use this tool to inject the skill's instructions and resources into the current conversation. " +
	"The output may contain detailed workflow guidance as well as references to scripts, files, etc. " +
	"in the same directory as the skill.\n" +
	"\n" +
	"The skill name must match one of the available skills in the system context."

var SkillInputSchema = jsonschema.Object(
	jsonschema.Required(
		"name",
		jsonschema.String(jsonschema.Description("The name of the skill from the available skills list")),
	),
)

var SkillOutputSchema = jsonschema.Object(
	jsonschema.Required("name", jsonschema.String()),
	jsonschema.Required("directory", jsonschema.String()),
	jsonschema.Required("output", jsonschema.String()),
)

type SkillInput struct {
	Name string `json:"name"`
}

type SkillOutput struct {
	Name      string `json:"name"`
	Directory string `json:"directory"`
	Output    string `json:"output"`
}

// SkillLister is the narrow skill surface the tool depends on.
type SkillLister interface {
	List() ([]core.SkillInfo, error)
}

type SkillToolDeps struct {
	Skills     SkillLister
	Permission core.Permission
}

// SkillToModelOutput renders a loaded skill and its files for the model.
func SkillToModelOutput(skill core.SkillInfo, files []string) string {
	directory := filepath.Dir(string(skill.Location))
	lines := []string{
		fmt.Sprintf(`<skill_content name="%s">`, skill.Name),
		"# Skill: " + skill.Name,
		"",
		strings.TrimSpace(skill.Content),
		"",
		"Base directory for this skill: " + directory,
		"Relative paths in this skill (e.g., scripts/, reference/) are relative to this base directory.",
		"Note: file list is sampled.",
		"",
		"<skill_files>",
	}
	for _, file := range files {
		lines = append(lines, "<file>"+file+"</file>")
	}
	lines = append(lines, "</skill_files>", "</skill_content>")
	return strings.Join(lines, "\n")
}

func executeSkill(deps SkillToolDeps, skill core.SkillInfo, ctx Context) (SkillOutput, error) {
	source := &core.PermissionSource{
		Type:      core.PermissionSourceTypeTool,
		MessageID: ctx.AssistantMessageID,
		CallID:    ctx.ToolCallID,
	}
	if err := deps.Permission.Assert(core.PermissionAssertInput{
		SessionID: ctx.SessionID,
		Action:    SkillName,
		Resources: []string{skill.Name},
		Save:      []string{skill.Name},
		Source:    source,
		Agent:     &ctx.Agent,
	}); err != nil {
		return SkillOutput{}, err
	}

	directory := filepath.Dir(string(skill.Location))
	var files []string
	if filepath.Base(string(skill.Location)) == "SKILL.md" {
		matched, err := core.Glob("**/*", util.GlobOptions{
			Cwd: directory, Absolute: true, Include: "file", Dot: true,
		})
		if err != nil {
			return SkillOutput{}, err
		}
		filtered := make([]string, 0, len(matched))
		for _, file := range matched {
			if filepath.Base(file) != "SKILL.md" {
				filtered = append(filtered, file)
			}
		}
		sort.Strings(filtered)
		if len(filtered) > skillFileLimit {
			filtered = filtered[:skillFileLimit]
		}
		files = filtered
	}

	return SkillOutput{
		Name:      skill.Name,
		Directory: directory,
		Output:    SkillToModelOutput(skill, files),
	}, nil
}

func MakeSkill(deps SkillToolDeps) *Def[SkillInput, SkillOutput, SkillOutput] {
	return Make(Config[SkillInput, SkillOutput, SkillOutput]{
		Description: SkillDescription,
		Input:       SkillInputSchema,
		Output:      SkillOutputSchema,
		ToModelOutput: func(input SkillInput, output any) []Content {
			return []Content{TextContent{Text: DecodeStructured[SkillOutput](output).Output}}
		},
		Execute: func(execCtx context.Context, input SkillInput, ctx Context) (SkillOutput, error) {
			if err := execCtx.Err(); err != nil {
				return SkillOutput{}, err
			}
			current, err := deps.Skills.List()
			if err != nil {
				return SkillOutput{}, err
			}
			var found *core.SkillInfo
			for i := range current {
				if current[i].Name == input.Name {
					found = &current[i]
					break
				}
			}
			if found == nil {
				return SkillOutput{}, &Failure{Message: "Unable to load skill " + input.Name}
			}
			result, err := executeSkill(deps, *found, ctx)
			if err != nil {
				return SkillOutput{}, failure("Unable to load skill "+input.Name, err)
			}
			return result, nil
		},
	})
}

func RegisterSkill(tools Tools, deps SkillToolDeps) (func(), error) {
	return tools.Register(map[string]Tool{
		SkillName: MakeSkill(deps),
	})
}
