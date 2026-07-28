package process

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"embassy.dev/bot/toolkit/log"
	"embassy.dev/bot/toolkit/nopanic"
	"github.com/sqlbunny/errors"
)

type Process struct {
	Name string
	Fn   func(ctx context.Context) error
}

type ret struct {
	name string
	err  error
}

func Run(processes []Process) error {
	return RunTimeout(60*time.Second, processes)
}

func RunTimeout(timeout time.Duration, processes []Process) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	force := make(chan struct{})
	oneExited := make(chan struct{})
	go func() {
		select {
		case sig := <-sigs:
			log.Warnf(ctx, "got signal, shutting down", log.Fields{"signal": sig.String()})
		case <-oneExited:
			log.Info(ctx, "one process exited, shutting down")
		}

		cancel()

		t := time.NewTimer(timeout)
		select {
		case <-t.C:
			log.Error(ctx, "shutdown is taking too long, forcing shutdown")
		case sig := <-sigs:
			log.Errorf(ctx, "got second signal, forcing shutdown", log.Fields{"signal": sig.String()})
		}
		close(force)
	}()

	rets := make(chan ret)

	for i := range processes {
		process := processes[i]
		go func() {
			log.Infof(ctx, "process started", log.Fields{"name": process.Name})
			err := nopanic.Run(func() error {
				return process.Fn(ctx)
			})
			rets <- ret{
				name: process.Name,
				err:  err,
			}
		}()
	}

	oneExitedClosed := false

	for range processes {
		select {
		case <-force:
			return errors.Errorf("Force exiting")
		case r := <-rets:
			if r.err == nil {
				log.Infof(ctx, "process exited", log.Fields{"name": r.name})
			} else if errors.Is(r.err, context.Canceled) {
				log.Infof(ctx, "process canceled", log.Fields{"name": r.name})
			} else {
				log.Errorf(ctx, "process exited with error", log.Fields{"name": r.name, "err": errors.StackTrace(r.err)})
			}

			if !oneExitedClosed {
				close(oneExited)
				oneExitedClosed = true
			}
		}
	}

	log.Info(ctx, "all processes exited")
	return nil
}
