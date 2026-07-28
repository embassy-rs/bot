package migrate

import (
	"context"

	"embassy.dev/bot/toolkit/db"
	"github.com/sqlbunny/sqlbunny/runtime/migration"
)

func Run(store migration.Store) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		theDB, err := Wire()
		if err != nil {
			return err
		}

		ctx = db.Context(ctx, theDB)

		err = store.Run(ctx)
		if err != nil {
			return err
		}

		return nil
	}
}
