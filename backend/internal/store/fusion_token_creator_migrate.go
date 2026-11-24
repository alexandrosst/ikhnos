package store

import "database/sql"

// migrateFusionTokenCreator records who made a FUSION token by account id (PRAGMA user_version 20), beside the username the
// audit trail shows. The list marks a token whose maker is no longer an administrator of the organisation, and a username
// cannot say that reliably: a deleted account frees its name for someone else. The column is nullable and existing rows keep
// NULL rather than being matched up by username, for the same reason; the list reports their maker as unknown.
func migrateFusionTokenCreator(db *sql.DB) error {
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	if v >= 20 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	has, err := hasColumn(tx, "fusion_tokens", "created_by_id")
	if err != nil {
		return err
	}
	if !has {
		if _, err := tx.Exec(`ALTER TABLE fusion_tokens ADD COLUMN created_by_id TEXT`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`PRAGMA user_version = 20`); err != nil {
		return err
	}
	return tx.Commit()
}
