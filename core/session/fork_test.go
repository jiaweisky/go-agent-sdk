package session

import (
	"encoding/json"
	"testing"
)

// TestRebindSessionID guards that only sessionID changes: every other field,
// including numeric time values, is preserved byte-for-byte.
func TestRebindSessionID(t *testing.T) {
	data := `{"sessionID":"ses_old","text":"hi","time":{"created":1759000000000}}`
	got, err := rebindSessionID(data, "ses_new")
	if err != nil {
		t.Fatalf("rebind: %v", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(got), &object); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(object["sessionID"]) != `"ses_new"` {
		t.Fatalf("sessionID = %s, want %q", object["sessionID"], "ses_new")
	}
	if string(object["time"]) != `{"created":1759000000000}` {
		t.Fatalf("time = %s, want it unchanged", object["time"])
	}
}
