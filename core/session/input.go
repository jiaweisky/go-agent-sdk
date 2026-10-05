package session

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

// LifecycleConflict reports an input whose message already exists.
type LifecycleConflict struct {
	ID schema.MessageID
}

func (e *LifecycleConflict) Error() string {
	return fmt.Sprintf("SessionInput lifecycle conflict: %s", e.ID)
}

const inputSelectColumns = "`id`,`session_id`,`prompt`,`delivery`,`admitted_seq`,`promoted_seq`,`time_created`"

type rowScanner interface {
	Scan(dest ...any) error
}

func scanInputRow(scanner rowScanner) (SessionInputRow, error) {
	var row SessionInputRow
	var promoted sql.NullInt64
	if err := scanner.Scan(
		&row.ID, &row.SessionID, &row.Prompt, &row.Delivery, &row.AdmittedSeq, &promoted, &row.TimeCreated,
	); err != nil {
		return SessionInputRow{}, err
	}
	if promoted.Valid {
		value := promoted.Int64
		row.PromotedSeq = &value
	}
	return row, nil
}

func fromInputRow(row SessionInputRow) (schema.SessionInputAdmitted, error) {
	var prompt schema.Prompt
	if err := json.Unmarshal([]byte(row.Prompt), &prompt); err != nil {
		return schema.SessionInputAdmitted{}, err
	}
	admitted := schema.SessionInputAdmitted{
		AdmittedSeq: schema.NonNegativeInt(row.AdmittedSeq),
		ID:          schema.MessageID(row.ID),
		SessionID:   schema.SessionID(row.SessionID),
		Prompt:      prompt,
		Delivery:    schema.SessionDelivery(row.Delivery),
		TimeCreated: schema.NonNegativeInt(row.TimeCreated),
	}
	if row.PromotedSeq != nil {
		value := schema.NonNegativeInt(*row.PromotedSeq)
		admitted.PromotedSeq = &value
	}
	return admitted, nil
}

func encodePrompt(prompt schema.Prompt) string {
	data, err := json.Marshal(prompt)
	if err != nil {
		return ""
	}
	return string(data)
}

// Find returns the admitted input for a message id, or nil.
func Find(db DB, id schema.MessageID) (*schema.SessionInputAdmitted, error) {
	row := db.QueryRow("SELECT "+inputSelectColumns+" FROM `session_input` WHERE `id` = ?", string(id))
	inputRow, err := scanInputRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	admitted, err := fromInputRow(inputRow)
	if err != nil {
		return nil, err
	}
	return &admitted, nil
}

// AdmitInput describes a prompt to admit.
type AdmitInput struct {
	ID        schema.MessageID
	SessionID schema.SessionID
	Prompt    schema.Prompt
	Delivery  schema.SessionDelivery
}

// Admit records a prompt as admitted and publishes the event.
func Admit(db DB, events EventPublisher, input AdmitInput) (schema.SessionInputAdmitted, error) {
	existing, err := Find(db, input.ID)
	if err != nil {
		return schema.SessionInputAdmitted{}, err
	}
	if existing != nil {
		return *existing, nil
	}
	timestamp := time.Now().UnixMilli()
	durable, err := events.Publish(schema.SessionEventPromptAdmitted, map[string]any{
		"messageID": input.ID,
		"sessionID": input.SessionID,
		"timestamp": timestamp,
		"prompt":    input.Prompt,
		"delivery":  input.Delivery,
	})
	if err == nil && durable == nil {
		err = errors.New("Prompt admission event is missing aggregate sequence")
	}
	if err != nil {
		if stored, findErr := Find(db, input.ID); findErr == nil && stored != nil {
			return *stored, nil
		}
		return schema.SessionInputAdmitted{}, err
	}
	return schema.SessionInputAdmitted{
		AdmittedSeq: schema.NonNegativeInt(durable.Seq),
		ID:          input.ID,
		SessionID:   input.SessionID,
		Prompt:      input.Prompt,
		Delivery:    input.Delivery,
		TimeCreated: schema.NonNegativeInt(timestamp),
	}, nil
}

// ProjectAdmittedInput describes an admitted input to project.
type ProjectAdmittedInput struct {
	AdmittedSeq int64
	ID          schema.MessageID
	SessionID   schema.SessionID
	Prompt      schema.Prompt
	Delivery    schema.SessionDelivery
	TimeCreated int64
}

