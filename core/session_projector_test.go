package core

import (
	"strings"
	"testing"

	"github.com/user/go-agent-sdk/core/database"
	"github.com/user/go-agent-sdk/schema"
	v1 "github.com/user/go-agent-sdk/schema/v1"
)

func sessionProjectorTestEvents(t *testing.T) (*database.Database, func(schema.EventDefinition, map[string]any) schema.EventPayload) {
	t.Helper()
	db := eventTestDB(t)
	if _, err := db.DB.Exec(
		"INSERT INTO `project` (`id`,`worktree`,`sandboxes`,`time_created`,`time_updated`) VALUES ('p1','/project','[]',0,0)",
	); err != nil {
		t.Fatalf("project: %v", err)
	}
	if _, err := db.DB.Exec(
		"INSERT INTO `session` (`id`,`project_id`,`slug`,`directory`,`title`,`version`,`time_created`,`time_updated`) " +
			"VALUES ('ses_1','p1','s','/project','t','1',0,0)",
	); err != nil {
		t.Fatalf("session: %v", err)
	}
	events := NewEvent(EventConfig{DB: db.DB, Durable: schema.Durable(schema.SessionEventDurableDefinitions)})
	if err := RegisterSessionProjectors(events); err != nil {
		t.Fatalf("register: %v", err)
	}
	publish := func(def schema.EventDefinition, data map[string]any) schema.EventPayload {
		t.Helper()
		event, err := events.Publish(def, data, nil)
		if err != nil {
			t.Fatalf("publish %s: %v", def.Type, err)
		}
		return event
	}
	return db, publish
}

func TestSessionProjectorV1UpdatedKeepsAbsentFields(t *testing.T) {
	db := eventTestDB(t)
	if _, err := db.DB.Exec(
		"INSERT INTO `project` (`id`,`worktree`,`sandboxes`,`time_created`,`time_updated`) VALUES ('p1','/project','[]',0,0)",
	); err != nil {
		t.Fatalf("project: %v", err)
	}
	definitions := append([]schema.EventDefinition{}, schema.SessionEventDurableDefinitions...)
	definitions = append(definitions, v1.V1EventCreated, v1.V1EventUpdated)
	events := NewEvent(EventConfig{DB: db.DB, Durable: schema.Durable(definitions)})
	if err := RegisterSessionProjectors(events); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := events.Publish(v1.V1EventCreated, map[string]any{
		"sessionID": "ses_v2",
		"info": map[string]any{
			"id": "ses_v2", "slug": "s", "projectID": "p1", "directory": "/project", "title": "t", "version": "1",
			"model": map[string]any{"id": "m", "providerID": "p"},
			"time":  map[string]any{"created": 0, "updated": 0},
		},
	}, nil); err != nil {
		t.Fatalf("session.created: %v", err)
	}
	if _, err := events.Publish(v1.V1EventUpdated, map[string]any{
		"sessionID": "ses_v2",
		"info": map[string]any{
			"id": "ses_v2", "slug": "s2", "projectID": "p1", "directory": "/project", "title": "t2", "version": "2",
			"time": map[string]any{"created": 0, "updated": 0},
		},
	}, nil); err != nil {
		t.Fatalf("session.updated: %v", err)
	}
	var model *string
	var slug string
	if err := db.DB.QueryRow("SELECT `model`, `slug` FROM `session` WHERE `id` = 'ses_v2'").Scan(&model, &slug); err != nil {
		t.Fatalf("session: %v", err)
	}
	if slug != "s2" {
		t.Fatalf("slug = %q", slug)
	}
	if model == nil || !strings.Contains(*model, `"id":"m"`) {
		t.Fatalf("model should be preserved, got %v", model)
	}
}

