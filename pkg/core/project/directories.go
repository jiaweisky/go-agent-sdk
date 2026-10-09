package project

import (
	"database/sql"
	"errors"
	"time"

	"github.com/jiaweisky/go-agent-sdk/internal/core/database"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

type Directory struct {
	Directory schema.AbsolutePath
	Strategy  *string
}

type CreateInput struct {
	ProjectID schema.ProjectID
	Directory schema.AbsolutePath
	Strategy  *string
	Behavior  *string
}

type RemoveInput struct {
	ProjectID schema.ProjectID
	Directory schema.AbsolutePath
}

type Directories interface {
	List(projectID schema.ProjectID) ([]Directory, error)
	Get(projectID schema.ProjectID, directory schema.AbsolutePath) (*Directory, error)
	Contains(projectID schema.ProjectID, directory schema.AbsolutePath) (bool, error)
	Create(input CreateInput) (bool, error)
	Remove(input RemoveInput) (bool, error)
}

type directoryStore struct {
	db *database.Database
}

func NewDirectories(db *database.Database) Directories {
	return &directoryStore{db: db}
}

func (s *directoryStore) List(projectID schema.ProjectID) ([]Directory, error) {
	rows, err := s.db.DB.Query(
		"SELECT directory, strategy FROM project_directory WHERE project_id = ? ORDER BY time_created DESC, directory ASC",
		string(projectID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Directory
	for rows.Next() {
		var directory string
		var strategy sql.NullString
		if err := rows.Scan(&directory, &strategy); err != nil {
			return nil, err
		}
		item := Directory{Directory: schema.AbsolutePath(directory)}
		if strategy.Valid {
			item.Strategy = &strategy.String
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *directoryStore) Get(projectID schema.ProjectID, directory schema.AbsolutePath) (*Directory, error) {
	var dir string
	var strategy sql.NullString
	err := s.db.DB.QueryRow(
		"SELECT directory, strategy FROM project_directory WHERE project_id = ? AND directory = ?",
		string(projectID), string(directory),
	).Scan(&dir, &strategy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item := &Directory{Directory: schema.AbsolutePath(dir)}
	if strategy.Valid {
		item.Strategy = &strategy.String
	}
	return item, nil
}

func (s *directoryStore) Contains(projectID schema.ProjectID, directory schema.AbsolutePath) (bool, error) {
	var dir string
	err := s.db.DB.QueryRow(
		"SELECT directory FROM project_directory WHERE project_id = ? AND directory = ?",
		string(projectID), string(directory),
	).Scan(&dir)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *directoryStore) Create(input CreateInput) (bool, error) {
	now := time.Now().UnixMilli()
	replace := input.Behavior != nil && *input.Behavior == "replace"

	if replace {
		var strategy any
		if input.Strategy != nil {
			strategy = *input.Strategy
		}
		var condition string
		if input.Strategy != nil {
			condition = "strategy IS NULL OR strategy != ?"
		} else {
			condition = "strategy IS NOT NULL"
		}
		args := []any{string(input.ProjectID), string(input.Directory), strategy, now}
		if input.Strategy != nil {
			args = append(args, *input.Strategy)
		}
		row := s.db.DB.QueryRow(
			"INSERT INTO project_directory (project_id, directory, strategy, time_created) VALUES (?, ?, ?, ?) "+
				"ON CONFLICT (project_id, directory) DO UPDATE SET strategy = excluded.strategy WHERE "+condition+
				" RETURNING directory",
			args...,
		)
		var dir string
		err := row.Scan(&dir)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return true, nil
	}

	row := s.db.DB.QueryRow(
		"INSERT INTO project_directory (project_id, directory, strategy, time_created) VALUES (?, ?, ?, ?) "+
			"ON CONFLICT (project_id, directory) DO NOTHING RETURNING directory",
		string(input.ProjectID), string(input.Directory), input.Strategy, now,
	)
	var dir string
	err := row.Scan(&dir)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *directoryStore) Remove(input RemoveInput) (bool, error) {
	row := s.db.DB.QueryRow(
		"DELETE FROM project_directory WHERE project_id = ? AND directory = ? RETURNING directory",
		string(input.ProjectID), string(input.Directory),
	)
	var dir string
	err := row.Scan(&dir)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
