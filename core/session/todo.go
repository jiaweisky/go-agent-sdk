package session

import (
	"time"

	"github.com/jiaweisky/go-agent-sdk/core/database"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// Todo is the session todo service.
type Todo struct {
	db     *database.Database
	events EventPublisher
}

func NewTodo(db *database.Database, events EventPublisher) *Todo {
	return &Todo{db: db, events: events}
}

type todoUpdatedEvent struct {
	SessionID schema.SessionID         `json:"sessionID"`
	Todos     []schema.SessionTodoInfo `json:"todos"`
}

// Update replaces the session's todos and publishes SessionTodo.Event.Updated.
func (t *Todo) Update(sessionID schema.SessionID, todos []schema.SessionTodoInfo) error {
	tx, err := t.db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM `todo` WHERE `session_id` = ?", string(sessionID)); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	for position, todo := range todos {
		if _, err := tx.Exec(
			"INSERT INTO `todo` (`session_id`, `content`, `status`, `priority`, `position`, `time_created`, `time_updated`) "+
				"VALUES (?, ?, ?, ?, ?, ?, ?)",
			string(sessionID), todo.Content, todo.Status, todo.Priority, position, now, now,
		); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if t.events != nil {
		_, err := t.events.Publish(schema.SessionTodoEventUpdated, todoUpdatedEvent{SessionID: sessionID, Todos: todos})
		return err
	}
	return nil
}

// Get returns the session's todos ordered by position.
func (t *Todo) Get(sessionID schema.SessionID) ([]schema.SessionTodoInfo, error) {
	rows, err := t.db.DB.Query(
		"SELECT `content`, `status`, `priority` FROM `todo` WHERE `session_id` = ? ORDER BY `position` ASC",
		string(sessionID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var todos []schema.SessionTodoInfo
	for rows.Next() {
		var todo schema.SessionTodoInfo
		if err := rows.Scan(&todo.Content, &todo.Status, &todo.Priority); err != nil {
			return nil, err
		}
		todos = append(todos, todo)
	}
	return todos, rows.Err()
}
