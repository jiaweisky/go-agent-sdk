package schema

// RevertFileDiff is one file's diff in a revert.
type RevertFileDiff struct {
	Path      RelativePath   `json:"path"`
	Status    string         `json:"status"`
	Additions NonNegativeInt `json:"additions"`
	Deletions NonNegativeInt `json:"deletions"`
	Patch     string         `json:"patch"`
}

// RevertState is a staged revert.
type RevertState struct {
	MessageID MessageID        `json:"messageID"`
	PartID    *string          `json:"partID,omitempty"`
	Snapshot  *string          `json:"snapshot,omitempty"`
	Diff      *string          `json:"diff,omitempty"`
	Files     []RevertFileDiff `json:"files,omitempty"`
}
