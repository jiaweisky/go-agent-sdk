package reference

import (
	"sort"
	"strings"

	"golang.org/x/text/language"

	"github.com/user/go-agent-sdk/core/systemcontext"
	"github.com/user/go-agent-sdk/core/util"
)

// Info mirrors Reference.Info (only the fields guidance needs).
type Info struct {
	Name        string
	Path        string
	Description *string
}

// Lister is the narrow Reference service surface guidance needs.
type Lister interface {
	List() ([]Info, error)
}

// Summary mirrors ReferenceGuidance.Summary.
type Summary struct {
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	Description *string `json:"description,omitempty"`
}

// Guidance mirrors the ReferenceGuidance service.
type Guidance struct {
	references Lister
	locale     language.Tag
}

func NewGuidance(references Lister, locale language.Tag) *Guidance {
	return &Guidance{references: references, locale: locale}
}

// Load mirrors ReferenceGuidance.load.
func (g *Guidance) Load() (systemcontext.SystemContext, error) {
	all, err := g.references.List()
	if err != nil {
		return systemcontext.SystemContext{}, err
	}
	available := make([]Summary, 0, len(all))
	for _, item := range all {
		if item.Description == nil {
			continue
		}
		available = append(available, Summary{Name: item.Name, Path: item.Path, Description: item.Description})
	}
	sort.Slice(available, func(i, j int) bool { return util.CompareLocale(available[i].Name, available[j].Name, g.locale) < 0 })
	if len(available) == 0 {
		return systemcontext.Empty, nil
	}

	return systemcontext.Make(systemcontext.Source[[]Summary]{
		Key:      "core/reference-guidance",
		Load:     func() ([]Summary, bool, error) { return available, true, nil },
		Baseline: renderReferenceGuidance,
		Update: func(_, current []Summary) string {
			return strings.Join([]string{
				"The available project references have changed. This list supersedes the previous reference list.",
				renderReferenceGuidance(current),
			}, "\n")
		},
		Removed: func(_ []Summary) string {
			return "Project reference guidance is no longer available. Do not use previously listed references."
		},
	}), nil
}

func renderReferenceGuidance(references []Summary) string {
	lines := []string{
		"Project references provide additional directories that can be accessed when relevant.",
		"<available_references>",
	}
	for _, reference := range references {
		lines = append(lines,
			"  <reference>",
			"    <name>"+reference.Name+"</name>",
			"    <path>"+reference.Path+"</path>",
		)
		if reference.Description != nil {
			lines = append(lines, "    <description>"+*reference.Description+"</description>")
		}
		lines = append(lines, "  </reference>")
	}
	lines = append(lines, "</available_references>")
	return strings.Join(lines, "\n")
}
