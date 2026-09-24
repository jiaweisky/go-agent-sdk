package utils

// IsRecord reports whether v is a JSON object.
func IsRecord(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}
