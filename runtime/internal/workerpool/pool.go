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
	"github.com/duxweb/dux-runtime/runtime/internal/status"
	"github.com/duxweb/dux-runtime/runtime/internal/task"
)

var ErrPoolUnavailable = errors.New("worker pool unavailable")

type workerFactory interface {
	NewWorker() (*phpworker.Worker, error)
}

type Pool struct {
	config   *config.Config
	factory  workerFactory
	state    *status.State
	once     sync.Once
	mu       sync.Mutex
	idle     []*phpworker.Worker
	total    int
	started  chan struct{}
	shutdown chan struct{}
	notify   chan struct{}
}

func New(cfg *config.Config, factory *phpworker.Factory, state *status.State) *Pool {
	return &Pool{
		config:   cfg,
		factory:  factory,
		state:    state,
		started:  make(chan struct{}),
		shutdown: make(chan struct{}),
		notify:   make(chan struct{}, 1),
	}
}

func (p *Pool) Run(ctx context.Context) error {
	var startErr error
	p.once.Do(func() {
		startErr = p.bootstrap()
	})
	if startErr != nil {
		return startErr
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.stop()
			return nil
		case <-ticker.C:
			p.shrinkIdle()
		}
	}
}

func (p *Pool) Workers() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.total
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

func (p *Pool) bootstrap() error {
	minWorkers := p.config.Workers
	if minWorkers <= 0 {
		minWorkers = 1
	}
	if p.state != nil {
		p.state.SetWorkerPoolConfigured(minWorkers)
	}

	created, err := p.spawn(minWorkers)
	if err != nil {
		for _, worker := range created {
			_ = worker.Close()
		}
		return err
	}

	p.mu.Lock()
	p.idle = append(p.idle, created...)
	p.total = len(created)
	current := p.total
	p.mu.Unlock()

	if p.state != nil {
		p.state.SetWorkerPoolStarted(current)
		p.state.SetWorkerPoolCurrent(current)
	}
	close(p.started)
	log.Printf("workerpool: started with %d workers (min=%d max=%s step=%d idle_ttl=%ds)", current, minWorkers, p.maxWorkersLabel(), p.scaleUpStep(), p.config.WorkerIdleTTLSeconds)
	return nil
}

func (p *Pool) acquire(ctx context.Context) (*phpworker.Worker, error) {
	for {
		p.mu.Lock()
		if p.isShutdownLocked() {
			p.mu.Unlock()
			return nil, ErrPoolUnavailable
		}

		idleCount := len(p.idle)
		if idleCount > 0 {
			worker := p.idle[idleCount-1]
			p.idle = p.idle[:idleCount-1]
			p.mu.Unlock()
			return worker, nil
		}

		grow := p.reserveGrowthLocked()
		p.mu.Unlock()

		if grow > 0 {
			return p.growAndAcquire(grow)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-p.shutdown:
			return nil, ErrPoolUnavailable
		case <-p.notify:
		}
	}
}

func (p *Pool) release(worker *phpworker.Worker) {
	if worker == nil {
		return
	}

	if worker.ShouldRestart() {
		p.recycle(worker)
		return
	}

	p.mu.Lock()
	if p.isShutdownLocked() {
		p.total--
		current := p.total
		p.mu.Unlock()
		p.syncCurrent(current)
		_ = worker.Close()
		return
	}
	p.idle = append(p.idle, worker)
	p.mu.Unlock()
	p.signal()
}

func (p *Pool) recycle(worker *phpworker.Worker) {
	reason := worker.RestartReason()
	if reason == "" {
		reason = "recycle"
	}
	log.Printf("workerpool: recycling %s reason=%s", worker, reason)
	if p.state != nil {
		p.state.IncWorkerPoolRecycled(reason == "broken")
	}
	_ = worker.Close()

	if p.closed() {
		p.mu.Lock()
		p.total--
		current := p.total
		p.mu.Unlock()
		p.syncCurrent(current)
		return
	}

	next, err := p.factory.NewWorker()
	if err != nil {
		log.Printf("workerpool: restart %s failed: %v", worker, err)
		p.mu.Lock()
		p.total--
		current := p.total
		p.mu.Unlock()
		p.syncCurrent(current)
		p.signal()
		return
	}

	p.mu.Lock()
	if p.isShutdownLocked() {
		p.total--
		current := p.total
		p.mu.Unlock()
		p.syncCurrent(current)
		_ = next.Close()
		return
	}
	p.idle = append(p.idle, next)
	current := p.total
	p.mu.Unlock()
	p.syncCurrent(current)
	p.signal()
}

