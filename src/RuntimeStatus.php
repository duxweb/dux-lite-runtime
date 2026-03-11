<?php

declare(strict_types=1);

namespace Core\Runtime;

use Core\App;

class RuntimeStatus
{
    public function __construct(
        private readonly string $socketPath,
        private readonly string $gatewaySocketPath,
        private readonly int $port,
        private readonly string $goCommand,
        private readonly string $workerCommand
    ) {
    }

    public function snapshot(): array
    {
        $queueConfig = $this->queueConfig();
        $queueStats = $this->queueStats();
        $schedulerJobs = App::scheduler()->loadJobs();
        $runtimeMetrics = $this->runtimeMetrics();
        $workerPool = is_array($runtimeMetrics['worker_pool'] ?? null) ? $runtimeMetrics['worker_pool'] : [];
        $ws = is_array($runtimeMetrics['websocket'] ?? null) ? $runtimeMetrics['websocket'] : [];

        return [
            'overview' => [
                ['Control endpoint', $this->socketPath],
                ['Gateway endpoint', $this->gatewaySocketPath],
                ['Realtime port', (string)$this->port],
                ['WS URL', 'ws://127.0.0.1:' . $this->port . '/ws'],
                ['Health URL', 'http://127.0.0.1:' . $this->port . '/healthz'],
                ['Metrics URL', 'http://127.0.0.1:' . $this->port . '/metrics'],
                ['Go command', $this->goCommand ?: 'disabled'],
                ['Worker command', $this->workerCommand],
                ['Control protocol', RuntimeConfig::endpointProtocol($this->socketPath)],
                ['Worker protocol', 'goridge frame over pipes'],
                ['Queue poll interval', RuntimeConfig::queuePollInterval()],
                ['Queue pull limit', (string)RuntimeConfig::queuePullLimit()],
                ['Queue dispatchers', (string)count($queueConfig)],
                ['Queue concurrency', (string)$this->queueConcurrency($queueConfig)],
                ['Scheduler poll interval', RuntimeConfig::schedulerPollInterval()],
                ['Scheduler jobs', (string)count($schedulerJobs)],
                ['Scheduler pull limit', (string)RuntimeConfig::schedulerPullLimit()],
                ['Min PHP worker pool', (string)RuntimeConfig::minWorkers()],
                ['Max PHP worker pool', RuntimeConfig::maxWorkers() > 0 ? (string)RuntimeConfig::maxWorkers() : 'unlimited'],
                ['Scale up step', (string)RuntimeConfig::scaleUpStep()],
                ['Worker idle TTL', RuntimeConfig::workerIdleTtl() . 's'],
                ['Current PHP worker pool', (string)($workerPool['current'] ?? RuntimeConfig::minWorkers())],
                ['Worker recycled', (string)($workerPool['recycled'] ?? 0)],
                ['Worker scaled up', (string)($workerPool['scaled_up'] ?? 0)],
                ['Worker scaled down', (string)($workerPool['scaled_down'] ?? 0)],
                ['WS online clients', (string)($ws['online'] ?? 0)],
                ['WS topics', (string)($ws['topics'] ?? 0)],
                ['Updated at', date('Y-m-d H:i:s')],
            ],
            'queue' => array_map(function (array $item) use ($queueStats): array {
                $stats = $queueStats[$item['name']] ?? [];
                $weights = $item['weights'] ?? [];
                return [
                    (string)($item['name'] ?? ''),
                    (string)($item['num'] ?? 0),
                    (string)($item['type'] ?? ''),
                    (string)($item['driver'] ?? ''),
                    sprintf(
                        '%d/%d/%d',
                        (int)($weights['high'] ?? 0),
                        (int)($weights['medium'] ?? 0),
                        (int)($weights['low'] ?? 0)
                    ),
                    $stats['pending'] === null ? '-' : (string)$stats['pending'],
                    $stats['running'] === null ? '-' : (string)$stats['running'],
                ];
            }, $queueConfig),
            'scheduler' => array_map(static function (array $item): array {
                return [
                    (string)($item['name'] ?? ''),
                    (string)($item['cron'] ?? ''),
                    (string)($item['callback'] ?? ''),
                    (string)($item['desc'] ?? ''),
                ];
            }, $schedulerJobs),
            'websocket' => [
                ['Online', (string)($ws['online'] ?? 0)],
                ['Topics', (string)($ws['topics'] ?? 0)],
                ['Subscribed', (string)($ws['subscribed'] ?? 0)],
                ['Unsubscribed', (string)($ws['unsubscribed'] ?? 0)],
                ['Published', (string)($ws['published'] ?? 0)],
                ['Kicked', (string)($ws['kicked'] ?? 0)],
            ],
        ];
    }

    private function queueConfig(): array
    {
        $workers = App::config('queue')->get('workers', []);
        if (!is_array($workers) || !$workers) {
            return [];
        }
        return App::queue()->runtimeConfig();
    }

    private function queueStats(): array
    {
        $items = [];
        foreach (App::queue()->stats() as $row) {
            $name = (string)($row['name'] ?? '');
            if (!$name) {
                continue;
            }
            $items[$name] = [
                'pending' => $row['pending'] ?? null,
                'running' => $row['running'] ?? null,
            ];
        }
        return $items;
    }

    private function queueConcurrency(array $queueConfig): int
    {
        return array_sum(array_map(static fn (array $item): int => (int)($item['num'] ?? 0), $queueConfig));
    }

    private function runtimeMetrics(): array
    {
        $port = RuntimeConfig::realtimeAddr();
        $url = str_starts_with($port, ':') ? 'http://127.0.0.1' . $port : 'http://' . $port;
        $payload = @file_get_contents(rtrim($url, '/') . '/metrics');
        if (!$payload) {
            return [];
        }

        $data = json_decode($payload, true);
        if (!is_array($data) || !is_array($data['runtime'] ?? null)) {
            return [];
        }

        return $data['runtime'];
    }
}
