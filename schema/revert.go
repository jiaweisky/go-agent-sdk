package schema

// RevertFileDiff mirrors Revert.FileDiff.
type RevertFileDiff struct {
	Path      RelativePath   `json:"path"`
	Status    string         `json:"status"`
	Additions NonNegativeInt `json:"additions"`
	Deletions NonNegativeInt `json:"deletions"`
	Patch     string         `json:"patch"`
}

// RevertState mirrors Revert.State.
type RevertState struct {
	MessageID MessageID        `json:"messageID"`
	PartID    *string          `json:"partID,omitempty"`
	Snapshot  *string          `json:"snapshot,omitempty"`
	Diff      *string          `json:"diff,omitempty"`
	Files     []RevertFileDiff `json:"files,omitempty"`
}
