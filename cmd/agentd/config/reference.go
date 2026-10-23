package config

import (
	"errors"
	"fmt"
)

// ReferenceType selects how a reference source is obtained.
type ReferenceType string

const (
	// ReferenceLocal reads a host path.
	ReferenceLocal ReferenceType = "local"
	// ReferenceGit clones a git repository.
	ReferenceGit ReferenceType = "git"
)

// Valid reports whether t is a supported reference type.
func (t ReferenceType) Valid() bool {
	return t == ReferenceLocal || t == ReferenceGit
}

// Reference configures one named reference source (local path or git repository)
// injected into the system context.
type Reference struct {
	Name        string        `yaml:"name"`
	Type        ReferenceType `yaml:"type"`
	Path        string        `yaml:"path"`
	Repository  string        `yaml:"repository"`
	Branch      string        `yaml:"branch"`
	Description string        `yaml:"description"`
	Hidden      *bool         `yaml:"hidden"`
}

// ValidateReferences checks configured reference sources.
func ValidateReferences(refs []Reference) error {
	for _, ref := range refs {
		if ref.Name == "" {
			return errors.New("references: each requires a name")
		}
		switch ref.Type {
		case ReferenceLocal:
			if ref.Path == "" {
				return fmt.Errorf("reference %s: local requires path", ref.Name)
			}
		case ReferenceGit:
			if ref.Repository == "" {
				return fmt.Errorf("reference %s: git requires repository", ref.Name)
			}
		default:
			return fmt.Errorf("reference %s: type must be local or git, got %q", ref.Name, ref.Type)
		}
	}
	return nil
}
