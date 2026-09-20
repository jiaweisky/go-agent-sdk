package schema

// SessionTodoInfo mirrors SessionTodo.Info.
type SessionTodoInfo struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

var SessionTodoEventUpdated = Define(DefineInput{
	Type: "todo.updated",
	Schema: map[string]any{
		"sessionID": nil,
		"todos":     nil,
	},
})

var SessionTodoEvent = struct {
	Updated     EventDefinition
	Definitions []EventDefinition
}{
	Updated:     SessionTodoEventUpdated,
	Definitions: Inventory(SessionTodoEventUpdated),
}
