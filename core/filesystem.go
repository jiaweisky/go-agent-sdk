package core

// FileSystemContent is a file's content and its encoding
// (encoding is "utf8" or "base64").
type FileSystemContent struct {
	URI      string  `json:"uri"`
	Name     *string `json:"name,omitempty"`
	Content  string  `json:"content"`
	Encoding string  `json:"encoding"`
	MIME     string  `json:"mime"`
}

// FileSystemEntry is one entry of a directory listing
// (path is Location-relative).
type FileSystemEntry struct {
	Path string   `json:"path"`
	Type FileKind `json:"type"`
}

// FileSystemSubmatch is one capture group of a glob match.
type FileSystemSubmatch struct {
	Text  string `json:"text"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// FileSystemMatch is a glob match: its path and capture groups.
type FileSystemMatch struct {
	Entry      FileSystemEntry      `json:"entry"`
	Line       int                  `json:"line"`
	Offset     int                  `json:"offset"`
	Text       string               `json:"text"`
	Submatches []FileSystemSubmatch `json:"submatches"`
}