func (p *Pool) growAndAcquire(grow int) (*phpworker.Worker, error) {
	created, err := p.spawn(grow)
	if err != nil {
		p.mu.Lock()
		p.total -= grow
		current := p.total
		p.mu.Unlock()
		p.syncCurrent(current)
		p.signal()
		return nil, err
	}

	worker := created[0]
	idle := created[1:]

	p.mu.Lock()
	if p.isShutdownLocked() {
		p.total -= len(created)
		current := p.total
		p.mu.Unlock()
		p.syncCurrent(current)
		_ = worker.Close()
		for _, item := range idle {
			_ = item.Close()
		}
		return nil, ErrPoolUnavailable
	}
	p.idle = append(p.idle, idle...)
	current := p.total
	p.mu.Unlock()

	p.syncCurrent(current)
	if p.state != nil {
		p.state.AddWorkerPoolScaledUp(len(created))
	}
	log.Printf("workerpool: scaled up by %d, current=%d", len(created), current)
	p.signal()
	return worker, nil
}

func (p *Pool) spawn(count int) ([]*phpworker.Worker, error) {
	if count <= 0 {
		return nil, nil
	}

	created := make([]*phpworker.Worker, 0, count)
	for i := 0; i < count; i++ {
		worker, err := p.factory.NewWorker()
		if err != nil {
			for _, item := range created {
				_ = item.Close()
			}
			return nil, err
		}
		created = append(created, worker)
	}
	return created, nil
}

func (p *Pool) reserveGrowthLocked() int {
	step := p.scaleUpStep()
	allowed := step
	if maxWorkers := p.config.MaxWorkers; maxWorkers > 0 {
		remaining := maxWorkers - p.total
		if remaining <= 0 {
			return 0
		}
		if allowed > remaining {
			allowed = remaining
		}
	}
	if allowed <= 0 {
		return 0
	}
	p.total += allowed
	return allowed
}

func (p *Pool) shrinkIdle() {
	ttl := time.Duration(p.config.WorkerIdleTTLSeconds) * time.Second
	if ttl <= 0 {
		return
	}

	now := time.Now()
	minWorkers := p.config.Workers
	if minWorkers <= 0 {
		minWorkers = 1
	}

	p.mu.Lock()
	if p.isShutdownLocked() || p.total <= minWorkers || len(p.idle) == 0 {
		p.mu.Unlock()
		return
	}

	keep := make([]*phpworker.Worker, 0, len(p.idle))
	drop := make([]*phpworker.Worker, 0)
	removable := p.total - minWorkers

	for _, worker := range p.idle {
		if removable > 0 && now.Sub(worker.LastUsed()) >= ttl {
			drop = append(drop, worker)
			removable--
			p.total--
			continue
		}
		keep = append(keep, worker)
	}

	p.idle = keep
	current := p.total
	p.mu.Unlock()

	if len(drop) == 0 {
		return
	}

	for _, worker := range drop {
		_ = worker.Close()
	}
	p.syncCurrent(current)
	if p.state != nil {
		p.state.AddWorkerPoolScaledDown(len(drop))
	}
	log.Printf("workerpool: scaled down by %d, current=%d", len(drop), current)
}

func (p *Pool) stop() {
	p.mu.Lock()
	if p.isShutdownLocked() {
		p.mu.Unlock()
		return
	}
	close(p.shutdown)
	idle := p.idle
	p.idle = nil
	p.mu.Unlock()

	for _, worker := range idle {
		_ = worker.Close()
	}
}

func (p *Pool) closed() bool {
	select {
	case <-p.shutdown:
		return true
	default:
		return false
	}
}

func (p *Pool) isShutdownLocked() bool {
	select {
	case <-p.shutdown:
		return true
	default:
		return false
	}
}

func (p *Pool) scaleUpStep() int {
	if p.config.ScaleUpStep > 0 {
		return p.config.ScaleUpStep
	}
	return 1
}

func (p *Pool) maxWorkersLabel() string {
	if p.config.MaxWorkers <= 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d", p.config.MaxWorkers)
}

func (p *Pool) signal() {
	select {
	case p.notify <- struct{}{}:
	default:
	}
}

func (p *Pool) syncCurrent(current int) {
	if p.state != nil {
		p.state.SetWorkerPoolCurrent(current)
	}
}
