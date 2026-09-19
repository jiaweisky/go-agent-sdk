package core

// FileSystemContent mirrors the `Content` struct from
// packages/core/src/filesystem.ts (encoding is "utf8" or "base64").
type FileSystemContent struct {
	URI      string  `json:"uri"`
	Name     *string `json:"name,omitempty"`
	Content  string  `json:"content"`
	Encoding string  `json:"encoding"`
	MIME     string  `json:"mime"`
}

// FileSystemEntry mirrors `FileSystem.Entry` from
// packages/schema/src/filesystem.ts (path is Location-relative).
type FileSystemEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

// FileSystemSubmatch mirrors `FileSystem.Submatch` from
// packages/schema/src/filesystem.ts.
type FileSystemSubmatch struct {
	Text  string `json:"text"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// FileSystemMatch mirrors `FileSystem.Match` from
// packages/schema/src/filesystem.ts.
type FileSystemMatch struct {
	Entry      FileSystemEntry      `json:"entry"`
	Line       int                  `json:"line"`
	Offset     int                  `json:"offset"`
	Text       string               `json:"text"`
	Submatches []FileSystemSubmatch `json:"submatches"`
}