func TestSessionProjectorV1CreatedAndMessage(t *testing.T) {
	db := eventTestDB(t)
	if _, err := db.DB.Exec(
		"INSERT INTO `project` (`id`,`worktree`,`sandboxes`,`time_created`,`time_updated`) VALUES ('p1','/project','[]',0,0)",
	); err != nil {
		t.Fatalf("project: %v", err)
	}
	definitions := append([]schema.EventDefinition{}, schema.SessionEventDurableDefinitions...)
	definitions = append(definitions,
		v1.V1EventCreated, v1.V1EventUpdated, v1.V1EventDeleted, v1.V1EventMessageUpdated,
		v1.V1EventMessageRemoved, v1.V1EventPartUpdated, v1.V1EventPartRemoved,
	)
	events := NewEvent(EventConfig{DB: db.DB, Durable: schema.Durable(definitions)})
	if err := RegisterSessionProjectors(events); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := events.Publish(v1.V1EventCreated, map[string]any{
		"sessionID": "ses_v1",
		"info": map[string]any{
			"id": "ses_v1", "slug": "s", "projectID": "p1", "directory": "/project", "title": "t", "version": "1",
			"time": map[string]any{"created": 0, "updated": 0},
		},
	}, nil); err != nil {
		t.Fatalf("session.created: %v", err)
	}
	var title string
	if err := db.DB.QueryRow("SELECT `title` FROM `session` WHERE `id` = 'ses_v1'").Scan(&title); err != nil {
		t.Fatalf("session: %v", err)
	}
	if title != "t" {
		t.Fatalf("title = %q", title)
	}

	if _, err := events.Publish(v1.V1EventMessageUpdated, map[string]any{
		"sessionID": "ses_v1",
		"info": map[string]any{
			"id": "msg_v1", "sessionID": "ses_v1", "role": "user",
			"time": map[string]any{"created": 5},
		},
	}, nil); err != nil {
		t.Fatalf("message.updated: %v", err)
	}
	var data string
	var timeCreated int64
	if err := db.DB.QueryRow(
		"SELECT `time_created`, `data` FROM `message` WHERE `id` = 'msg_v1'",
	).Scan(&timeCreated, &data); err != nil {
		t.Fatalf("message: %v", err)
	}
	if timeCreated != 5 {
		t.Fatalf("time_created = %d", timeCreated)
	}
	if !strings.Contains(data, `"role":"user"`) {
		t.Fatalf("data = %s", data)
	}
	if strings.Contains(data, `"id"`) || strings.Contains(data, `"sessionID"`) {
		t.Fatalf("data should omit id/sessionID: %s", data)
	}
}

func TestSessionProjectorProjectsMessages(t *testing.T) {
	db := eventTestDB(t)
	// Reuse the fixture DB by wiring events against it directly.
	if _, err := db.DB.Exec(
		"INSERT INTO `project` (`id`,`worktree`,`sandboxes`,`time_created`,`time_updated`) VALUES ('p1','/project','[]',0,0)",
	); err != nil {
		t.Fatalf("project: %v", err)
	}
	if _, err := db.DB.Exec(
		"INSERT INTO `session` (`id`,`project_id`,`slug`,`directory`,`title`,`version`,`time_created`,`time_updated`) " +
			"VALUES ('ses_1','p1','s','/project','t','1',0,0)",
	); err != nil {
		t.Fatalf("session: %v", err)
	}
	events := NewEvent(EventConfig{DB: db.DB, Durable: schema.Durable(schema.SessionEventDurableDefinitions)})
	if err := RegisterSessionProjectors(events); err != nil {
		t.Fatalf("register: %v", err)
	}
	publish := func(def schema.EventDefinition, data map[string]any) {
		t.Helper()
		if _, err := events.Publish(def, data, nil); err != nil {
			t.Fatalf("publish %s: %v", def.Type, err)
		}
	}
	model := map[string]any{"id": "m", "providerID": "p"}
	publish(schema.SessionEventAgentSwitched, map[string]any{"sessionID": "ses_1", "messageID": "msg_a", "timestamp": 1, "agent": "build"})
	publish(schema.SessionEventStepStarted, map[string]any{"sessionID": "ses_1", "assistantMessageID": "msg_asst", "timestamp": 2, "agent": "build", "model": model})
	publish(schema.SessionEventTextStarted, map[string]any{"sessionID": "ses_1", "assistantMessageID": "msg_asst", "timestamp": 3, "textID": "t1"})
	publish(schema.SessionEventTextEnded, map[string]any{"sessionID": "ses_1", "assistantMessageID": "msg_asst", "timestamp": 5, "textID": "t1", "text": "hello world"})
	publish(schema.SessionEventStepEnded, map[string]any{"sessionID": "ses_1", "assistantMessageID": "msg_asst", "timestamp": 6, "finish": "stop", "cost": 0, "tokens": map[string]any{"input": 0, "output": 0, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}}})

	rows, err := db.DB.Query("SELECT `id`,`type`,`data` FROM `session_message` WHERE `session_id` = 'ses_1' ORDER BY `seq` ASC")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	type row struct{ id, messageType, data string }
	var messages []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.messageType, &r.data); err != nil {
			t.Fatalf("scan: %v", err)
		}
		messages = append(messages, r)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %+v", messages)
	}
	if messages[0].messageType != "agent-switched" || messages[1].messageType != "assistant" {
		t.Fatalf("types = %q %q", messages[0].messageType, messages[1].messageType)
	}
	decoded, err := schema.DecodeSessionMessageWithMeta([]byte(messages[1].data), schema.MessageID(messages[1].id), "assistant")
	if err != nil {
		t.Fatalf("decode assistant: %v", err)
	}
	assistant := decoded.(schema.SessionMessageAssistant)
	if assistant.Finish == nil || *assistant.Finish != "stop" || assistant.Time.Completed == nil || *assistant.Time.Completed != 6 {
		t.Fatalf("assistant = %+v", assistant)
	}
	if len(assistant.Content) != 1 {
		t.Fatalf("content = %+v", assistant.Content)
	}
	text := assistant.Content[0].(schema.SessionMessageAssistantText)
	if text.Text != "hello world" {
		t.Fatalf("text = %q", text.Text)
	}
}

