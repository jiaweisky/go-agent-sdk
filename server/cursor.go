package server

import (
	"encoding/base64"
	"encoding/json"
)

// cursorToken is the opaque pagination anchor: the id of a boundary item and the
// direction the client is paging.
type cursorToken struct {
	ID  string `json:"id"`
	Dir string `json:"dir"` // "next" | "previous"
}

func encodeCursor(id, dir string) string {
	data, err := json.Marshal(cursorToken{ID: id, Dir: dir})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

// decodeCursor parses an opaque cursor, returning nil for an absent or invalid
// value (callers then start from the beginning).
func decodeCursor(raw string) *cursorToken {
	if raw == "" {
		return nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil
	}
	var token cursorToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil
	}
	if token.Dir != "next" && token.Dir != "previous" {
		return nil
	}
	return &token
}

// cursorPage is the paged result plus the cursors to move either way.
type cursorPage[T any] struct {
	Items    []T
	Previous string
	Next     string
}

// paginate returns the page of items around the cursor anchor. items must be
// ordered; id returns each item's stable identifier. A nil cursor starts at the
// beginning; "next" returns items after the anchor and "previous" items before
// it.
func paginate[T any](items []T, id func(T) string, cursor *cursorToken, limit int) cursorPage[T] {
	start, end := 0, min(limit, len(items))
	if cursor != nil {
		idx := indexOf(items, id, cursor.ID)
		switch {
		case idx < 0: // anchor gone; fall back to the near edge
			if cursor.Dir == "previous" {
				start = max(0, len(items)-limit)
			}
			end = min(len(items), start+limit)
		case cursor.Dir == "previous":
			end = idx
			start = max(0, end-limit)
		default:
			start = idx + 1
			end = min(len(items), start+limit)
		}
	}
	page := cursorPage[T]{Items: items[start:end]}
	if len(page.Items) == 0 {
		return page
	}
	if start > 0 {
		page.Previous = encodeCursor(id(page.Items[0]), "previous")
	}
	if end < len(items) {
		page.Next = encodeCursor(id(page.Items[len(page.Items)-1]), "next")
	}
	return page
}

// nullable turns an empty cursor into nil so it serializes as JSON null.
func nullable(cursor string) any {
	if cursor == "" {
		return nil
	}
	return cursor
}

// clampLimit keeps limit within [1, max], falling back when it is not positive.
func clampLimit(limit, fallback, max int) int {
	if limit <= 0 || limit > max {
		return fallback
	}
	return limit
}

func indexOf[T any](items []T, id func(T) string, target string) int {
	for i, item := range items {
		if id(item) == target {
			return i
		}
	}
	return -1
}
