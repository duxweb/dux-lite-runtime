package phpmaster

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/duxweb/dux-runtime/runtime/internal/task"
)

var ErrUnavailable = errors.New("php master transport unavailable")

type ScheduleReport struct {
	TaskID string         `json:"task_id"`
	Result map[string]any `json:"result,omitempty"`
	Error  string         `json:"error,omitempty"`
}

type WsAuthRequest struct {
	App   string         `json:"app"`
	Token string         `json:"token"`
	Meta  map[string]any `json:"meta,omitempty"`
}

type WsAuthResponse struct {
	ClientID       string              `json:"client_id"`
	ClientType     string              `json:"client_type"`
	AllowSubscribe map[string][]string `json:"allow_subscribe,omitempty"`
	AllowPublish   map[string][]string `json:"allow_publish,omitempty"`
	Meta           map[string]any      `json:"meta,omitempty"`
}

type Backend interface {
	QueueConfig(context.Context) ([]QueueWorkerConfig, error)
	PullQueue(context.Context, string, int) ([]task.Envelope, error)
	AckQueue(context.Context, string, map[string]any) error
	FailQueue(context.Context, string, string, bool) error
	PullSchedule(context.Context, time.Time, int) ([]task.Envelope, error)
	ReportSchedule(context.Context, ScheduleReport) error
	WsAuth(context.Context, WsAuthRequest) (*WsAuthResponse, error)
}

type Client struct {
	socketPath string
	backend    Backend
}

type QueueWorkerConfig struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Driver  string         `json:"driver"`
	Num     int            `json:"num"`
	Weights map[string]int `json:"weights,omitempty"`
}

func NewClient(socketPath string) *Client {
	if socketPath == "" {
		return &Client{
			socketPath: socketPath,
			backend:    NoopBackend{},
		}
	}
	return &Client{
		socketPath: socketPath,
		backend:    NewSocketBackend(socketPath),
	}
}

func NewClientWithBackend(socketPath string, backend Backend) *Client {
	if backend == nil {
		backend = NoopBackend{}
	}
	return &Client{
		socketPath: socketPath,
		backend:    backend,
	}
}

func (c *Client) SocketPath() string {
	return c.socketPath
}

func (c *Client) QueueConfig(ctx context.Context) ([]QueueWorkerConfig, error) {
	return c.backend.QueueConfig(ctx)
}

func (c *Client) PullQueue(ctx context.Context, queue string, limit int) ([]task.Envelope, error) {
	return c.backend.PullQueue(ctx, queue, limit)
}

func (c *Client) AckQueue(ctx context.Context, jobID string, result map[string]any) error {
	return c.backend.AckQueue(ctx, jobID, result)
}

func (c *Client) FailQueue(ctx context.Context, jobID string, message string, retryable bool) error {
	return c.backend.FailQueue(ctx, jobID, message, retryable)
}

func (c *Client) PullSchedule(ctx context.Context, now time.Time, limit int) ([]task.Envelope, error) {
	return c.backend.PullSchedule(ctx, now, limit)
}

func (c *Client) ReportSchedule(ctx context.Context, report ScheduleReport) error {
	return c.backend.ReportSchedule(ctx, report)
}

func (c *Client) WsAuth(ctx context.Context, request WsAuthRequest) (*WsAuthResponse, error) {
	return c.backend.WsAuth(ctx, request)
}

type NoopBackend struct{}

func (NoopBackend) QueueConfig(context.Context) ([]QueueWorkerConfig, error) {
	return nil, ErrUnavailable
}

func (NoopBackend) PullQueue(context.Context, string, int) ([]task.Envelope, error) {
	return nil, ErrUnavailable
}

func (NoopBackend) AckQueue(context.Context, string, map[string]any) error {
	return ErrUnavailable
}

func (NoopBackend) FailQueue(context.Context, string, string, bool) error {
	return ErrUnavailable
}

func (NoopBackend) PullSchedule(context.Context, time.Time, int) ([]task.Envelope, error) {
	return nil, ErrUnavailable
}

func (NoopBackend) ReportSchedule(context.Context, ScheduleReport) error {
	return ErrUnavailable
}

func (NoopBackend) WsAuth(context.Context, WsAuthRequest) (*WsAuthResponse, error) {
	return nil, ErrUnavailable
}

type SocketBackend struct {
	socketPath string
	counter    atomic.Uint64
}

type rpcRequest struct {
	ID     string         `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

func NewSocketBackend(socketPath string) *SocketBackend {
	return &SocketBackend{
		socketPath: socketPath,
	}
}

func (b *SocketBackend) QueueConfig(ctx context.Context) ([]QueueWorkerConfig, error) {
	var items []QueueWorkerConfig
	if err := b.call(ctx, "Queue.Config", nil, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (b *SocketBackend) PullQueue(ctx context.Context, queue string, limit int) ([]task.Envelope, error) {
	var items []task.Envelope
	if err := b.call(ctx, "Queue.Pull", map[string]any{
		"queue": queue,
		"limit": limit,
	}, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (b *SocketBackend) AckQueue(ctx context.Context, jobID string, result map[string]any) error {
	return b.call(ctx, "Queue.Ack", map[string]any{
		"job_id": jobID,
		"result": result,
	}, nil)
}

func (b *SocketBackend) FailQueue(ctx context.Context, jobID string, message string, retryable bool) error {
	return b.call(ctx, "Queue.Fail", map[string]any{
		"job_id":    jobID,
		"error":     message,
		"retryable": retryable,
	}, nil)
}

func (b *SocketBackend) PullSchedule(ctx context.Context, now time.Time, limit int) ([]task.Envelope, error) {
	var items []task.Envelope
	if err := b.call(ctx, "Schedule.Pull", map[string]any{
		"now":   now.Format(time.RFC3339),
		"limit": limit,
	}, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (b *SocketBackend) ReportSchedule(ctx context.Context, report ScheduleReport) error {
	return b.call(ctx, "Schedule.Report", map[string]any{
		"task_id": report.TaskID,
		"result":  report.Result,
		"error":   report.Error,
	}, nil)
}

func (b *SocketBackend) WsAuth(ctx context.Context, request WsAuthRequest) (*WsAuthResponse, error) {
	var response WsAuthResponse
	if err := b.call(ctx, "Ws.Auth", map[string]any{
		"app":   request.App,
		"token": request.Token,
		"meta":  request.Meta,
	}, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (b *SocketBackend) call(ctx context.Context, method string, params map[string]any, result any) error {
	if b.socketPath == "" {
		return ErrUnavailable
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", b.socketPath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	request := rpcRequest{
		ID:     fmt.Sprintf("%d", b.counter.Add(1)),
		Method: method,
		Params: params,
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	if _, err = conn.Write(payload); err != nil {
		return err
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return err
	}

	var response rpcResponse
	if err = json.Unmarshal(line, &response); err != nil {
		return err
	}
	if !response.OK {
		if response.Error == "" {
			return ErrUnavailable
		}
		return errors.New(response.Error)
	}
	if result == nil || len(response.Result) == 0 || string(response.Result) == "null" {
		return nil
	}
	return json.Unmarshal(response.Result, result)
}
