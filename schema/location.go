package schema

type LocationRef struct {
	Directory   AbsolutePath
	WorkspaceID *WorkspaceID
}

type LocationProject struct {
	ID        ProjectID
	Directory AbsolutePath
}

type LocationInfo struct {
	Directory   AbsolutePath
	WorkspaceID *WorkspaceID
	Project     LocationProject
}

type LocationResponse struct {
	Location LocationInfo
	Data     any
}

func NewLocationResponse(data any) LocationResponse {
	return LocationResponse{Data: data}
}
