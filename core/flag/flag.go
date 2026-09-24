// Package flag keeps only the pure helpers of flag.ts that the SDK uses.
//
// TS's flag module is the CLI's environment surface: it snapshots ~25
// OPENCODE_* / OTEL_* variables into a Flag table at module load and exposes an
// accessor per flag. The SDK has no consumer for any of that, and an embeddable
// library must not read the host environment on its own, so the table and its
// accessors are deliberately not ported. Callers that want the CLI convention
// read the variable they name themselves (see tool.WebSearchConfigFromEnv).
package flag

import (
	"os"
	"strings"
)

// Truthy reports whether the named environment variable is set to a truthy
// value ("true" or "1", case-insensitive).
func Truthy(key string) bool {
	value := strings.ToLower(os.Getenv(key))
	return value == "true" || value == "1"
}
