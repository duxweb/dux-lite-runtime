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
            'Ws.Subscribe' => $this->authorize('runtime.ws.subscribe', $params),
            'Ws.Publish' => $this->authorize('runtime.ws.publish', $params),
            'Ws.Event' => $this->event($params),
            'Ws.Message' => $this->message($params),
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
        $callback = RuntimeConfig::wsAuthCallback();
        if (!$callback) {
            $event = new WsEvent('runtime.ws.auth', $params);
            App::event()->dispatch($event, 'runtime.ws.auth');
            $response = $event->response();
            $response['meta'] = $this->normalizeMeta($response['meta'] ?? []);
            return $response;
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
            'meta' => $this->normalizeMeta(is_array($auth['meta'] ?? null) ? $auth['meta'] : []),
        ];
    }

    private function authorize(string $eventName, array $params): array
    {
        $event = new WsEvent($eventName, $params);
        App::event()->dispatch($event, $eventName);

        return [
            'allow' => $event->allowed(),
            'meta' => $this->normalizeMeta($event->response()),
        ];
    }

    private function event(array $params): array
    {
        $name = (string)($params['event'] ?? '');
        if (!$name) {
            throw new Exception('runtime ws event is required');
        }

        $event = new WsEvent('runtime.ws.' . $name, $params);
        App::event()->dispatch($event, 'runtime.ws.' . $name);

        return [
            'ok' => true,
            'meta' => $this->normalizeMeta($event->response()),
        ];
    }

    private function message(array $params): array
    {
        $event = new WsEvent('runtime.ws.message', $params);
        App::event()->dispatch($event, 'runtime.ws.message');

        return [
            'ok' => $event->allowed(),
            'meta' => $this->normalizeMeta($event->response()),
        ];
    }

    private function normalizeMeta(mixed $meta): mixed
    {
        if (!is_array($meta)) {
            return $meta ?? new \stdClass();
        }

        if ($meta === []) {
            return new \stdClass();
        }

        return $meta;
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
