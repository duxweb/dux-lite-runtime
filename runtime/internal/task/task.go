package task

import "time"

type Kind string

const (
	KindQueue    Kind = "queue"
	KindSchedule Kind = "schedule"
)

type Envelope struct {
	ID             string         `json:"id"`
	Type           Kind           `json:"type"`
	Name           string         `json:"name"`
	Queue          string         `json:"queue,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
	Attempt        int            `json:"attempt"`
	TimeoutSeconds int            `json:"timeout"`
	Metadata       map[string]any `json:"meta,omitempty"`
}

type Result struct {
	ID         string         `json:"id"`
	OK         bool           `json:"ok"`
	Result     map[string]any `json:"result,omitempty"`
	Error      string         `json:"error,omitempty"`
	Retryable  bool           `json:"retryable"`
	FinishedAt time.Time      `json:"finished_at"`
}
