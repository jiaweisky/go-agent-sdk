package tool

// Tools is the narrow registration-only capability of a tool registry.
//
// It lets a caller register tools without depending on the full ToolRegistry
// (materialize/settle). *Registry implements Tools, and the RegisterXxx helpers
// accept Tools so registration code only needs this narrow capability.
type Tools interface {
	Register(tools map[string]Tool) (func(), error)
}

var _ Tools = (*Registry)(nil)
