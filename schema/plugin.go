package schema

// PluginID mirrors Plugin.ID.
type PluginID string

func (id PluginID) String() string { return string(id) }

// PluginEventAdded mirrors Plugin.Event.Added.
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
