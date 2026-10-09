package schema

// PluginID identifies a plugin.
type PluginID string

func (id PluginID) String() string { return string(id) }

// PluginEventAdded is published when a plugin finishes loading.
var PluginEventAdded = Define(DefineInput{
	Type:   "plugin.added",
	Schema: map[string]any{"id": nil},
})

var PluginEvent = struct {
	Added       EventDefinition
	Definitions []EventDefinition
}{
	Added:       PluginEventAdded,
	Definitions: Inventory(PluginEventAdded),
}
