package schema

type FileDiffStatus string

const (
	FileDiffStatusAdded    FileDiffStatus = "added"
	FileDiffStatusDeleted  FileDiffStatus = "deleted"
	FileDiffStatusModified FileDiffStatus = "modified"
)

type FileDiffInfo struct {
	File      *string         `json:"file,omitempty"`
	Patch     *string         `json:"patch,omitempty"`
	Additions int             `json:"additions"`
	Deletions int             `json:"deletions"`
	Status    *FileDiffStatus `json:"status,omitempty"`
}
