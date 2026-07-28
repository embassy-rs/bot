package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"embassy.dev/bot/toolkit/asynccontext"
	"embassy.dev/bot/toolkit/log"
	"github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sqlbunny/errors"
	"github.com/sqlbunny/sqlbunny/runtime/bunny"
)

func Context(ctx context.Context, db bunny.DB) context.Context {
	return asynccontext.WithValue(ctx, bunny.ContextDBKey, db)
}

func From(ctx context.Context) bunny.DB {
	return bunny.DBFromContext(ctx)
}

type noQueryLogKey struct{}

func NoQueryLog(ctx context.Context) context.Context {
	return asynccontext.WithValue(ctx, noQueryLogKey{}, struct{}{})
}

func isQueryLog(ctx context.Context) bool {
	return ctx.Value(noQueryLogKey{}) == nil
}

type metrics struct {
	queriesTotal         prometheus.Counter
	queriesDuration      prometheus.Histogram
	transactionsTotal    prometheus.Counter
	transactionsDuration prometheus.Histogram
}

var theMetrics = metrics{
	queriesTotal: promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "db_queries_total",
			Help: "Total number of db queries.",
		},
	),
	queriesDuration: promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "db_queries_duration_seconds",
			Help:    "DB query duration.",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 20),
		},
	),
	transactionsTotal: promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "db_transactions_total",
			Help: "Total number of db transactions.",
		},
	),
	transactionsDuration: promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "db_transactions_duration_seconds",
			Help:    "DB transaction duration.",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 20),
		},
	),
}

type queryLogger struct {
	queries                 bool
	queriesMinDuration      time.Duration
	transactions            bool
	transactionsMinDuration time.Duration
}

func (l *queryLogger) LogQuery(ctx context.Context, info bunny.QueryLogInfo) {
	theMetrics.queriesTotal.Inc()
	theMetrics.queriesDuration.Observe(info.Duration.Seconds())

	if isQueryLog(ctx) && l.queries && info.Duration >= l.queriesMinDuration {
		log.Infof(ctx, "query", log.Fields{
			"query":    info.Query,
			"args":     info.Args,
			"duration": info.Duration,
			"err":      info.Err,
		})
	}
}

func (l *queryLogger) LogBegin(ctx context.Context, info bunny.BeginLogInfo) context.Context {
	if info.ReadOnly {
		ctx = log.With(ctx, log.Fields{"dbtx": "ro"})
	} else {
		ctx = log.With(ctx, log.Fields{"dbtx": "rw"})
	}
	if isQueryLog(ctx) && l.transactions && l.transactionsMinDuration == 0 {
		log.Info(ctx,
			"begin",
		)
	}
	return ctx
}

func (l *queryLogger) LogCommit(ctx context.Context, info bunny.CommitLogInfo) {
	theMetrics.transactionsTotal.Inc()
	theMetrics.transactionsDuration.Observe(info.Duration.Seconds())
	if isQueryLog(ctx) && l.transactions && info.Duration >= l.transactionsMinDuration {
		log.Infof(ctx, "commit", log.Fields{
			"duration": info.Duration,
		})
	}
}

func (l *queryLogger) LogRollback(ctx context.Context, info bunny.RollbackLogInfo) {
	theMetrics.transactionsTotal.Inc()
	theMetrics.transactionsDuration.Observe(info.Duration.Seconds())
	if isQueryLog(ctx) && l.transactions && info.Duration >= l.transactionsMinDuration {
		log.Infof(ctx, "rollback", log.Fields{
			"duration": info.Duration,
			"err":      info.Err,
		})
	}
}

func waitForConnection(db *sql.DB) error {
	ctx := context.Background()
	log.Info(ctx, "Waiting for DB connection")
	var err error
	// Wait up to 1 minute
	for i := 0; i < 60; i++ {
		ctx2, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err = db.ExecContext(ctx2, "SELECT 1+1")
		cancel()
		if err == nil {
			log.Info(ctx, "DB connection OK")
			return nil
		}
		log.Infof(ctx, "DB connection fail", log.Fields{
			"err": errors.StackTrace(err),
		})
		time.Sleep(time.Second)
	}
	return err
}

func connstring(config *Config) string {
	return fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=disable",
		config.DBHost,
		config.DBPort,
		config.DBName,
		config.DBUser,
		config.DBPassword,
	)
}

func New(config *Config) (*sql.DB, error) {
	db, err := sql.Open("postgres", connstring(config))
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(config.MaxConns)
	db.SetMaxIdleConns(config.MaxConns)
	db.SetConnMaxIdleTime(time.Hour)
	db.SetConnMaxLifetime(time.Hour)

	if err := waitForConnection(db); err != nil {
		return nil, err
	}

	bunny.SetLogger(&queryLogger{
		queries:                 config.DBLogQueries,
		queriesMinDuration:      config.DBLogQueriesMinDuration,
		transactions:            config.DBLogTransactions,
		transactionsMinDuration: config.DBLogTransactionsMinDuration,
	})

	// Create a new collector, the name will be used as a label on the metrics
	// Register it with Prometheus
	_ = prometheus.Register(NewStatsCollector(config.DBName, db))

	return db, nil
}

func Listen(config *Config, topic string) (*pq.Listener, error) {
	l := pq.NewListener(connstring(config), 100*time.Millisecond, 20*time.Second, func(event pq.ListenerEventType, err error) {
		ctx := context.Background()
		log.Warn(ctx, fmt.Sprintf("Listen event %v %v", event, err))
	})

	err := l.Listen(topic)
	if err != nil {
		return nil, errors.Errorf("Failed to setup listen connection: %w", err)
	}

	return l, nil
}
