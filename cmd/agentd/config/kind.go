package config

// Kind selects a served transport.
type Kind string

const (
	// KindHTTP serves the HTTP/SSE API.
	KindHTTP Kind = "http"
	// KindA2A serves the A2A JSON-RPC API and agent cards.
	KindA2A Kind = "a2a"
)

// Valid reports whether k is a supported transport.
func (k Kind) Valid() bool {
	return k == KindHTTP || k == KindA2A
}
