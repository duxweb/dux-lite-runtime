<?php

declare(strict_types=1);

namespace Core\Runtime;

use Core\App;

class RuntimeConfig
{
    public static function get(string $key, mixed $default = null): mixed
    {
        return App::config('use')->get('runtime.' . $key, $default);
    }

    public static function socketPath(): string
    {
        return self::absolutePath((string)self::get('control_socket', data_path('runtime/master.sock')));
    }

    public static function gatewaySocketPath(): string
    {
        return self::absolutePath((string)self::get('gateway_socket', data_path('runtime/gateway.sock')));
    }

    public static function realtimePort(): int
    {
        $port = (int)self::get('port', 9504);
        return $port > 0 ? $port : 9504;
    }

    public static function realtimeAddr(): string
    {
        return (string)self::get('realtime_addr', ':' . self::realtimePort());
    }

    public static function goCommand(): string
    {
        return (string)self::get('go_command', '');
    }

    public static function resolvedGoCommand(): string
    {
        $command = self::goCommand();
        if ($command !== '') {
            return $command;
        }

        [$os, $arch, $ext] = self::currentPlatform();
        $candidates = [
            "/runtime/bin/dux-runtime{$ext}",
            "/runtime/bin/dux-runtime-{$os}-{$arch}{$ext}",
            "/runtime/bin/{$os}/{$arch}/dux-runtime{$ext}",
            "/runtime/dux-runtime{$ext}",
            "/runtime/dux-runtime-{$os}-{$arch}{$ext}",
        ];

        foreach (self::runtimeRoots() as $root) {
            foreach ($candidates as $item) {
                $file = $root . $item;
                if (is_file($file)) {
                    return self::relativeRuntimeCommand($root, $item);
                }
            }
        }

        return '';
    }

    public static function workerCommand(): string
    {
        return (string)self::get('worker_command', 'php dux runtime --worker');
    }

    public static function workerMaxJobs(): int
    {
        $value = (int)self::get('worker_max_jobs', 1000);
        return $value > 0 ? $value : 1000;
    }

    public static function watchStatusInterval(): int
    {
        $value = (int)self::get('status_interval', 10);
        return $value > 0 ? $value : 10;
    }

    public static function wsFallback(): bool
    {
        return (bool)self::get('ws_fallback', false);
    }

    public static function wsAuthCallback(): string
    {
        return (string)self::get('ws_auth_callback', '');
    }

    public static function minWorkers(): int
    {
        $value = (int)self::get('min_workers', self::get('workers', 4));
        return $value > 0 ? $value : 4;
    }

    public static function maxWorkers(): int
    {
        $value = (int)self::get('max_workers', 0);
        return $value >= 0 ? $value : 0;
    }

    public static function scaleUpStep(): int
    {
        $value = (int)self::get('scale_up_step', 1);
        return $value > 0 ? $value : 1;
    }

    public static function workerIdleTtl(): int
    {
        $value = (int)self::get('worker_idle_ttl', 300);
        return $value > 0 ? $value : 300;
    }

    public static function queuePollInterval(): string
    {
        return (string)self::get('queue_poll_interval', '1s');
    }

    public static function queuePullLimit(): int
    {
        $value = (int)self::get('queue_pull_limit', 8);
        return $value > 0 ? $value : 8;
    }

    public static function queueConfigRefresh(): string
    {
        return (string)self::get('queue_config_refresh', '10s');
    }

    public static function schedulerPollInterval(): string
    {
        return (string)self::get('schedule_poll_interval', '1s');
    }

    public static function schedulerPullLimit(): int
    {
        $value = (int)self::get('schedule_pull_limit', 8);
        return $value > 0 ? $value : 8;
    }

    public static function taskTimeout(): int
    {
        $value = (int)self::get('task_timeout', 30);
        return $value > 0 ? $value : 30;
    }

    private static function absolutePath(string $path): string
    {
        $path = trim($path);
        if ($path === '') {
            return $path;
        }

        if (str_starts_with($path, '/') || preg_match('/^[A-Za-z]:[\\\\\\/]/', $path)) {
            return $path;
        }

        return base_path($path);
    }

    private static function currentPlatform(): array
    {
        $os = match (strtolower(PHP_OS_FAMILY)) {
            'windows' => 'windows',
            'darwin' => 'darwin',
            default => 'linux',
        };
        $machine = strtolower(php_uname('m'));
        $arch = match (true) {
            str_contains($machine, 'aarch64'), str_contains($machine, 'arm64') => 'arm64',
            default => 'amd64',
        };
        $ext = $os === 'windows' ? '.exe' : '';

        return [$os, $arch, $ext];
    }

    private static function runtimeRoots(): array
    {
        $roots = [
            dirname(__DIR__),
            base_path(),
        ];

        $items = [];
        foreach ($roots as $root) {
            $root = rtrim((string)$root, DIRECTORY_SEPARATOR);
            if ($root === '' || isset($items[$root])) {
                continue;
            }
            $items[$root] = $root;
        }

        return array_values($items);
    }

    private static function relativeRuntimeCommand(string $root, string $path): string
    {
        $appRoot = rtrim(base_path(), DIRECTORY_SEPARATOR);
        $root = rtrim($root, DIRECTORY_SEPARATOR);
        if ($root === $appRoot) {
            return '.' . $path;
        }
        return $root . $path;
    }
}
