package database

import "database/sql"

func migrate(db *sql.DB) error {
	statements := []string{
		"CREATE TABLE IF NOT EXISTS `project` (" +
			"`id` text PRIMARY KEY," +
			"`worktree` text NOT NULL," +
			"`vcs` text," +
			"`name` text," +
			"`icon_url` text," +
			"`icon_url_override` text," +
			"`icon_color` text," +
			"`time_created` integer NOT NULL," +
			"`time_updated` integer NOT NULL," +
			"`time_initialized` integer," +
			"`sandboxes` text NOT NULL," +
			"`commands` text" +
			")",
		"CREATE TABLE IF NOT EXISTS `project_directory` (" +
			"`project_id` text NOT NULL," +
			"`directory` text NOT NULL," +
			"`type` text," +
			"`strategy` text," +
			"`time_created` integer NOT NULL," +
			"CONSTRAINT `project_directory_pk` PRIMARY KEY(`project_id`, `directory`)," +
			"CONSTRAINT `fk_project_directory_project_id_project_id_fk` FOREIGN KEY (`project_id`) REFERENCES `project`(`id`) ON DELETE CASCADE" +
			")",
		"CREATE TABLE IF NOT EXISTS `event_sequence` (" +
			"`aggregate_id` text PRIMARY KEY," +
			"`seq` integer NOT NULL," +
			"`owner_id` text" +
			")",
		"CREATE TABLE IF NOT EXISTS `event` (" +
			"`id` text PRIMARY KEY," +
			"`aggregate_id` text NOT NULL," +
			"`seq` integer NOT NULL," +
			"`type` text NOT NULL," +
			"`data` text NOT NULL," +
			"CONSTRAINT `fk_event_aggregate_id_event_sequence_aggregate_id_fk` FOREIGN KEY (`aggregate_id`) REFERENCES `event_sequence`(`aggregate_id`) ON DELETE CASCADE" +
			")",
		"CREATE UNIQUE INDEX IF NOT EXISTS `event_aggregate_seq_idx` ON `event` (`aggregate_id`, `seq`)",
		"CREATE INDEX IF NOT EXISTS `event_aggregate_type_seq_idx` ON `event` (`aggregate_id`, `type`, `seq`)",
		"CREATE TABLE IF NOT EXISTS `session` (" +
			"`id` text PRIMARY KEY," +
			"`project_id` text NOT NULL," +
			"`workspace_id` text," +
			"`parent_id` text," +
			"`slug` text NOT NULL," +
			"`directory` text NOT NULL," +
			"`path` text," +
			"`title` text NOT NULL," +
			"`version` text NOT NULL," +
			"`share_url` text," +
			"`summary_additions` integer," +
			"`summary_deletions` integer," +
			"`summary_files` integer," +
			"`summary_diffs` text," +
			"`metadata` text," +
			"`cost` real NOT NULL DEFAULT 0," +
			"`tokens_input` integer NOT NULL DEFAULT 0," +
			"`tokens_output` integer NOT NULL DEFAULT 0," +
			"`tokens_reasoning` integer NOT NULL DEFAULT 0," +
			"`tokens_cache_read` integer NOT NULL DEFAULT 0," +
			"`tokens_cache_write` integer NOT NULL DEFAULT 0," +
			"`revert` text," +
			"`permission` text," +
			"`agent` text," +
			"`model` text," +
			"`time_created` integer NOT NULL," +
			"`time_updated` integer NOT NULL," +
			"`time_compacting` integer," +
			"`time_archived` integer," +
			"CONSTRAINT `fk_session_project_id_project_id_fk` FOREIGN KEY (`project_id`) REFERENCES `project`(`id`) ON DELETE CASCADE" +
			")",
		"CREATE INDEX IF NOT EXISTS `session_project_idx` ON `session` (`project_id`)",
		"CREATE INDEX IF NOT EXISTS `session_workspace_idx` ON `session` (`workspace_id`)",
		"CREATE INDEX IF NOT EXISTS `session_parent_idx` ON `session` (`parent_id`)",
		"CREATE TABLE IF NOT EXISTS `todo` (" +
			"`session_id` text NOT NULL," +
			"`content` text NOT NULL," +
			"`status` text NOT NULL," +
			"`priority` text NOT NULL," +
			"`position` integer NOT NULL," +
			"`time_created` integer NOT NULL," +
			"`time_updated` integer NOT NULL," +
			"CONSTRAINT `todo_pk` PRIMARY KEY(`session_id`, `position`)," +
			"CONSTRAINT `fk_todo_session_id_session_id_fk` FOREIGN KEY (`session_id`) REFERENCES `session`(`id`) ON DELETE CASCADE" +
			")",
		"CREATE INDEX IF NOT EXISTS `todo_session_idx` ON `todo` (`session_id`)",
		"CREATE TABLE IF NOT EXISTS `session_message` (" +
			"`id` text PRIMARY KEY," +
			"`session_id` text NOT NULL," +
			"`type` text NOT NULL," +
			"`seq` integer NOT NULL," +
			"`time_created` integer NOT NULL," +
			"`time_updated` integer NOT NULL," +
			"`data` text NOT NULL," +
			"CONSTRAINT `fk_session_message_session_id_session_id_fk` FOREIGN KEY (`session_id`) REFERENCES `session`(`id`) ON DELETE CASCADE" +
			")",
		"CREATE UNIQUE INDEX IF NOT EXISTS `session_message_session_seq_idx` ON `session_message` (`session_id`, `seq`)",
		"CREATE INDEX IF NOT EXISTS `session_message_session_type_seq_idx` ON `session_message` (`session_id`, `type`, `seq`)",
		"CREATE INDEX IF NOT EXISTS `session_message_session_time_created_id_idx` ON `session_message` (`session_id`, `time_created`, `id`)",
		"CREATE INDEX IF NOT EXISTS `session_message_time_created_idx` ON `session_message` (`time_created`)",
		"CREATE TABLE IF NOT EXISTS `session_context_epoch` (" +
			"`session_id` text PRIMARY KEY," +
			"`baseline` text NOT NULL," +
			"`snapshot` text NOT NULL," +
			"`baseline_seq` integer NOT NULL," +
			"CONSTRAINT `fk_session_context_epoch_session_id_session_id_fk` FOREIGN KEY (`session_id`) REFERENCES `session`(`id`) ON DELETE CASCADE" +
			")",
		"CREATE TABLE IF NOT EXISTS `session_input` (" +
			"`id` text PRIMARY KEY," +
			"`session_id` text NOT NULL," +
			"`prompt` text NOT NULL," +
			"`delivery` text NOT NULL," +
			"`admitted_seq` integer NOT NULL," +
			"`promoted_seq` integer," +
			"`time_created` integer NOT NULL," +
			"CONSTRAINT `fk_session_input_session_id_session_id_fk` FOREIGN KEY (`session_id`) REFERENCES `session`(`id`) ON DELETE CASCADE" +
			")",
		"CREATE INDEX IF NOT EXISTS `session_input_session_pending_delivery_seq_idx` ON `session_input` (`session_id`, `promoted_seq`, `delivery`, `admitted_seq`)",
		"CREATE UNIQUE INDEX IF NOT EXISTS `session_input_session_admitted_seq_idx` ON `session_input` (`session_id`, `admitted_seq`)",
		"CREATE UNIQUE INDEX IF NOT EXISTS `session_input_session_promoted_seq_idx` ON `session_input` (`session_id`, `promoted_seq`)",
		"CREATE TABLE IF NOT EXISTS `workspace` (" +
			"`id` text PRIMARY KEY," +
			"`type` text NOT NULL," +
			"`name` text NOT NULL DEFAULT ''," +
			"`branch` text," +
			"`directory` text," +
			"`extra` text," +
			"`project_id` text NOT NULL," +
			"`time_used` integer NOT NULL," +
			"CONSTRAINT `fk_workspace_project_id_project_id_fk` FOREIGN KEY (`project_id`) REFERENCES `project`(`id`) ON DELETE CASCADE" +
			")",
		"CREATE TABLE IF NOT EXISTS `message` (" +
			"`id` text PRIMARY KEY," +
			"`session_id` text NOT NULL," +
			"`time_created` integer NOT NULL," +
			"`time_updated` integer NOT NULL," +
			"`data` text NOT NULL," +
			"CONSTRAINT `fk_message_session_id_session_id_fk` FOREIGN KEY (`session_id`) REFERENCES `session`(`id`) ON DELETE CASCADE" +
			")",
		"CREATE INDEX IF NOT EXISTS `message_session_time_created_id_idx` ON `message` (`session_id`, `time_created`, `id`)",
		"CREATE TABLE IF NOT EXISTS `part` (" +
			"`id` text PRIMARY KEY," +
			"`message_id` text NOT NULL," +
			"`session_id` text NOT NULL," +
			"`time_created` integer NOT NULL," +
			"`time_updated` integer NOT NULL," +
			"`data` text NOT NULL," +
			"CONSTRAINT `fk_part_message_id_message_id_fk` FOREIGN KEY (`message_id`) REFERENCES `message`(`id`) ON DELETE CASCADE" +
			")",
		"CREATE INDEX IF NOT EXISTS `part_message_id_id_idx` ON `part` (`message_id`, `id`)",
		"CREATE INDEX IF NOT EXISTS `part_session_idx` ON `part` (`session_id`)",
		"CREATE TABLE IF NOT EXISTS `permission` (" +
			"`id` text PRIMARY KEY," +
			"`project_id` text NOT NULL," +
			"`action` text NOT NULL," +
			"`resource` text NOT NULL," +
			"`time_created` integer NOT NULL," +
			"`time_updated` integer NOT NULL," +
			"CONSTRAINT `fk_permission_project_id_project_id_fk` FOREIGN KEY (`project_id`) REFERENCES `project`(`id`) ON DELETE CASCADE" +
			")",
		"CREATE UNIQUE INDEX IF NOT EXISTS `permission_project_action_resource_idx` ON `permission` (`project_id`, `action`, `resource`)",
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}
