package session

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/jiaweisky/go-agent-sdk/internal/core/database"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

const sessionSelectColumns = "`id`,`project_id`,`workspace_id`,`parent_id`,`slug`,`directory`," +
	"`path`,`title`,`version`,`share_url`," +
	"`summary_additions`,`summary_deletions`,`summary_files`,`summary_diffs`,`metadata`,`cost`," +
	"`tokens_input`,`tokens_output`,`tokens_reasoning`,`tokens_cache_read`,`tokens_cache_write`," +
	"`revert`,`permission`,`agent`,`model`,`time_created`,`time_updated`,`time_compacting`,`time_archived`"

func scanSessionRow(db *database.Database, sessionID schema.SessionID) (*SessionRow, error) {
	row := db.DB.QueryRow(
		"SELECT "+sessionSelectColumns+" FROM `session` WHERE `id` = ?",
		string(sessionID),
	)
	var (
		result                                           SessionRow
		workspaceID, parentID, path, shareURL            sql.NullString
		summaryDiffs, metadata                           sql.NullString
		revert, permission, agent, model                 sql.NullString
		summaryAdditions, summaryDeletions, summaryFiles sql.NullInt64
		timeCompacting, timeArchived                     sql.NullInt64
	)
	err := row.Scan(
		&result.ID, &result.ProjectID, &workspaceID, &parentID, &result.Slug, &result.Directory, &path,
		&result.Title, &result.Version, &shareURL,
		&summaryAdditions, &summaryDeletions, &summaryFiles, &summaryDiffs, &metadata, &result.Cost,
		&result.TokensInput, &result.TokensOutput, &result.TokensReasoning, &result.TokensCacheRead, &result.TokensCacheWrite,
		&revert, &permission, &agent, &model, &result.TimeCreated, &result.TimeUpdated, &timeCompacting, &timeArchived,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	result.WorkspaceID = nullStringPtr(workspaceID)
	result.ParentID = nullStringPtr(parentID)
	result.Path = nullStringPtr(path)
	result.ShareURL = nullStringPtr(shareURL)
	result.SummaryDiffs = nullStringPtr(summaryDiffs)
	result.Metadata = nullStringPtr(metadata)
	result.Revert = nullStringPtr(revert)
	result.Permission = nullStringPtr(permission)
	result.Agent = nullStringPtr(agent)
	result.Model = nullStringPtr(model)
	result.SummaryAdditions = nullInt64Ptr(summaryAdditions)
	result.SummaryDeletions = nullInt64Ptr(summaryDeletions)
	result.SummaryFiles = nullInt64Ptr(summaryFiles)
	result.TimeCompacting = nullInt64Ptr(timeCompacting)
	result.TimeArchived = nullInt64Ptr(timeArchived)
	return &result, nil
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

// FromRow converts a stored session row into session info.
func FromRow(row SessionRow) schema.SessionInfo {
	info := schema.SessionInfo{
		ID:        schema.SessionID(row.ID),
		ProjectID: schema.ProjectID(row.ProjectID),
		Title:     row.Title,
		Cost:      row.Cost,
		Tokens: schema.SessionTokens{
			Input:     float64(row.TokensInput),
			Output:    float64(row.TokensOutput),
			Reasoning: float64(row.TokensReasoning),
			Cache: schema.SessionMessageCache{
				Read:  float64(row.TokensCacheRead),
				Write: float64(row.TokensCacheWrite),
			},
		},
		Location: schema.LocationRef{Directory: schema.AbsolutePath(row.Directory)},
		Time: schema.SessionTime{
			Created: schema.NonNegativeInt(row.TimeCreated),
			Updated: schema.NonNegativeInt(row.TimeUpdated),
		},
	}
	if truthy(row.ParentID) {
		id := schema.SessionID(*row.ParentID)
		info.ParentID = &id
	}
	if truthy(row.Agent) {
		id := schema.AgentID(*row.Agent)
		info.Agent = &id
	}
	if truthy(row.WorkspaceID) {
		id := schema.WorkspaceID(*row.WorkspaceID)
		info.Location.WorkspaceID = &id
	}
	if truthy(row.Path) {
		value := schema.RelativePath(*row.Path)
		info.Subpath = &value
	}
	if row.TimeArchived != nil && *row.TimeArchived != 0 {
		value := schema.NonNegativeInt(*row.TimeArchived)
		info.Time.Archived = &value
	}
	if truthy(row.Model) && *row.Model != "null" {
		var model struct {
			ID         string  `json:"id"`
			ProviderID string  `json:"providerID"`
			Variant    *string `json:"variant"`
		}
		if err := json.Unmarshal([]byte(*row.Model), &model); err == nil {
			variant := "default"
			if model.Variant != nil {
				variant = *model.Variant
			}
			variantID := schema.ModelVariantID(variant)
			ref := schema.ModelRef{
				ID:         schema.ModelID(model.ID),
				ProviderID: schema.ProviderID(model.ProviderID),
				Variant:    &variantID,
			}
			info.Model = &ref
		}
	}
	if truthy(row.Revert) && *row.Revert != "null" {
		var revert schema.RevertState
		if err := json.Unmarshal([]byte(*row.Revert), &revert); err == nil {
			info.Revert = &revert
		}
	}
	if truthy(row.Permission) && *row.Permission != "null" {
		var permission schema.PermissionRuleset
		if err := json.Unmarshal([]byte(*row.Permission), &permission); err == nil {
			info.Permission = &permission
		}
	}
	return info
}

func truthy(value *string) bool {
	return value != nil && *value != ""
}
