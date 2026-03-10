<?php

declare(strict_types=1);

namespace Core\Runtime;

use Core\App;
use Core\Handlers\Exception;
use RuntimeException;

class Control
{
    public function handle(string $method, array $params = []): mixed
    {
        return match ($method) {
            'Ping' => ['pong' => true, 'time' => date(DATE_ATOM)],
            'Queue.Config' => $this->queueConfig(),
            'Queue.Pull' => $this->pullQueue($params),
            'Queue.Ack' => ['ok' => $this->ackQueue($params)],
            'Queue.Fail' => ['ok' => $this->failQueue($params)],
            'Schedule.Pull' => App::scheduler()->pullRuntimeTasks($params['now'] ?? 'now', (int)($params['limit'] ?? 1)),
            'Schedule.Report' => $this->reportSchedule($params),
            'Ws.Auth' => $this->auth($params),
            default => throw new Exception('runtime method not supported: ' . $method),
        };
    }

    private function reportSchedule(array $params): array
    {
        App::scheduler()->reportRuntimeTask(
            $params['task_id'] ?? '',
            is_array($params['result'] ?? null) ? $params['result'] : [],
            $params['error'] ?? ''
        );
        return ['ok' => true];
    }

    private function auth(array $params): array
    {
        $callback = getenv('DUX_RUNTIME_WS_AUTH_CALLBACK') ?: '';
        if (!$callback) {
            throw new Exception('runtime ws auth callback not configured');
        }

        [$class, $method] = str_contains($callback, ':') ? explode(':', $callback, 2) : [$callback, '__invoke'];
        if (!class_exists($class)) {
            throw new Exception($class . ' does not exist');
        }
        if (!method_exists($class, $method)) {
            throw new Exception($class . ':' . $method . ' does not exist');
        }

        $auth = call_user_func([new $class, $method], $params['app'] ?? '', $params['token'] ?? '', is_array($params['meta'] ?? null) ? $params['meta'] : []);
        if (!is_array($auth)) {
            throw new Exception('runtime ws auth callback must return array');
        }

        return [
            'client_id' => $auth['client_id'] ?? '',
            'client_type' => $auth['client_type'] ?? '',
            'allow_subscribe' => is_array($auth['allow_subscribe'] ?? null) ? $auth['allow_subscribe'] : [],
            'allow_publish' => is_array($auth['allow_publish'] ?? null) ? $auth['allow_publish'] : [],
            'meta' => is_array($auth['meta'] ?? null) ? $auth['meta'] : [],
        ];
    }

    private function pullQueue(array $params): array
    {
        if (!$this->hasQueueWorker()) {
            return [];
        }

        try {
            return App::queue()->pull($params['queue'] ?? '', (int)($params['limit'] ?? 1));
        } catch (RuntimeException $e) {
            if (str_contains($e->getMessage(), 'Queue worker not configured') || str_contains($e->getMessage(), 'Queue default worker is not configured')) {
                return [];
            }
            throw $e;
        }
    }

    private function ackQueue(array $params): bool
    {
        if (!$this->hasQueueWorker()) {
            return false;
        }
        return App::queue()->ack($params['job_id'] ?? '', is_array($params['result'] ?? null) ? $params['result'] : []);
    }

    private function failQueue(array $params): bool
    {
        if (!$this->hasQueueWorker()) {
            return false;
        }
        return App::queue()->fail($params['job_id'] ?? '', $params['error'] ?? '', (bool)($params['retryable'] ?? true));
    }

    private function hasQueueWorker(): bool
    {
        $workers = App::config('queue')->get('workers', []);
        return is_array($workers) && (bool)$workers;
    }

    private function queueConfig(): array
    {
        if (!$this->hasQueueWorker()) {
            return [];
        }
        return App::queue()->runtimeConfig();
    }
}
