package workerpool

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/duxweb/dux-runtime/runtime/internal/config"
	"github.com/duxweb/dux-runtime/runtime/internal/phpworker"
	"github.com/duxweb/dux-runtime/runtime/internal/task"
)

var ErrPoolUnavailable = errors.New("worker pool unavailable")

type Pool struct {
	config   *config.Config
	factory  *phpworker.Factory
	once     sync.Once
	workers  chan *phpworker.Worker
	started  chan struct{}
	shutdown chan struct{}
}

func New(cfg *config.Config, factory *phpworker.Factory) *Pool {
	return &Pool{
		config:   cfg,
		factory:  factory,
		workers:  make(chan *phpworker.Worker, cfg.Workers),
		started:  make(chan struct{}),
		shutdown: make(chan struct{}),
	}
}

func (p *Pool) Run(ctx context.Context) error {
	var startErr error
	p.once.Do(func() {
		for i := 0; i < p.config.Workers; i++ {
			worker, err := p.factory.NewWorker()
			if err != nil {
				startErr = err
				return
			}
			p.workers <- worker
		}
		if startErr != nil {
			return
		}
		close(p.started)
		log.Printf("workerpool: started with %d workers", p.config.Workers)
	})
	if startErr != nil {
		return startErr
	}

	<-ctx.Done()
	close(p.shutdown)
	for {
		select {
		case worker := <-p.workers:
			_ = worker.Close()
		default:
			return nil
		}
	}
}

func (p *Pool) Workers() int {
	return p.config.Workers
}

func (p *Pool) Factory() *phpworker.Factory {
	return p.factory
}

func (p *Pool) Execute(ctx context.Context, envelope task.Envelope) (task.Result, error) {
	select {
	case <-p.started:
	case <-ctx.Done():
		return task.Result{}, ctx.Err()
	default:
		return task.Result{}, ErrPoolUnavailable
	}

	worker, err := p.acquire(ctx)
	if err != nil {
		return task.Result{}, err
	}
	defer p.release(worker)

	timeout := p.config.TaskTimeoutSeconds
	if envelope.TimeoutSeconds > 0 {
		timeout = envelope.TimeoutSeconds
	}
	if timeout <= 0 {
		timeout = 30
	}

	taskCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	result := worker.Execute(taskCtx, envelope)
	if !result.OK {
		return result, fmt.Errorf(result.Error)
	}
	return result, nil
}

func (p *Pool) acquire(ctx context.Context) (*phpworker.Worker, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.shutdown:
		return nil, ErrPoolUnavailable
	case worker := <-p.workers:
		return worker, nil
	}
}

func (p *Pool) release(worker *phpworker.Worker) {
	if worker == nil {
		return
	}
	if worker.ShouldRestart() {
		log.Printf("workerpool: recycling %s after max jobs", worker)
		_ = worker.Close()
		next, err := p.factory.NewWorker()
		if err != nil {
			log.Printf("workerpool: restart %s failed: %v", worker, err)
			return
		}
		worker = next
	}

	select {
	case <-p.shutdown:
		_ = worker.Close()
		return
	default:
		p.workers <- worker
	}
}
