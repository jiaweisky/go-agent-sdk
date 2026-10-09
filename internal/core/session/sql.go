package session

// SessionRow is the Go equivalent of SessionTable.$inferSelect. The DDL lives in
// core/database/migration.go; this struct only covers the columns read by
// FromRow.
type SessionRow struct {
	ID               string
	ProjectID        string
	WorkspaceID      *string
	ParentID         *string
	Slug             string
	Directory        string
	Path             *string
	Title            string
	Version          string
	ShareURL         *string
	SummaryAdditions *int64
	SummaryDeletions *int64
	SummaryFiles     *int64
	SummaryDiffs     *string
	Metadata         *string
	Cost             float64
	TokensInput      int64
	TokensOutput     int64
	TokensReasoning  int64
	TokensCacheRead  int64
	TokensCacheWrite int64
	Revert           *string
	Permission       *string
	Agent            *string
	Model            *string
	TimeCreated      int64
	TimeUpdated      int64
	TimeCompacting   *int64
	TimeArchived     *int64
}

// SessionMessageRow is the Go equivalent of SessionMessageTable.$inferSelect.
type SessionMessageRow struct {
	ID          string
	SessionID   string
	Type        string
	Seq         int64
	TimeCreated int64
	TimeUpdated int64
	Data        string
}

// SessionInputRow is the Go equivalent of SessionInputTable.$inferSelect.
type SessionInputRow struct {
	ID          string
	SessionID   string
	Prompt      string
	Delivery    string
	AdmittedSeq int64
	PromotedSeq *int64
	TimeCreated int64
}
