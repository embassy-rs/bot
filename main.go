package main

import (
	"context"

	"embassy.dev/bot/internal/migrations"
	"embassy.dev/bot/internal/server"
	"embassy.dev/bot/toolkit/commands"
	"embassy.dev/bot/toolkit/migrate"
)

func main() {
	commands.Run(map[string]func(ctx context.Context) error{
		"migrate": migrate.Run(migrations.Store),
		"server":  server.Command,
		"sync":    server.SyncCommand,
	})
}
