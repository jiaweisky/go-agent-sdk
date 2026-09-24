package core

import (
	"fmt"
	"path/filepath"
)

type LocationMutationKind string

const (
	LocationMutationKindFile      LocationMutationKind = "file"
	LocationMutationKindDirectory LocationMutationKind = "directory"
)

type LocationMutationResolveInput struct {
	Path string
	Kind *LocationMutationKind
}

type LocationMutationExternalDirectoryAuthorization struct {
	Action    string
	Directory string
	Resource  string
	Save      string
}

type LocationMutationTarget struct {
	Canonical         string
	Resource          string
	ExternalDirectory *LocationMutationExternalDirectoryAuthorization
}

type LocationMutationPathErrorReason string

const (
	LocationMutationReasonRelativeEscape LocationMutationPathErrorReason = "relative_escape"
	LocationMutationReasonLocationEscape LocationMutationPathErrorReason = "location_escape"
	LocationMutationReasonNonDirAncestor LocationMutationPathErrorReason = "non_directory_ancestor"
)

type LocationMutationPathError struct {
	Path   string
	Reason LocationMutationPathErrorReason
}

func (e *LocationMutationPathError) Error() string {
	return fmt.Sprintf("path error: %s: %s", e.Path, e.Reason)
}

type locationMutationResolvedPath struct {
	Canonical string
	Type      *string
	Directory string
}

type LocationMutation interface {
	Resolve(input LocationMutationResolveInput) (*LocationMutationTarget, error)
}

func NewLocationMutation(location *Location) LocationMutation {
	return &locationMutation{location: location}
}

type locationMutation struct {
	location *Location
}

func (lm *locationMutation) Resolve(input LocationMutationResolveInput) (*LocationMutationTarget, error) {
	locationDir := string(lm.location.Directory)
	relative := !filepath.IsAbs(input.Path)
	var absolute string
	if relative {
		absolute = filepath.Join(locationDir, input.Path)
	} else {
		absolute = filepath.Clean(input.Path)
	}
	lexicallyInternal := Contains(locationDir, absolute)

	if relative && !lexicallyInternal {
		return nil, &LocationMutationPathError{Path: input.Path, Reason: LocationMutationReasonRelativeEscape}
	}

	locationRoot, err := RealPath(locationDir)
	if err != nil {
		return nil, err
	}

	resolved, err := lm.resolvePath(absolute)
	if err != nil {
		return nil, err
	}

	if lexicallyInternal && !Contains(locationRoot, resolved.Canonical) {
		return nil, &LocationMutationPathError{Path: input.Path, Reason: LocationMutationReasonLocationEscape}
	}

	external := !lexicallyInternal
	resource := resolved.Canonical
	if !external {
		rel, err := filepath.Rel(locationRoot, resolved.Canonical)
		if err != nil || rel == "" {
			rel = "."
		}
		resource = rel
	}

	var externalDir *LocationMutationExternalDirectoryAuthorization
	if external {
		dir := resolved.Directory
		if input.Kind != nil && *input.Kind == LocationMutationKindDirectory &&
			resolved.Type != nil && *resolved.Type == "Directory" {
			dir = resolved.Canonical
		}
		extResource := filepath.Join(dir, "*")
		externalDir = &LocationMutationExternalDirectoryAuthorization{
			Action:    "external_directory",
			Directory: dir,
			Resource:  extResource,
			Save:      extResource,
		}
	}

	return &LocationMutationTarget{
		Canonical:         resolved.Canonical,
		Resource:          resource,
		ExternalDirectory: externalDir,
	}, nil
}

func (lm *locationMutation) resolvePath(absolute string) (*locationMutationResolvedPath, error) {
	existing, err := RealPath(absolute)
	if err == nil {
		info, err2 := Stat(existing)
		if err2 == nil {
			dir := filepath.Dir(existing)
			if info.Type == "Directory" {
				dir = existing
			}
			return &locationMutationResolvedPath{Canonical: existing, Type: &info.Type, Directory: dir}, nil
		}
	}

	anchor := filepath.Dir(absolute)
	for {
		canonical, err := RealPath(anchor)
		if err == nil {
			info, err2 := Stat(canonical)
			if err2 == nil {
				if info.Type != "Directory" {
					return nil, &LocationMutationPathError{Path: absolute, Reason: LocationMutationReasonNonDirAncestor}
				}
				rel, _ := filepath.Rel(anchor, absolute)
				return &locationMutationResolvedPath{
					Canonical: filepath.Join(canonical, rel),
					Directory: canonical,
				}, nil
			}
		}
		parent := filepath.Dir(anchor)
		if parent == anchor {
			return nil, &LocationMutationPathError{Path: absolute, Reason: LocationMutationReasonNonDirAncestor}
		}
		anchor = parent
	}
}

type LocationMutationPermissionRequest struct {
	Action    string
	Resources []string
	Save      []string
}

func LocationMutationExternalDirectoryPermission(input LocationMutationExternalDirectoryAuthorization) LocationMutationPermissionRequest {
	return LocationMutationPermissionRequest{
		Action:    input.Action,
		Resources: []string{input.Resource},
		Save:      []string{input.Save},
	}
}
