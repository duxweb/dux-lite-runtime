package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	LogLevel              string
	Workers               int
	MaxWorkers            int
	ScaleUpStep           int
	WorkerMaxJobs         int
	WorkerMaxMemoryMB     int
	WorkerIdleTTLSeconds  int
	RestartOnCrash        bool
	TaskTimeoutSeconds    int
	ControlSocketPath     string
	GatewaySocketPath     string
	ProjectRoot           string
	PHPWorkerCommand      string
	PHPWorkerWorkdir      string
	QueueNames            []string
	QueueConfigRefresh    time.Duration
	QueuePullLimit        int
	QueuePollInterval     time.Duration
	SchedulerPullLimit    int
	SchedulerPollInterval time.Duration
	RealtimeEnabled       bool
	RealtimeListenAddr    string
	WSMaxMessageSize      int64
}

func Load() (*Config, error) {
	projectRoot := inferProjectRoot()
	cfg := &Config{
		LogLevel:              getenv("DUX_RUNTIME_LOG_LEVEL", "info"),
		Workers:               getenvInt("DUX_RUNTIME_WORKERS", 4),
		MaxWorkers:            getenvInt("DUX_RUNTIME_MAX_WORKERS", 0),
		ScaleUpStep:           getenvInt("DUX_RUNTIME_SCALE_UP_STEP", 1),
		WorkerMaxJobs:         getenvInt("DUX_RUNTIME_WORKER_MAX_JOBS", 1000),
		WorkerMaxMemoryMB:     getenvInt("DUX_RUNTIME_WORKER_MAX_MEMORY_MB", 256),
		WorkerIdleTTLSeconds:  getenvInt("DUX_RUNTIME_WORKER_IDLE_TTL", 300),
		RestartOnCrash:        getenvBool("DUX_RUNTIME_RESTART_ON_CRASH", true),
		TaskTimeoutSeconds:    getenvInt("DUX_RUNTIME_TASK_TIMEOUT", 30),
		ControlSocketPath:     getenv("DUX_RUNTIME_CONTROL_SOCKET", defaultControlEndpoint()),
		GatewaySocketPath:     getenv("DUX_RUNTIME_GATEWAY_SOCKET", defaultGatewayEndpoint()),
		ProjectRoot:           projectRoot,
		PHPWorkerCommand:      getenv("DUX_RUNTIME_PHP_WORKER_COMMAND", inferWorkerCommand(projectRoot)),
		PHPWorkerWorkdir:      getenv("DUX_RUNTIME_PHP_WORKER_WORKDIR", projectRoot),
		QueueNames:            getenvList("DUX_RUNTIME_QUEUES", []string{"default"}),
		QueueConfigRefresh:    getenvDuration("DUX_RUNTIME_QUEUE_CONFIG_REFRESH", 10*time.Second),
		QueuePullLimit:        getenvInt("DUX_RUNTIME_QUEUE_PULL_LIMIT", 8),
		QueuePollInterval:     getenvDuration("DUX_RUNTIME_QUEUE_POLL_INTERVAL", time.Second),
		SchedulerPullLimit:    getenvInt("DUX_RUNTIME_SCHEDULE_PULL_LIMIT", 8),
		SchedulerPollInterval: getenvDuration("DUX_RUNTIME_SCHEDULE_POLL_INTERVAL", time.Second),
		RealtimeEnabled:       getenvBool("DUX_RUNTIME_REALTIME_ENABLED", true),
		RealtimeListenAddr:    getenv("DUX_RUNTIME_REALTIME_ADDR", ":9504"),
		WSMaxMessageSize:      getenvInt64("DUX_RUNTIME_WS_MAX_MESSAGE_SIZE", 32*1024*1024),
	}

	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.ScaleUpStep <= 0 {
		cfg.ScaleUpStep = 1
	}
	if cfg.QueuePullLimit <= 0 {
		cfg.QueuePullLimit = 1
	}
	if cfg.SchedulerPullLimit <= 0 {
		cfg.SchedulerPullLimit = 1
	}
	if cfg.TaskTimeoutSeconds <= 0 {
		cfg.TaskTimeoutSeconds = 30
	}
	if cfg.QueuePollInterval <= 0 {
		cfg.QueuePollInterval = time.Second
	}
	if cfg.QueueConfigRefresh <= 0 {
		cfg.QueueConfigRefresh = 10 * time.Second
	}
	if cfg.SchedulerPollInterval <= 0 {
		cfg.SchedulerPollInterval = time.Second
	}
	if cfg.WSMaxMessageSize <= 0 {
		cfg.WSMaxMessageSize = 32 * 1024 * 1024
	}

	return cfg, nil
}

func defaultControlEndpoint() string {
	if runtime.GOOS == "windows" {
		return "tcp://127.0.0.1:0"
	}
	return "/tmp/dux-lite-runtime.sock"
}

func defaultGatewayEndpoint() string {
	if runtime.GOOS == "windows" {
		return "tcp://127.0.0.1:0"
	}
	return "/tmp/dux-lite-gateway.sock"
}

func inferProjectRoot() string {
	if value := os.Getenv("DUX_RUNTIME_PROJECT_ROOT"); value != "" {
		return value
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return ""
	}
	workingDir, _ = filepath.Abs(workingDir)
	if isFile(workingDir + "/dux") {
		return workingDir
	}
	parent := filepath.Dir(workingDir)
	if isFile(parent + "/dux") {
		return parent
	}
	return workingDir
}

func inferWorkerCommand(projectRoot string) string {
	if projectRoot == "" {
		return ""
	}
	if isFile(projectRoot + "/dux") {
		return "php dux runtime --worker"
	}
	return ""
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return number
}

func getenvInt64(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return number
}

func getenvBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return duration
}

func getenvList(key string, fallback []string) []string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		result = append(result, part)
	}
	if len(result) == 0 {
		return fallback
	}
	return result
}
