<?php

declare(strict_types=1);

namespace Core\Runtime;

use Core\App;
use Core\Handlers\Exception;
use Core\Queue\QueueJobMessage;
use Core\Queue\QueueJobMessageHandler;

class Worker
{
    public function run(int $maxJobs = 0): int
    {
        $jobs = 0;
        while (!feof(STDIN)) {
            $line = fgets(STDIN);
            if ($line === false) {
                break;
            }

            $line = trim($line);
            if (!$line) {
                continue;
            }

            $result = $this->handle($line);
            fwrite(STDOUT, json_encode($result, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES) . "\n");
            fflush(STDOUT);

            $jobs++;
            if ($maxJobs > 0 && $jobs >= $maxJobs) {
                break;
            }
        }

        return 0;
    }

    private function handle(string $line): array
    {
        $task = [];
        $id = '';

        try {
            $task = json_decode($line, true, 512, JSON_THROW_ON_ERROR);
            if (!is_array($task)) {
                throw new Exception('runtime worker task payload must be object');
            }
            $id = $task['id'] ?? '';
            $type = $task['type'] ?? '';
            $result = match ($type) {
                'queue' => $this->executeQueue($task),
                'schedule' => $this->executeSchedule($task),
                default => throw new Exception('runtime worker task type not supported: ' . $type),
            };

            return [
                'id' => $id,
                'ok' => true,
                'result' => $result,
                'error' => '',
                'retryable' => false,
            ];
        } catch (\Throwable $e) {
            App::log('runtime')->error($e->getMessage(), [
                'file' => $e->getFile() . ':' . $e->getLine(),
                'task_id' => $id,
            ]);

            return [
                'id' => $id,
                'ok' => false,
                'result' => [],
                'error' => $e->getMessage(),
                'retryable' => true,
            ];
        }
    }

    private function executeQueue(array $task): array
    {
        $payload = is_array($task['payload'] ?? null) ? $task['payload'] : [];
        $class = $payload['class'] ?? '';
        if (!$class) {
            throw new Exception('runtime queue task class is required');
        }

        putenv('DUX_QUEUE_WORK=' . ($payload['worker'] ?? ''));
        putenv('DUX_QUEUE_PRIORITY=' . ($payload['priority'] ?? 'medium'));
        putenv('DUX_QUEUE_RUN_ID=runtime-' . getmypid());

        (new QueueJobMessageHandler())->__invoke(new QueueJobMessage(
            $class,
            $payload['method'] ?? '',
            is_array($payload['params'] ?? null) ? $payload['params'] : [],
            $payload['priority'] ?? '',
            $task['id'] ?? ''
        ));

        return [
            'task' => $task['name'] ?? '',
            'worker' => $payload['worker'] ?? '',
            'priority' => $payload['priority'] ?? '',
        ];
    }

    private function executeSchedule(array $task): array
    {
        $payload = is_array($task['payload'] ?? null) ? $task['payload'] : [];
        $callback = $payload['callback'] ?? ($task['name'] ?? '');
        if (!$callback) {
            throw new Exception('runtime schedule callback is required');
        }

        App::scheduler()->executeCallback($callback, is_array($payload['params'] ?? null) ? $payload['params'] : []);

        return [
            'task' => $callback,
        ];
    }
}
