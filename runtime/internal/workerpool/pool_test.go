package workerpool

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/duxweb/dux-runtime/runtime/internal/config"
	"github.com/duxweb/dux-runtime/runtime/internal/phpworker"
)

func TestPoolScalesUpUnderContention(t *testing.T) {
	requirePHP(t)

	pool, cancel, done := startTestPool(t, &config.Config{
		Workers:              1,
		MaxWorkers:           3,
		ScaleUpStep:          2,
		WorkerIdleTTLSeconds: 60,
		PHPWorkerCommand:     `php -r 'while (true) { usleep(100000); }'`,
	})
	defer stopTestPool(t, cancel, done)

	first, err := pool.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire first worker: %v", err)
	}
	second, err := pool.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire scaled worker: %v", err)
	}

	if pool.Workers() != 3 {
		t.Fatalf("expected pool to scale to 3 workers, got %d", pool.Workers())
	}

	pool.release(first)
	pool.release(second)
}

func TestPoolScalesDownIdleWorkers(t *testing.T) {
	requirePHP(t)

	pool, cancel, done := startTestPool(t, &config.Config{
		Workers:              1,
		MaxWorkers:           2,
		ScaleUpStep:          1,
		WorkerIdleTTLSeconds: 1,
		PHPWorkerCommand:     `php -r 'while (true) { usleep(100000); }'`,
	})
	defer stopTestPool(t, cancel, done)

	first, err := pool.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire first worker: %v", err)
	}
	second, err := pool.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire second worker: %v", err)
	}

	pool.release(first)
	pool.release(second)

	waitFor(t, 4*time.Second, func() bool {
		return pool.Workers() == 1
	})
}

func TestPoolRespectsMaxWorkers(t *testing.T) {
	requirePHP(t)

	pool, cancel, done := startTestPool(t, &config.Config{
		Workers:              1,
		MaxWorkers:           2,
		ScaleUpStep:          4,
		WorkerIdleTTLSeconds: 60,
		PHPWorkerCommand:     `php -r 'while (true) { usleep(100000); }'`,
	})
	defer stopTestPool(t, cancel, done)

	first, err := pool.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire first worker: %v", err)
	}
	second, err := pool.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire second worker: %v", err)
	}

	ctx, cancelAcquire := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancelAcquire()

	_, err = pool.acquire(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
	if pool.Workers() != 2 {
		t.Fatalf("expected pool to stay at max 2 workers, got %d", pool.Workers())
	}

	pool.release(first)
	pool.release(second)
}

func startTestPool(t *testing.T, cfg *config.Config) (*Pool, context.CancelFunc, <-chan error) {
	t.Helper()

	if cfg.TaskTimeoutSeconds <= 0 {
		cfg.TaskTimeoutSeconds = 30
	}

	pool := New(cfg, phpworker.NewFactory(cfg), nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- pool.Run(ctx)
	}()

	select {
	case <-pool.started:
	case err := <-done:
		t.Fatalf("pool exited before start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("pool start timeout")
	}

	return pool, cancel, done
}

func stopTestPool(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("pool run failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pool stop timeout")
	}
}

func waitFor(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("condition not reached in time")
}

func requirePHP(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("php not available")
	}
}
