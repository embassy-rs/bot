package migrate

import (
	"database/sql"

	"embassy.dev/bot/toolkit/db"
)

func Wire() (*sql.DB, error) {
	config, err := db.ConfigFromEnv()
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.New(config)
	if err != nil {
		return nil, err
	}
	return sqlDB, nil
}
