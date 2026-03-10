package status

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/duxweb/dux-runtime/runtime/internal/phpmaster"
)

type State struct {
	startedAt time.Time

	workerPoolConfigured atomic.Int64
	workerPoolStarted    atomic.Int64
	workerPoolCurrent    atomic.Int64
	workerPoolRecycled   atomic.Uint64
	workerPoolBroken     atomic.Uint64
	workerPoolScaledUp   atomic.Uint64
	workerPoolScaledDown atomic.Uint64

	queuePulled atomic.Uint64
	queueAcked  atomic.Uint64
	queueFailed atomic.Uint64

	schedulerPulled   atomic.Uint64
	schedulerReported atomic.Uint64
	schedulerFailed   atomic.Uint64

	wsOnline       atomic.Int64
	wsTopics       atomic.Int64
	wsSubscribed   atomic.Uint64
	wsUnsubscribed atomic.Uint64
	wsPublished    atomic.Uint64
	wsKicked       atomic.Uint64

	mu           sync.RWMutex
	queueWorkers map[string]QueueWorker
}

type QueueWorker struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Driver  string `json:"driver"`
	Num     int    `json:"num"`
	Active  int    `json:"active"`
	Pending int    `json:"pending"`
	Running int    `json:"running"`
}

type Snapshot struct {
	StartedAt  string        `json:"started_at"`
	WorkerPool WorkerPool    `json:"worker_pool"`
	Queue      QueueMetrics  `json:"queue"`
	Scheduler  SchedulerData `json:"scheduler"`
	WebSocket  WebSocketData `json:"websocket"`
}

type WorkerPool struct {
	Configured int64  `json:"configured"`
	Started    int64  `json:"started"`
	Current    int64  `json:"current"`
	Recycled   uint64 `json:"recycled"`
	Broken     uint64 `json:"broken"`
	ScaledUp   uint64 `json:"scaled_up"`
	ScaledDown uint64 `json:"scaled_down"`
}

type QueueMetrics struct {
	Pulled      uint64        `json:"pulled"`
	Acked       uint64        `json:"acked"`
	Failed      uint64        `json:"failed"`
	Dispatchers []QueueWorker `json:"dispatchers"`
}

type SchedulerData struct {
	Pulled   uint64 `json:"pulled"`
	Reported uint64 `json:"reported"`
	Failed   uint64 `json:"failed"`
}

type WebSocketData struct {
	Online       int64  `json:"online"`
	Topics       int64  `json:"topics"`
	Subscribed   uint64 `json:"subscribed"`
	Unsubscribed uint64 `json:"unsubscribed"`
	Published    uint64 `json:"published"`
	Kicked       uint64 `json:"kicked"`
}

func New() *State {
	return &State{
		startedAt:    time.Now(),
		queueWorkers: map[string]QueueWorker{},
	}
}

func (s *State) SetWorkerPoolConfigured(n int) {
	s.workerPoolConfigured.Store(int64(n))
}

func (s *State) SetWorkerPoolStarted(n int) {
	s.workerPoolStarted.Store(int64(n))
}

func (s *State) SetWorkerPoolCurrent(n int) {
	s.workerPoolCurrent.Store(int64(n))
}

func (s *State) IncWorkerPoolRecycled(broken bool) {
	s.workerPoolRecycled.Add(1)
	if broken {
		s.workerPoolBroken.Add(1)
	}
}

func (s *State) IncWorkerPoolScaledUp() {
	s.workerPoolScaledUp.Add(1)
}

func (s *State) IncWorkerPoolScaledDown() {
	s.workerPoolScaledDown.Add(1)
}

func (s *State) AddWorkerPoolScaledUp(n int) {
	if n > 0 {
		s.workerPoolScaledUp.Add(uint64(n))
	}
}

func (s *State) AddWorkerPoolScaledDown(n int) {
	if n > 0 {
		s.workerPoolScaledDown.Add(uint64(n))
	}
}

func (s *State) SetQueueWorkers(items []phpmaster.QueueWorkerConfig, active map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := map[string]QueueWorker{}
	for _, item := range items {
		next[item.Name] = QueueWorker{
			Name:   item.Name,
			Type:   item.Type,
			Driver: item.Driver,
			Num:    item.Num,
			Active: active[item.Name],
		}
	}
	s.queueWorkers = next
}

func (s *State) SetQueueWorkerActive(name string, active int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.queueWorkers[name]
	if !ok {
		item = QueueWorker{Name: name}
	}
	item.Active = active
	s.queueWorkers[name] = item
}

func (s *State) IncQueuePulled(n int) {
	s.queuePulled.Add(uint64(n))
}

func (s *State) IncQueueAcked() {
	s.queueAcked.Add(1)
}

func (s *State) IncQueueFailed() {
	s.queueFailed.Add(1)
}

func (s *State) IncSchedulerPulled(n int) {
	s.schedulerPulled.Add(uint64(n))
}

func (s *State) IncSchedulerReported(failed bool) {
	s.schedulerReported.Add(1)
	if failed {
		s.schedulerFailed.Add(1)
	}
}

func (s *State) SetWSOnline(n int) {
	s.wsOnline.Store(int64(n))
}

func (s *State) SetWSTopics(n int) {
	s.wsTopics.Store(int64(n))
}

func (s *State) IncWSSubscribed() {
	s.wsSubscribed.Add(1)
}

func (s *State) IncWSUnsubscribed() {
	s.wsUnsubscribed.Add(1)
}

func (s *State) IncWSPublished() {
	s.wsPublished.Add(1)
}

func (s *State) IncWSKicked() {
	s.wsKicked.Add(1)
}

func (s *State) Snapshot() Snapshot {
	s.mu.RLock()
	dispatchers := make([]QueueWorker, 0, len(s.queueWorkers))
	for _, item := range s.queueWorkers {
		dispatchers = append(dispatchers, item)
	}
	s.mu.RUnlock()

	return Snapshot{
		StartedAt: s.startedAt.Format(time.RFC3339),
		WorkerPool: WorkerPool{
			Configured: s.workerPoolConfigured.Load(),
			Started:    s.workerPoolStarted.Load(),
			Current:    s.workerPoolCurrent.Load(),
			Recycled:   s.workerPoolRecycled.Load(),
			Broken:     s.workerPoolBroken.Load(),
			ScaledUp:   s.workerPoolScaledUp.Load(),
			ScaledDown: s.workerPoolScaledDown.Load(),
		},
		Queue: QueueMetrics{
			Pulled:      s.queuePulled.Load(),
			Acked:       s.queueAcked.Load(),
			Failed:      s.queueFailed.Load(),
			Dispatchers: dispatchers,
		},
		Scheduler: SchedulerData{
			Pulled:   s.schedulerPulled.Load(),
			Reported: s.schedulerReported.Load(),
			Failed:   s.schedulerFailed.Load(),
		},
		WebSocket: WebSocketData{
			Online:       s.wsOnline.Load(),
			Topics:       s.wsTopics.Load(),
			Subscribed:   s.wsSubscribed.Load(),
			Unsubscribed: s.wsUnsubscribed.Load(),
			Published:    s.wsPublished.Load(),
			Kicked:       s.wsKicked.Load(),
		},
	}
}
