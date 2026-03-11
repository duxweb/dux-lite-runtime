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
        return self::resolvedEndpoint('control', (string)self::get('control_socket', self::defaultControlEndpoint()));
    }

    public static function gatewaySocketPath(): string
    {
        return self::resolvedEndpoint('gateway', (string)self::get('gateway_socket', self::defaultGatewayEndpoint()));
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

    public static function endpointProtocol(string $endpoint): string
    {
        return self::isTcpEndpoint($endpoint) ? 'goridge frame over tcp' : 'goridge frame over unix socket';
    }

    public static function isTcpEndpoint(string $endpoint): bool
    {
        return str_starts_with(strtolower(trim($endpoint)), 'tcp://');
    }

    public static function streamServerUri(string $endpoint): string
    {
        return self::isTcpEndpoint($endpoint) ? $endpoint : 'unix://' . $endpoint;
    }

    public static function persistEndpoint(string $name, string $endpoint): void
    {
        $file = self::endpointStateFile($name);
        $dir = dirname($file);
        if (!is_dir($dir)) {
            mkdir($dir, 0777, true);
        }
        file_put_contents($file, $endpoint);
    }

    public static function clearPersistedEndpoint(string $name): void
    {
        $file = self::endpointStateFile($name);
        if (is_file($file)) {
            @unlink($file);
        }
    }

    private static function endpoint(string $endpoint): string
    {
        $endpoint = trim($endpoint);
        if ($endpoint === '') {
            return $endpoint;
        }

        if (self::isTcpEndpoint($endpoint)) {
            return $endpoint;
        }

        if (str_starts_with($endpoint, '/') || preg_match('/^[A-Za-z]:[\\\\\\/]/', $endpoint)) {
            return $endpoint;
        }

        return base_path($endpoint);
    }

    private static function resolvedEndpoint(string $name, string $endpoint): string
    {
        $endpoint = self::endpoint($endpoint);
        if (!self::isDynamicTcpEndpoint($endpoint)) {
            return $endpoint;
        }

        $resolved = self::persistedEndpoint($name);
        return $resolved !== '' ? $resolved : $endpoint;
    }

    private static function persistedEndpoint(string $name): string
    {
        $file = self::endpointStateFile($name);
        if (!is_file($file)) {
            return '';
        }
        return trim((string)file_get_contents($file));
    }

    private static function endpointStateFile(string $name): string
    {
        return data_path('runtime/' . $name . '.endpoint');
    }

    private static function isDynamicTcpEndpoint(string $endpoint): bool
    {
        if (!self::isTcpEndpoint($endpoint)) {
            return false;
        }

        $parts = parse_url($endpoint);
        return (int)($parts['port'] ?? 0) === 0;
    }

    private static function defaultControlEndpoint(): string
    {
        if (strtolower(PHP_OS_FAMILY) === 'windows') {
            return 'tcp://127.0.0.1:0';
        }
        return data_path('runtime/master.sock');
    }

    private static function defaultGatewayEndpoint(): string
    {
        if (strtolower(PHP_OS_FAMILY) === 'windows') {
            return 'tcp://127.0.0.1:0';
        }
        return data_path('runtime/gateway.sock');
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