func TestSessionProjectorTypedPublish(t *testing.T) {
	db := eventTestDB(t)
	if _, err := db.DB.Exec(
		"INSERT INTO `project` (`id`,`worktree`,`sandboxes`,`time_created`,`time_updated`) VALUES ('p1','/project','[]',0,0)",
	); err != nil {
		t.Fatalf("project: %v", err)
	}
	if _, err := db.DB.Exec(
		"INSERT INTO `session` (`id`,`project_id`,`slug`,`directory`,`title`,`version`,`time_created`,`time_updated`) " +
			"VALUES ('ses_1','p1','s','/project','t','1',0,0)",
	); err != nil {
		t.Fatalf("session: %v", err)
	}
	events := NewEvent(EventConfig{DB: db.DB, Durable: schema.Durable(schema.SessionEventDurableDefinitions)})
	if err := RegisterSessionProjectors(events); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Publish a typed struct (in-process path), not a decoded map.
	if _, err := events.Publish(schema.SessionEventAgentSwitched, schema.SessionEventAgentSwitchedData{
		SessionEventEnvelope: schema.SessionEventEnvelope{Timestamp: 1, SessionID: "ses_1"},
		MessageID:            "msg_a",
		Agent:                "build",
	}, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM `session_message` WHERE `session_id` = 'ses_1'").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("session_message count = %d, want 1", count)
	}
}

func TestSessionProjectorPrompted(t *testing.T) {
	db, publish := sessionProjectorTestEvents(t)
	event := publish(schema.SessionEventPrompted, map[string]any{
		"sessionID": "ses_1", "messageID": "msg_u", "timestamp": 1,
		"prompt": map[string]any{"text": "hello"}, "delivery": "queue",
	})
	if event.Durable == nil {
		t.Fatalf("expected durable event")
	}
	var promoted int64
	if err := db.DB.QueryRow("SELECT `promoted_seq` FROM `session_input` WHERE `id` = 'msg_u'").Scan(&promoted); err != nil {
		t.Fatalf("session_input: %v", err)
	}
	if promoted != int64(event.Durable.Seq) {
		t.Fatalf("promoted_seq = %d, want %d", promoted, event.Durable.Seq)
	}
	var messageType string
	if err := db.DB.QueryRow("SELECT `type` FROM `session_message` WHERE `id` = 'msg_u'").Scan(&messageType); err != nil {
		t.Fatalf("session_message: %v", err)
	}
	if messageType != "user" {
		t.Fatalf("message type = %q", messageType)
	}
}
