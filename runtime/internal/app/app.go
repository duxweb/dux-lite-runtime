package app

import (
	"context"
	"errors"
	"sync"

	"github.com/duxweb/dux-runtime/runtime/internal/config"
	"github.com/duxweb/dux-runtime/runtime/internal/phpmaster"
	"github.com/duxweb/dux-runtime/runtime/internal/phpworker"
	"github.com/duxweb/dux-runtime/runtime/internal/queue"
	"github.com/duxweb/dux-runtime/runtime/internal/realtime"
	"github.com/duxweb/dux-runtime/runtime/internal/scheduler"
	"github.com/duxweb/dux-runtime/runtime/internal/status"
	"github.com/duxweb/dux-runtime/runtime/internal/workerpool"
)

type Runner interface {
	Run(context.Context) error
}

type App struct {
	runners []Runner
}

func New(cfg *config.Config) (*App, error) {
	if cfg == nil {
		return nil, errors.New("config is nil")
	}

	master := phpmaster.NewClient(cfg.ControlSocketPath)
	state := status.New()
	workers := workerpool.New(cfg, phpworker.NewFactory(cfg), state)

	return &App{
		runners: []Runner{
			realtime.New(cfg, master, state),
			scheduler.New(cfg, master, workers, state),
			queue.New(cfg, master, workers, state),
			workers,
		},
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(a.runners))

	for _, runner := range a.runners {
		wg.Add(1)
		go func(r Runner) {
			defer wg.Done()
			if err := r.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				errCh <- err
			}
		}(runner)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		<-done
		return nil
	case <-done:
		close(errCh)
		for err := range errCh {
			if err != nil {
				return err
			}
		}
		return nil
	case err := <-errCh:
		return err
	}
}
