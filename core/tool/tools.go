package tool

// Tools is the narrow registration-only capability mirrored from Tools.Service
// in packages/core/src/tool/tools.ts.
//
// In opencode it lets a caller register tools without depending on the full
// ToolRegistry (materialize/settle). Here *Registry implements Tools, and the
// RegisterXxx helpers accept Tools so registration code only needs this narrow
// capability.
type Tools interface {
	Register(tools map[string]Tool) (func(), error)
}

var _ Tools = (*Registry)(nil)
