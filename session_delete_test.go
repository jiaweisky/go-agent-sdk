package agent

import (
	"context"
	"testing"
)

// TestDeleteSessionRemovesEventsAndChildren guards that deleting a session also
// removes its durable event log and its subagent child sessions, neither of
// which is covered by the session row's foreign-key cascades.
func TestDeleteSessionRemovesEventsAndChildren(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	session, err := client.CreateSession(ctx, &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	childID := "ses_delete_child"
	if _, err := client.rt.db.DB.Exec(
		"INSERT INTO `session` (`id`,`project_id`,`parent_id`,`slug`,`directory`,`title`,`version`,`revert`,`time_created`,`time_updated`) "+
			"VALUES (?,?,?,?,?,?,?,?,?,?)",
		childID, string(client.rt.projectID), session.ID(), "session", "/tmp", "child", "1", "null", 1, 1,
	); err != nil {
		t.Fatalf("insert child session: %v", err)
	}

	// Give the parent and the child a durable event, so the delete has to clear
	// the event log too.
	for _, id := range []string{session.ID(), childID} {
		if _, err := client.rt.db.DB.Exec(
			"INSERT INTO `event_sequence` (`aggregate_id`,`seq`,`owner_id`) VALUES (?,?,NULL)", id, 0,
		); err != nil {
			t.Fatalf("insert event_sequence %s: %v", id, err)
		}
		if _, err := client.rt.db.DB.Exec(
			"INSERT INTO `event` (`id`,`aggregate_id`,`seq`,`type`,`data`) VALUES (?,?,?,?,?)",
			"evt_"+id, id, 0, "test", "{}",
		); err != nil {
			t.Fatalf("insert event %s: %v", id, err)
		}
	}

	if err := client.DeleteSession(ctx, session.ID()); err != nil {
		t.Fatalf("delete: %v", err)
	}

	assertNoRows(t, client, "SELECT count(*) FROM `session` WHERE `id` IN (?,?)", session.ID(), childID)
	assertNoRows(t, client, "SELECT count(*) FROM `event` WHERE `aggregate_id` IN (?,?)", session.ID(), childID)
	assertNoRows(t, client, "SELECT count(*) FROM `event_sequence` WHERE `aggregate_id` IN (?,?)", session.ID(), childID)
}

func assertNoRows(t *testing.T, client *Client, query string, args ...any) {
	t.Helper()
	var remaining int
	if err := client.rt.db.DB.QueryRow(query, args...).Scan(&remaining); err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("query %q still has %d row(s)", query, remaining)
	}
}
