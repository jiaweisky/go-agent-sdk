package schema

type PermissionSavedID string

func (id PermissionSavedID) String() string {
	return string(id)
}

func NewPermissionSavedID() PermissionSavedID {
	return PermissionSavedID("psv_" + Ascending())
}

type PermissionSavedInfo struct {
	ID        PermissionSavedID
	ProjectID ProjectID
	Action    string
	Resource  string
}
