package sqlite

import (
	"database/sql"
	"os"
	"runtime"
)

func IsRestrictedFileMode(mode os.FileMode) bool {
	if !mode.IsRegular() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return mode.Perm() == 0o600
}

func Close(database *sql.DB) error {
	if database == nil {
		return nil
	}
	database.SetMaxIdleConns(0)
	return database.Close()
}
