package schema

type WorkspaceID string

func (id WorkspaceID) String() string {
	return string(id)
}

func NewWorkspaceID() WorkspaceID {
	return WorkspaceID(generateID("wrk", false))
}

// ParseWorkspaceID validates an existing workspace id.
func ParseWorkspaceID(id string) (WorkspaceID, error) {
	if err := validateID("wrk", id); err != nil {
		return "", err
	}
	return WorkspaceID(id), nil
}
