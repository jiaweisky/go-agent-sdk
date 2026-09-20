package schema

var SessionCompactionEventCompacted = Define(DefineInput{
	Type:   "session.compacted",
	Schema: map[string]any{"sessionID": nil},
})

var SessionCompactionEvent = struct {
	Compacted   EventDefinition
	Definitions []EventDefinition
}{
	Compacted:   SessionCompactionEventCompacted,
	Definitions: Inventory(SessionCompactionEventCompacted),
}
