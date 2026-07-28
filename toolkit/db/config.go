package db

import (
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	DBHost                       string        `envconfig:"DB_HOST" required:"true"`
	DBPort                       int           `envconfig:"DB_PORT" default:"5432"`
	DBName                       string        `envconfig:"DB_NAME" required:"true"`
	DBUser                       string        `envconfig:"DB_USER" required:"true"`
	DBPassword                   string        `envconfig:"DB_PASSWORD" required:"true"`
	DBLogQueries                 bool          `envconfig:"DB_LOG_QUERIES" default:"false"`
	DBLogQueriesMinDuration      time.Duration `envconfig:"DB_LOG_QUERIES_MIN_DURATION" default:"1s"`
	DBLogTransactions            bool          `envconfig:"DB_LOG_TRANSACTIONS" default:"false"`
	DBLogTransactionsMinDuration time.Duration `envconfig:"DB_LOG_TRANSACTIONS_MIN_DURATION" default:"1s"`
	MaxConns                     int           `envconfig:"DB_MAX_CONNS" default:"6"`
}

func ConfigFromEnv() (*Config, error) {
	c := new(Config)
	err := envconfig.Process("config", c)
	if err != nil {
		return nil, err
	}
	return c, nil
}
