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
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}
