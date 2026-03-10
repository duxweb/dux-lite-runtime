package queue

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/duxweb/dux-runtime/runtime/internal/config"
	"github.com/duxweb/dux-runtime/runtime/internal/phpmaster"
	"github.com/duxweb/dux-runtime/runtime/internal/status"
	"github.com/duxweb/dux-runtime/runtime/internal/task"
	"github.com/duxweb/dux-runtime/runtime/internal/workerpool"
)

type Service struct {
	config      *config.Config
	master      *phpmaster.Client
	pool        Executor
	state       *status.State
	wg          sync.WaitGroup
	mu          sync.Mutex
	workers     map[string]*workerState
	lastRefresh time.Time
}

type Executor interface {
	Execute(context.Context, task.Envelope) (task.Result, error)
}

type workerState struct {
	config phpmaster.QueueWorkerConfig
	active int
}

func New(cfg *config.Config, master *phpmaster.Client, pool Executor, state *status.State) *Service {
	return &Service{
		config:  cfg,
		master:  master,
		pool:    pool,
		state:   state,
		workers: map[string]*workerState{},
	}
}

func (s *Service) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.config.QueuePollInterval)
	defer ticker.Stop()
	defer s.wg.Wait()

	log.Printf("queue: polling interval=%s limit=%d", s.config.QueuePollInterval, s.config.QueuePullLimit)

	for {
		if err := s.tick(ctx); err != nil && !errors.Is(err, phpmaster.ErrUnavailable) && !errors.Is(err, workerpool.ErrPoolUnavailable) {
			return err
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Service) tick(ctx context.Context) error {
	if err := s.refreshWorkers(ctx); err != nil {
		return err
	}

	for _, worker := range s.snapshotWorkers() {
		available := worker.config.Num - worker.active
		if available <= 0 {
			continue
		}
		limit := min(available, s.config.QueuePullLimit)
		if limit <= 0 {
			continue
		}

		jobs, err := s.master.PullQueue(ctx, worker.config.Name, limit)
		if err != nil {
			return err
		}
		if s.state != nil && len(jobs) > 0 {
			s.state.IncQueuePulled(len(jobs))
		}
		for _, job := range jobs {
			s.markActive(worker.config.Name, 1)
			s.wg.Add(1)
			go func(queueName string, job task.Envelope) {
				defer s.wg.Done()
				defer s.markActive(queueName, -1)
				s.handleJob(ctx, job)
			}(worker.config.Name, job)
		}
	}

	return nil
}

func (s *Service) handleJob(ctx context.Context, job task.Envelope) {
	result, err := s.pool.Execute(ctx, job)
	if err == nil {
		if s.state != nil {
			s.state.IncQueueAcked()
		}
		if ackErr := s.master.AckQueue(ctx, job.ID, result.Result); ackErr != nil && !errors.Is(ackErr, phpmaster.ErrUnavailable) {
			log.Printf("queue: ack failed for %s: %v", job.ID, ackErr)
		}
		return
	}

	retryable := true
	message := err.Error()
	if result.Error != "" {
		message = result.Error
		retryable = result.Retryable
	}
	if failErr := s.master.FailQueue(ctx, job.ID, message, retryable); failErr != nil && !errors.Is(failErr, phpmaster.ErrUnavailable) {
		log.Printf("queue: fail report failed for %s: %v", job.ID, failErr)
	}
	if s.state != nil {
		s.state.IncQueueFailed()
	}
}

func (s *Service) refreshWorkers(ctx context.Context) error {
	if time.Since(s.lastRefresh) < s.config.QueueConfigRefresh && len(s.workers) > 0 {
		return nil
	}

	items, err := s.master.QueueConfig(ctx)
	if err != nil {
		if errors.Is(err, phpmaster.ErrUnavailable) {
			s.useFallbackWorkers()
			return nil
		}
		return err
	}
	if len(items) == 0 {
		s.useFallbackWorkers()
		return nil
	}

	s.mu.Lock()
	next := map[string]*workerState{}
	for _, item := range items {
		if item.Name == "" {
			continue
		}
		if item.Num <= 0 {
			item.Num = 1
		}
		active := 0
		if current, ok := s.workers[item.Name]; ok {
			active = current.active
		}
		next[item.Name] = &workerState{
			config: item,
			active: active,
		}
	}
	s.workers = next
	s.lastRefresh = time.Now()
	s.mu.Unlock()
	s.syncState()
	return nil
}

func (s *Service) useFallbackWorkers() {
	s.mu.Lock()

	if time.Since(s.lastRefresh) < s.config.QueueConfigRefresh && len(s.workers) > 0 {
		s.mu.Unlock()
		return
	}

	next := map[string]*workerState{}
	for _, name := range s.config.QueueNames {
		if name == "" {
			continue
		}
		active := 0
		if current, ok := s.workers[name]; ok {
			active = current.active
		}
		next[name] = &workerState{
			config: phpmaster.QueueWorkerConfig{
				Name: name,
				Num:  1,
			},
			active: active,
		}
	}
	s.workers = next
	s.lastRefresh = time.Now()
	s.mu.Unlock()
	s.syncState()
}

func (s *Service) snapshotWorkers() []*workerState {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]*workerState, 0, len(s.workers))
	for _, item := range s.workers {
		copy := *item
		items = append(items, &copy)
	}
	return items
}

func (s *Service) markActive(name string, delta int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.workers[name]
	if !ok {
		return
	}
	item.active += delta
	if item.active < 0 {
		item.active = 0
	}
	if s.state != nil {
		s.state.SetQueueWorkerActive(name, item.active)
	}
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Service) syncState() {
	if s.state == nil {
		return
	}
	items := s.snapshotWorkers()
	configs := make([]phpmaster.QueueWorkerConfig, 0, len(items))
	active := map[string]int{}
	for _, item := range items {
		configs = append(configs, item.config)
		active[item.config.Name] = item.active
	}
	s.state.SetQueueWorkers(configs, active)
}
