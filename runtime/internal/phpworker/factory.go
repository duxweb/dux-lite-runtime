package phpworker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/duxweb/dux-runtime/runtime/internal/config"
	"github.com/duxweb/dux-runtime/runtime/internal/task"
	"github.com/roadrunner-server/goridge/v3/pkg/frame"
	"github.com/roadrunner-server/goridge/v3/pkg/pipe"
)

var ErrWorkerCommandRequired = errors.New("php worker command required")

type Factory struct {
	config  *config.Config
	counter atomic.Int64
}

func NewFactory(cfg *config.Config) *Factory {
	return &Factory{config: cfg}
}

func (f *Factory) Config() *config.Config {
	return f.config
}

func (f *Factory) NewWorker() (*Worker, error) {
	if f.config.PHPWorkerCommand == "" {
		return nil, ErrWorkerCommandRequired
	}

	id := int(f.counter.Add(1))
	args, err := splitCommandLine(f.config.PHPWorkerCommand)
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return nil, ErrWorkerCommandRequired
	}

	command := exec.Command(args[0], args[1:]...)
	if f.config.PHPWorkerWorkdir != "" {
		command.Dir = f.config.PHPWorkerWorkdir
	}
	command.Env = append(os.Environ(),
		fmt.Sprintf("DUX_RUNTIME_WORKER_ID=%d", id),
	)

	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err = command.Start(); err != nil {
		return nil, err
	}

	worker := &Worker{
		id:       id,
		maxJobs:  f.config.WorkerMaxJobs,
		cmd:      command,
		stdin:    stdin,
		stdout:   bufio.NewReader(stdout),
		relay:    pipe.NewPipeRelay(stdout, stdin),
		lastUsed: time.Now(),
	}
	go func() {
		_, _ = bufio.NewReader(stderr).WriteTo(os.Stderr)
	}()

	return worker, nil
}

type Worker struct {
	id       int
	jobs     int
	maxJobs  int
	lastUsed time.Time
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *bufio.Reader
	relay    *pipe.Relay
	mu       sync.Mutex
	broken   bool
}

func (w *Worker) ID() int {
	return w.id
}

func (w *Worker) Broken() bool {
	return w.broken
}

func (w *Worker) LastUsed() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastUsed
}

func (w *Worker) Execute(ctx context.Context, envelope task.Envelope) task.Result {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.jobs++
	w.lastUsed = time.Now()

	if w.broken {
		return task.Result{
			ID:         envelope.ID,
			OK:         false,
			Error:      "worker is broken",
			Retryable:  true,
			FinishedAt: time.Now(),
		}
	}

	if err := w.writeTask(envelope); err != nil {
		w.broken = true
		return task.Result{
			ID:         envelope.ID,
			OK:         false,
			Error:      err.Error(),
			Retryable:  true,
			FinishedAt: time.Now(),
		}
	}

	type response struct {
		result task.Result
		err    error
	}
	ch := make(chan response, 1)
	go func() {
		result, err := w.readResult()
		ch <- response{result: result, err: err}
	}()

	select {
	case <-ctx.Done():
		w.broken = true
		_ = w.Close()
		return task.Result{
			ID:         envelope.ID,
			OK:         false,
			Error:      ctx.Err().Error(),
			Retryable:  true,
			FinishedAt: time.Now(),
		}
	case resp := <-ch:
		if resp.err != nil {
			w.broken = true
			return task.Result{
				ID:         envelope.ID,
				OK:         false,
				Error:      resp.err.Error(),
				Retryable:  true,
				FinishedAt: time.Now(),
			}
		}
		if resp.result.FinishedAt.IsZero() {
			resp.result.FinishedAt = time.Now()
		}
		return resp.result
	}
}

func (w *Worker) ShouldRestart() bool {
	if w.broken {
		return true
	}
	if w.maxJobs <= 0 {
		return false
	}
	return w.jobs >= w.maxJobs
}

func (w *Worker) RestartReason() string {
	if w.broken {
		return "broken"
	}
	if w.maxJobs > 0 && w.jobs >= w.maxJobs {
		return "max_jobs"
	}
	return ""
}

func (w *Worker) Close() error {
	if w.stdin != nil {
		_ = w.stdin.Close()
	}
	if w.cmd == nil || w.cmd.Process == nil {
		return nil
	}
	if w.cmd.ProcessState != nil && w.cmd.ProcessState.Exited() {
		return nil
	}
	if err := w.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	_, _ = w.cmd.Process.Wait()
	return nil
}

func (w *Worker) String() string {
	return fmt.Sprintf("worker-%d", w.id)
}

func (w *Worker) writeTask(envelope task.Envelope) error {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	fr := frame.NewFrame()
	fr.WriteVersion(fr.Header(), frame.Version1)
	fr.WriteFlags(fr.Header(), frame.CodecJSON)
	fr.WritePayloadLen(fr.Header(), uint32(len(payload)))
	fr.WritePayload(payload)
	fr.WriteCRC(fr.Header())
	return w.relay.Send(fr)
}

func (w *Worker) readResult() (task.Result, error) {
	fr := frame.NewFrame()
	if err := w.relay.Receive(fr); err != nil {
		return task.Result{}, err
	}
	if !fr.VerifyCRC(fr.Header()) {
		return task.Result{}, errors.New("worker response crc verification failed")
	}
	payload := strings.TrimSpace(string(fr.Payload()))
	if payload == "" {
		return task.Result{}, errors.New("worker response payload is empty")
	}

	var result task.Result
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		return task.Result{}, err
	}
	return result, nil
}

func splitCommandLine(command string) ([]string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, nil
	}

	var (
		args    []string
		current strings.Builder
		quote   rune
		escape  bool
	)

	for _, char := range command {
		switch {
		case escape:
			current.WriteRune(char)
			escape = false
		case char == '\\' && quote != '\'':
			escape = true
		case quote != 0:
			if char == quote {
				quote = 0
			} else {
				current.WriteRune(char)
			}
		case char == '\'' || char == '"':
			quote = char
		case char == ' ' || char == '\t' || char == '\n':
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(char)
		}
	}

	if escape {
		return nil, errors.New("worker command has trailing escape")
	}
	if quote != 0 {
		return nil, errors.New("worker command has unclosed quote")
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args, nil
}
