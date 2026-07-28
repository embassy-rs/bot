package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"embassy.dev/bot/internal/migrations"
	"embassy.dev/bot/internal/web"
	"embassy.dev/bot/toolkit/db"
	"embassy.dev/bot/toolkit/log"
	"embassy.dev/bot/toolkit/process"
	tkserver "embassy.dev/bot/toolkit/server"
	"github.com/go-chi/chi/v5"
)

func Command(ctx context.Context) error {
	a, err := Wire()
	if err != nil {
		return err
	}

	ctx = db.Context(ctx, a.db)
	waitForMigrations(ctx)

	slug, err := a.githubApp.CheckAuth(ctx)
	if err != nil {
		return err
	}
	log.Infof(ctx, "authenticated with github", log.Fields{"app": slug})

	webServer := &tkserver.Server{
		Name:       "web",
		Config:     a.serverConfig,
		Middleware: []func(http.Handler) http.Handler{db.Middleware(a.db)},
		Timeout:    30 * time.Second,
		Router: func(r chi.Router) {
			r.Get("/", web.QueueHandler)
			r.Post("/webhook", a.bot.Webhook(a.githubConfig.WebhookSecret))
		},
	}

	return process.Run([]process.Process{
		{Name: "web", Fn: webServer.Run},
		{Name: "metrics", Fn: tkserver.MetricsServer(a.serverConfig).Run},
		{Name: "ticker", Fn: func(ctx context.Context) error {
			return a.bot.RunTicker(db.Context(ctx, a.db))
		}},
	})
}

// SyncCommand refreshes every known repo from the GitHub API. Handy after
// downtime, or after changing what "reviewable" means.
func SyncCommand(ctx context.Context) error {
	a, err := Wire()
	if err != nil {
		return err
	}

	ctx = db.Context(ctx, a.db)
	waitForMigrations(ctx)

	return a.bot.Sync(ctx)
}

func waitForMigrations(ctx context.Context) {
	log.Info(ctx, "waiting for migrations")

	var err error
	for i := 0; i < 10; i++ {
		err = migrations.Store.ValidateMigrated(ctx)
		if err == nil {
			log.Info(ctx, "migrations ok")
			return
		}
		log.Info(ctx, fmt.Sprintf("migrations not applied: %v", err))
		time.Sleep(2 * time.Second)
	}
	panic("timeout waiting for migrations to be applied")
}