// ProjectAdmitted applies an admitted input to the stored session state.
func ProjectAdmitted(db DB, input ProjectAdmittedInput) error {
	var existing string
	err := db.QueryRow("SELECT `id` FROM `session_message` WHERE `id` = ?", string(input.ID)).Scan(&existing)
	if err == nil {
		return &LifecycleConflict{ID: input.ID}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	prompt, err := json.Marshal(input.Prompt)
	if err != nil {
		return err
	}
	result, err := db.Exec(
		"INSERT INTO `session_input` (`id`,`session_id`,`admitted_seq`,`prompt`,`delivery`,`time_created`) VALUES (?,?,?,?,?,?) ON CONFLICT DO NOTHING",
		string(input.ID), string(input.SessionID), input.AdmittedSeq, string(prompt), string(input.Delivery), input.TimeCreated,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return &LifecycleConflict{ID: input.ID}
	}
	return nil
}

// ProjectPromptedInput describes a prompted input to project.
type ProjectPromptedInput struct {
	ID          schema.MessageID
	SessionID   schema.SessionID
	Prompt      schema.Prompt
	Delivery    schema.SessionDelivery
	TimeCreated int64
	PromotedSeq int64
}

// ProjectPrompted applies a prompted input to the stored session state.
func ProjectPrompted(db DB, input ProjectPromptedInput) error {
	row := db.QueryRow(
		"UPDATE `session_input` SET `promoted_seq` = ? WHERE `id` = ? AND `session_id` = ? AND `promoted_seq` IS NULL RETURNING "+inputSelectColumns,
		input.PromotedSeq, string(input.ID), string(input.SessionID),
	)
	inputRow, err := scanInputRow(row)
	if err == nil {
		stored, err := fromInputRow(inputRow)
		if err != nil {
			return err
		}
		if !matchesProjection(stored, input) {
			return &LifecycleConflict{ID: input.ID}
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	stored, err := Find(db, input.ID)
	if err != nil {
		return err
	}
	if stored != nil {
		if !matchesProjection(*stored, input) || stored.PromotedSeq == nil || int64(*stored.PromotedSeq) != input.PromotedSeq {
			return &LifecycleConflict{ID: input.ID}
		}
		return nil
	}

	prompt, err := json.Marshal(input.Prompt)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		"INSERT INTO `session_input` (`id`,`session_id`,`prompt`,`delivery`,`admitted_seq`,`promoted_seq`,`time_created`) VALUES (?,?,?,?,?,?,?)",
		string(input.ID), string(input.SessionID), string(prompt), string(input.Delivery),
		input.PromotedSeq, input.PromotedSeq, input.TimeCreated,
	)
	return err
}

// HasPending reports whether an admitted input of that delivery is still waiting.
func HasPending(db DB, sessionID schema.SessionID, delivery schema.SessionDelivery) (bool, error) {
	var id string
	err := db.QueryRow(
		"SELECT `id` FROM `session_input` WHERE `session_id` = ? AND `promoted_seq` IS NULL AND `delivery` = ? LIMIT 1",
		string(sessionID), string(delivery),
	).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Equivalent reports whether an admitted input matches the same session, prompt
// and delivery.
func Equivalent(input schema.SessionInputAdmitted, sessionID schema.SessionID, prompt schema.Prompt, delivery schema.SessionDelivery) bool {
	return input.Delivery == delivery && matchesPrompt(input, sessionID, prompt)
}

func matchesPrompt(input schema.SessionInputAdmitted, sessionID schema.SessionID, prompt schema.Prompt) bool {
	return input.SessionID == sessionID && encodePrompt(input.Prompt) == encodePrompt(prompt)
}

func matchesProjection(stored schema.SessionInputAdmitted, input ProjectPromptedInput) bool {
	return Equivalent(stored, input.SessionID, input.Prompt, input.Delivery) &&
		int64(stored.TimeCreated) == input.TimeCreated
}

func publishInputs(db DB, events EventPublisher, sessionID schema.SessionID, rows []SessionInputRow) (int, error) {
	for _, row := range rows {
		id := schema.MessageID(row.ID)
		var prompt schema.Prompt
		if err := json.Unmarshal([]byte(row.Prompt), &prompt); err != nil {
			return 0, err
		}
		_, err := events.Publish(schema.SessionEventPrompted, map[string]any{
			"sessionID": sessionID,
			"timestamp": row.TimeCreated,
			"messageID": id,
			"prompt":    prompt,
			"delivery":  row.Delivery,
		})
		if err != nil {
			var conflict *LifecycleConflict
			if errors.As(err, &conflict) {
				stored, findErr := Find(db, id)
				if findErr == nil && stored != nil && stored.PromotedSeq != nil {
					continue
				}
			}
			return 0, err
		}
	}
	return len(rows), nil
}

// PromoteSteers promotes every steering input admitted up to the cutoff.
func PromoteSteers(db DB, events EventPublisher, sessionID schema.SessionID, cutoff int64) (int, error) {
	rows, err := db.Query(
		"SELECT "+inputSelectColumns+" FROM `session_input` WHERE `session_id` = ? AND `promoted_seq` IS NULL AND `delivery` = 'steer' AND `admitted_seq` <= ? ORDER BY `admitted_seq` ASC",
		string(sessionID), cutoff,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var pending []SessionInputRow
	for rows.Next() {
		row, err := scanInputRow(rows)
		if err != nil {
			return 0, err
		}
		pending = append(pending, row)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return publishInputs(db, events, sessionID, pending)
}

// PromoteNextQueued promotes the oldest queued input, reporting whether there was
// one.
func PromoteNextQueued(db DB, events EventPublisher, sessionID schema.SessionID) (bool, error) {
	row := db.QueryRow(
		"SELECT "+inputSelectColumns+" FROM `session_input` WHERE `session_id` = ? AND `promoted_seq` IS NULL AND `delivery` = 'queue' ORDER BY `admitted_seq` ASC LIMIT 1",
		string(sessionID),
	)
	inputRow, err := scanInputRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if _, err := publishInputs(db, events, sessionID, []SessionInputRow{inputRow}); err != nil {
		return false, err
	}
	return true, nil
}
