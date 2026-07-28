package commands

import (
	"context"
	"os"

	"embassy.dev/bot/toolkit/log"
	"embassy.dev/bot/toolkit/nopanic"
	"github.com/sqlbunny/errors"
)

func Run(commands map[string]func(ctx context.Context) error) {
	log.InitDefault()
	ctx := context.Background()

	err := nopanic.Run(func() error {
		if len(os.Args) < 2 {
			return errors.New("usage: " + os.Args[0] + " <command>")
		}

		cmd := os.Args[1]
		fn, ok := commands[cmd]
		if !ok {
			return errors.New("unknown command: " + cmd)
		}

		return fn(ctx)
	})
	if err != nil {
		log.Error(ctx, errors.StackTrace(err))
		log.Flush(ctx)
		os.Exit(1)
	}

	log.Flush(ctx)
	os.Exit(0)
}

func All(cmds ...func(ctx context.Context) error) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		for _, cmd := range cmds {
			err := cmd(ctx)
			if err != nil {
				return err
			}
		}
		return nil
	}
}
