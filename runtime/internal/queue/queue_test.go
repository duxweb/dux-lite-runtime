package queue

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/duxweb/dux-runtime/runtime/internal/config"
	"github.com/duxweb/dux-runtime/runtime/internal/phpmaster"
	"github.com/duxweb/dux-runtime/runtime/internal/task"
)

type fakeBackend struct {
	mu           sync.Mutex
	queueConfig  []phpmaster.QueueWorkerConfig
	queueCfgErr  error
	pullCalls    map[string]int
	pullResponse map[string][]task.Envelope
}

func (b *fakeBackend) QueueConfig(context.Context) ([]phpmaster.QueueWorkerConfig, error) {
	return b.queueConfig, b.queueCfgErr
}

func (b *fakeBackend) PullQueue(_ context.Context, queue string, limit int) ([]task.Envelope, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pullCalls == nil {
		b.pullCalls = map[string]int{}
	}
	b.pullCalls[queue]++
	items := b.pullResponse[queue]
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (b *fakeBackend) AckQueue(context.Context, string, map[string]any) error { return nil }
func (b *fakeBackend) FailQueue(context.Context, string, string, bool) error  { return nil }
func (b *fakeBackend) PullSchedule(context.Context, time.Time, int) ([]task.Envelope, error) {
	return nil, nil
}
func (b *fakeBackend) ReportSchedule(context.Context, phpmaster.ScheduleReport) error { return nil }
func (b *fakeBackend) WsAuth(context.Context, phpmaster.WsAuthRequest) (*phpmaster.WsAuthResponse, error) {
	return nil, nil
}
func (b *fakeBackend) WsSubscribe(context.Context, phpmaster.WsActionRequest) (*phpmaster.WsActionResponse, error) {
	return nil, nil
}
func (b *fakeBackend) WsPublish(context.Context, phpmaster.WsActionRequest) (*phpmaster.WsActionResponse, error) {
	return nil, nil
}
func (b *fakeBackend) WsEvent(context.Context, phpmaster.WsGatewayEvent) (*phpmaster.WsActionResponse, error) {
	return nil, nil
}
func (b *fakeBackend) WsMessage(context.Context, phpmaster.WsMessageRequest) (*phpmaster.WsActionResponse, error) {
	return nil, nil
}

type blockingExecutor struct {
	done chan struct{}
}

func (e *blockingExecutor) Execute(context.Context, task.Envelope) (task.Result, error) {
	<-e.done
	return task.Result{OK: true, Result: map[string]any{}}, nil
}

type successExecutor struct{}

func (successExecutor) Execute(context.Context, task.Envelope) (task.Result, error) {
	return task.Result{OK: true, Result: map[string]any{}}, nil
}

type countingBlockingExecutor struct {
	mu    sync.Mutex
	count int
	done  chan struct{}
}

func (e *countingBlockingExecutor) Execute(context.Context, task.Envelope) (task.Result, error) {
	e.mu.Lock()
	e.count++
	e.mu.Unlock()
	<-e.done
	return task.Result{OK: true, Result: map[string]any{}}, nil
}

func (e *countingBlockingExecutor) Calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.count
}

func TestTickUsesWorkerConcurrencyFromPHPConfig(t *testing.T) {
	backend := &fakeBackend{
		queueConfig: []phpmaster.QueueWorkerConfig{
			{Name: "alpha", Num: 1},
		},
		pullResponse: map[string][]task.Envelope{
			"alpha": {
				{ID: "job-1", Type: task.KindQueue, Name: "Demo", Queue: "alpha"},
			},
		},
	}
	executor := &blockingExecutor{done: make(chan struct{})}
	service := New(&config.Config{
		QueuePullLimit:     8,
		QueueConfigRefresh: time.Minute,
		QueueNames:         []string{"default"},
	}, phpmaster.NewClientWithBackend("", backend), executor, nil)

	ctx := context.Background()
	if err := service.tick(ctx); err != nil {
		t.Fatalf("first tick returned error: %v", err)
	}
	if err := service.tick(ctx); err != nil {
		t.Fatalf("second tick returned error: %v", err)
	}

	backend.mu.Lock()
	calls := backend.pullCalls["alpha"]
	backend.mu.Unlock()

	if calls != 1 {
		t.Fatalf("expected one pull for alpha, got %d", calls)
	}

	close(executor.done)
	service.wg.Wait()
}

func TestTickSkipsDuplicateInflightJobs(t *testing.T) {
	backend := &fakeBackend{
		queueConfig: []phpmaster.QueueWorkerConfig{
			{Name: "alpha", Num: 3},
		},
		pullResponse: map[string][]task.Envelope{
			"alpha": {
				{ID: "job-1", Type: task.KindQueue, Name: "Demo", Queue: "alpha"},
				{ID: "job-1", Type: task.KindQueue, Name: "Demo", Queue: "alpha"},
				{ID: "job-1", Type: task.KindQueue, Name: "Demo", Queue: "alpha"},
			},
		},
	}
	executor := &countingBlockingExecutor{done: make(chan struct{})}
	service := New(&config.Config{
		QueuePullLimit:     8,
		QueueConfigRefresh: time.Minute,
		QueueNames:         []string{"default"},
	}, phpmaster.NewClientWithBackend("", backend), executor, nil)

	if err := service.tick(context.Background()); err != nil {
		t.Fatalf("tick returned error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if calls := executor.Calls(); calls != 1 {
		t.Fatalf("expected duplicate inflight job to execute once, got %d", calls)
	}

	close(executor.done)
	service.wg.Wait()
}

func TestRefreshWorkersFallsBackToConfiguredQueueNames(t *testing.T) {

	backend := &fakeBackend{
		queueCfgErr: phpmaster.ErrUnavailable,
	}
	service := New(&config.Config{
		QueueNames:         []string{"fallback"},
		QueueConfigRefresh: time.Minute,
		QueuePullLimit:     4,
	}, phpmaster.NewClientWithBackend("", backend), successExecutor{}, nil)

	if err := service.refreshWorkers(context.Background()); err != nil {
		t.Fatalf("refreshWorkers returned error: %v", err)
	}

	workers := service.snapshotWorkers()
	if len(workers) != 1 {
		t.Fatalf("expected one fallback worker, got %d", len(workers))
	}
	if workers[0].config.Name != "fallback" {
		t.Fatalf("expected fallback worker name, got %s", workers[0].config.Name)
	}
	if workers[0].config.Num != 1 {
		t.Fatalf("expected fallback worker concurrency 1, got %d", workers[0].config.Num)
	}
}
