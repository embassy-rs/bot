package log

import (
	"context"
	"fmt"
	"os"
	"sync"

	"embassy.dev/bot/toolkit/asynccontext"
	"github.com/sirupsen/logrus"
)

type loggerKey struct{}

type Fields map[string]any

var defaultLogger *logrus.Logger
var defaultLoggerOnce sync.Once

func Context(ctx context.Context, logger logrus.FieldLogger) context.Context {
	ctx = asynccontext.WithValue(ctx, loggerKey{}, logger)
	return ctx
}

func From(ctx context.Context) logrus.FieldLogger {
	val := ctx.Value(loggerKey{})
	if val == nil {
		// Anything that doesn't go through commands.Run -- tests, mostly --
		// would otherwise nil-deref on its first log line.
		defaultLoggerOnce.Do(func() {
			if defaultLogger == nil {
				InitDefault()
			}
		})
		return defaultLogger
	}

	return val.(logrus.FieldLogger)
}

func With(ctx context.Context, fields Fields) context.Context {
	return Context(ctx, From(ctx).WithFields(logrus.Fields(fields)))
}

func Debug(ctx context.Context, msg string) {
	From(ctx).Debug(msg)
}

func Debugf(ctx context.Context, msg string, fields Fields) {
	From(ctx).WithFields(logrus.Fields(fields)).Debug(msg)
}

func Info(ctx context.Context, msg string) {
	From(ctx).Info(msg)
}

func Infof(ctx context.Context, msg string, fields Fields) {
	From(ctx).WithFields(logrus.Fields(fields)).Info(msg)
}

func Warn(ctx context.Context, msg string) {
	From(ctx).Warn(msg)
}

func Warnf(ctx context.Context, msg string, fields Fields) {
	From(ctx).WithFields(logrus.Fields(fields)).Warn(msg)
}

func Error(ctx context.Context, msg string) {
	From(ctx).Error(msg)
}

func Errorf(ctx context.Context, msg string, fields Fields) {
	From(ctx).WithFields(logrus.Fields(fields)).Error(msg)
}
func Flush(ctx context.Context) {
}

func New(config *Config) (*logrus.Logger, error) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	switch config.LogFormat {
	case "json":
		logger.Formatter = &logrus.JSONFormatter{
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyLevel: "severity",
				logrus.FieldKeyMsg:   "message",
				logrus.FieldKeyTime:  "timestamp",
			},
		}
		logger.Out = os.Stdout
	case "text":
		logger.Formatter = &TextFormatter{}
	default:
		panic(fmt.Sprintf("Invalid CONFIG_LOG_FORMAT '%s'", config.LogFormat))
	}

	return logger, nil
}

func InitDefault() {
	config, err := ConfigFromEnv()
	if err != nil {
		panic(err)
	}

	logger, err := New(config)
	if err != nil {
		panic(err)
	}

	defaultLogger = logger
}
