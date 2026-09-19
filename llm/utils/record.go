package utils

func IsRecord(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}
