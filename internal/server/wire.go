package server

import (
	"database/sql"

	"embassy.dev/bot/internal/bot"
	ghapp "embassy.dev/bot/internal/github"
	"embassy.dev/bot/toolkit/db"
	tkserver "embassy.dev/bot/toolkit/server"
)

type app struct {
	db *sql.DB

	serverConfig *tkserver.Config
	githubConfig *ghapp.Config
	githubApp    *ghapp.App

	bot *bot.Bot
}

// Wire builds the app. Handwritten on purpose — there are five things to
// construct and a code generator would be more machinery than the job needs.
func Wire() (*app, error) {
	dbConfig, err := db.ConfigFromEnv()
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.New(dbConfig)
	if err != nil {
		return nil, err
	}

	serverConfig, err := tkserver.ConfigFromEnv()
	if err != nil {
		return nil, err
	}

	githubConfig, err := ghapp.ConfigFromEnv()
	if err != nil {
		return nil, err
	}
	githubApp, err := ghapp.New(githubConfig)
	if err != nil {
		return nil, err
	}

	botConfig, err := bot.ConfigFromEnv()
	if err != nil {
		return nil, err
	}

	return &app{
		db:           sqlDB,
		serverConfig: serverConfig,
		githubConfig: githubConfig,
		githubApp:    githubApp,
		bot:          bot.New(botConfig, githubApp),
	}, nil
}
