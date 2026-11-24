package store

import "database/sql"

// migrateFusionTokenApplications adds the list of ikhnos applications a FUSION token is limited to, and the members they had
// when it was made, which its rights never exceed (PRAGMA user_version 21). Existing tokens get [] for both - no such limit -
// which is what they had: they keep working exactly as before. The columns are NOT NULL with that default, and are read as
// strictly as the namespace and cluster lists (jsonStrings, jsonMembers): a JSON null in either is unreadable and the token is
// refused, never taken as unlimited.
//
// Each column is looked for whatever the version says, so that a database a draft of version 21 wrote without one of them
// gets it too; the version is set only when it is below 21.
func migrateFusionTokenApplications(db *sql.DB) error {
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	if v > 21 {
		return nil // (a newer build's database is refused before this runs; nothing here is its to change)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, col := range []string{"applications", "application_members"} {
		has, err := hasColumn(tx, "fusion_tokens", col)
		if err != nil {
			return err
		}
		if !has {
			if _, err := tx.Exec(`ALTER TABLE fusion_tokens ADD COLUMN ` + col + ` TEXT NOT NULL DEFAULT '[]'`); err != nil {
				return err
			}
		}
	}
	if v < 21 {
		if _, err := tx.Exec(`PRAGMA user_version = 21`); err != nil {
			return err
		}
	}
	return tx.Commit()
}
