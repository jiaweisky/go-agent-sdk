package schema

// WorkspaceConnectionStatus mirrors WorkspaceEvent.ConnectionStatus.
type WorkspaceConnectionStatus struct {
	WorkspaceID WorkspaceID `json:"workspaceID"`
	Status      string      `json:"status"`
}

var WorkspaceEventReady = Define(DefineInput{
	Type:   "workspace.ready",
	Schema: map[string]any{"name": nil},
})

var WorkspaceEventFailed = Define(DefineInput{
	Type:   "workspace.failed",
	Schema: map[string]any{"message": nil},
})

var WorkspaceEventStatus = Define(DefineInput{
	Type:   "workspace.status",
	Schema: map[string]any{"workspaceID": nil, "status": nil},
})

var WorkspaceEvent = struct {
	Ready       EventDefinition
	Failed      EventDefinition
	Status      EventDefinition
	Definitions []EventDefinition
}{
	Ready:       WorkspaceEventReady,
	Failed:      WorkspaceEventFailed,
	Status:      WorkspaceEventStatus,
	Definitions: Inventory(WorkspaceEventReady, WorkspaceEventFailed, WorkspaceEventStatus),
}
