package scheduler

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
	config *config.Config
	master *phpmaster.Client
	pool   *workerpool.Pool
	state  *status.State
	wg     sync.WaitGroup
}

func New(cfg *config.Config, master *phpmaster.Client, pool *workerpool.Pool, state *status.State) *Service {
	return &Service{
		config: cfg,
		master: master,
		pool:   pool,
		state:  state,
	}
}

func (s *Service) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.config.SchedulerPollInterval)
	defer ticker.Stop()
	defer s.wg.Wait()

	log.Printf("scheduler: polling interval=%s limit=%d", s.config.SchedulerPollInterval, s.config.SchedulerPullLimit)

	for {
		if err := s.tick(ctx, time.Now()); err != nil && !errors.Is(err, phpmaster.ErrUnavailable) && !errors.Is(err, workerpool.ErrPoolUnavailable) {
			return err
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Service) tick(ctx context.Context, now time.Time) error {
	jobs, err := s.master.PullSchedule(ctx, now, s.config.SchedulerPullLimit)
	if err != nil {
		return err
	}
	if s.state != nil && len(jobs) > 0 {
		s.state.IncSchedulerPulled(len(jobs))
	}
	for _, job := range jobs {
		s.wg.Add(1)
		go func(job task.Envelope) {
			defer s.wg.Done()
			s.handleJob(ctx, job)
		}(job)
	}
	return nil
}

func (s *Service) handleJob(ctx context.Context, job task.Envelope) {
	result, err := s.pool.Execute(ctx, job)
	report := phpmaster.ScheduleReport{
		TaskID: job.ID,
	}
	if err == nil {
		report.Result = result.Result
	} else {
		report.Error = err.Error()
	}
	if s.state != nil {
		s.state.IncSchedulerReported(err != nil)
	}

	if reportErr := s.master.ReportSchedule(ctx, report); reportErr != nil && !errors.Is(reportErr, phpmaster.ErrUnavailable) {
		log.Printf("scheduler: report failed for %s: %v", job.ID, reportErr)
	}
}
