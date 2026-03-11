<?php

declare(strict_types=1);

namespace Core\Runtime;

use Core\App;
use Spiral\Goridge\Frame;
use Symfony\Component\Console\Output\BufferedOutput;
use Symfony\Component\Console\Output\ConsoleOutputInterface;
use Symfony\Component\Console\Output\ConsoleSectionOutput;
use Symfony\Component\Console\Output\OutputInterface;
use Symfony\Component\Process\Process;

class Master
{
    private bool $running = true;
    private ?ConsoleSectionOutput $statusSection = null;

    public function __construct(
        private string $socketPath,
        private string $gatewaySocketPath,
        private int $port,
        private string $goCommand,
        private string $workerCommand,
        private OutputInterface $output,
        private bool $showStatus = true,
        private int $statusInterval = 10
    ) {
    }

    public function run(): int
    {
        $this->bootSignals();
        $server = $this->createControlServer();
        if (!$server) {
            throw new \RuntimeException('runtime endpoint listen failed');
        }

        stream_set_blocking($server, false);
        $this->resolveGatewayEndpoint();
        RuntimeConfig::persistEndpoint('control', $this->socketPath);
        RuntimeConfig::persistEndpoint('gateway', $this->gatewaySocketPath);

        $process = $this->startGoProcess();
        $nextStatusAt = time();

        if ($this->showStatus && $this->output instanceof ConsoleOutputInterface) {
            $this->statusSection = $this->output->section();
        }

        try {
            while ($this->running) {
                $client = @stream_socket_accept($server, 1);
                if ($client) {
                    $this->handleClient($client);
                }

                if ($this->showStatus && time() >= $nextStatusAt) {
                    $this->renderStatus();
                    $nextStatusAt = time() + $this->statusInterval;
                }

                if ($process && !$process->isRunning()) {
                    $this->output->writeln('<error>Go runtime exited</error>');
                    return $process->getExitCode() ?: 1;
                }
            }
        } finally {
            fclose($server);
            $this->cleanupEndpoint();
            RuntimeConfig::clearPersistedEndpoint('control');
            RuntimeConfig::clearPersistedEndpoint('gateway');

            if ($process && $process->isRunning()) {
                $process->stop(3);
            }
        }

        return 0;
    }

    private function startGoProcess(): ?Process
    {
        if (!$this->goCommand) {
            $this->output->writeln('<comment>runtime go command not configured, php master only</comment>');
            return null;
        }

        $process = Process::fromShellCommandline($this->goCommand, base_path());
        $process->setTimeout(null);
        $process->setIdleTimeout(null);
        $process->setEnv([
            ...$_ENV,
            'DUX_RUNTIME_CONTROL_SOCKET' => $this->socketPath,
            'DUX_RUNTIME_GATEWAY_SOCKET' => $this->gatewaySocketPath,
            'DUX_RUNTIME_REALTIME_ADDR' => ':' . $this->port,
            'DUX_RUNTIME_WORKERS' => (string)RuntimeConfig::minWorkers(),
            'DUX_RUNTIME_MAX_WORKERS' => (string)RuntimeConfig::maxWorkers(),
            'DUX_RUNTIME_SCALE_UP_STEP' => (string)RuntimeConfig::scaleUpStep(),
            'DUX_RUNTIME_WORKER_MAX_JOBS' => (string)RuntimeConfig::workerMaxJobs(),
            'DUX_RUNTIME_WORKER_IDLE_TTL' => (string)RuntimeConfig::workerIdleTtl(),
            'DUX_RUNTIME_TASK_TIMEOUT' => (string)RuntimeConfig::taskTimeout(),
            'DUX_RUNTIME_QUEUE_POLL_INTERVAL' => RuntimeConfig::queuePollInterval(),
            'DUX_RUNTIME_QUEUE_PULL_LIMIT' => (string)RuntimeConfig::queuePullLimit(),
            'DUX_RUNTIME_QUEUE_CONFIG_REFRESH' => RuntimeConfig::queueConfigRefresh(),
            'DUX_RUNTIME_SCHEDULE_POLL_INTERVAL' => RuntimeConfig::schedulerPollInterval(),
            'DUX_RUNTIME_SCHEDULE_PULL_LIMIT' => (string)RuntimeConfig::schedulerPullLimit(),
        ]);
        $process->start(function ($type, $buffer) {
            unset($type);
            if (!$this->showStatus) {
                $this->output->write($buffer);
            }
        });

        if (!$this->showStatus) {
            $this->output->writeln('<info>runtime go command started</info>');
        }
        return $process;
    }

    private function handleClient($client): void
    {
        stream_set_blocking($client, true);
        stream_set_timeout($client, 5);
        $frame = $this->receiveFrame($client);
        if (!$frame) {
            fclose($client);
            return;
        }

        $response = [
            'id' => '',
            'ok' => false,
            'result' => null,
            'error' => '',
        ];

        try {
            $payload = json_decode((string)$frame->payload, true, 512, JSON_THROW_ON_ERROR);
            if (!is_array($payload)) {
                throw new \RuntimeException('runtime request payload must be object');
            }
            $response['id'] = $payload['id'] ?? '';
            $response['result'] = (new Control())->handle($payload['method'] ?? '', is_array($payload['params'] ?? null) ? $payload['params'] : []);
            $response['ok'] = true;
        } catch (\Throwable $e) {
            $response['error'] = $e->getMessage();
            App::log('runtime')->error($e->getMessage(), [
                'file' => $e->getFile() . ':' . $e->getLine(),
            ]);
        }

        fwrite($client, Frame::packFrame(new Frame(
            json_encode($response, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES),
            [],
            Frame::CODEC_JSON
        )));
        fclose($client);
    }

    private function createControlServer()
    {
        $this->prepareEndpoint($this->socketPath);
        $server = @stream_socket_server(RuntimeConfig::streamServerUri($this->socketPath), $errno, $error);
        if (!$server) {
            throw new \RuntimeException('runtime control endpoint listen failed: ' . $error . ' (' . $errno . ')');
        }
        $this->socketPath = $this->resolveBoundEndpoint($this->socketPath, $server);
        return $server;
    }

    private function resolveGatewayEndpoint(): void
    {
        if (!RuntimeConfig::isTcpEndpoint($this->gatewaySocketPath)) {
            return;
        }
        $this->gatewaySocketPath = $this->reserveTcpEndpoint($this->gatewaySocketPath);
    }

    private function prepareEndpoint(string $endpoint): void
    {
        if (RuntimeConfig::isTcpEndpoint($endpoint)) {
            return;
        }

        $dir = dirname($endpoint);
        if (!is_dir($dir)) {
            mkdir($dir, 0777, true);
        }
        if (is_file($endpoint) || file_exists($endpoint)) {
            @unlink($endpoint);
        }
    }

    private function cleanupEndpoint(): void
    {
        if (RuntimeConfig::isTcpEndpoint($this->socketPath)) {
            return;
        }
        if (is_file($this->socketPath) || file_exists($this->socketPath)) {
            @unlink($this->socketPath);
        }
    }

    private function resolveBoundEndpoint(string $endpoint, $server): string
    {
        if (!RuntimeConfig::isTcpEndpoint($endpoint)) {
            return $endpoint;
        }

        $name = (string)stream_socket_get_name($server, false);
        if ($name === '') {
            return $endpoint;
        }
        return str_starts_with($name, 'tcp://') ? $name : 'tcp://' . $name;
    }

    private function reserveTcpEndpoint(string $endpoint): string
    {
        $server = @stream_socket_server($endpoint, $errno, $error);
        if (!$server) {
            throw new \RuntimeException('runtime gateway endpoint reserve failed: ' . $error . ' (' . $errno . ')');
        }
        $name = (string)stream_socket_get_name($server, false);
        fclose($server);
        if ($name === '') {
            return $endpoint;
        }
        return str_starts_with($name, 'tcp://') ? $name : 'tcp://' . $name;
    }

    private function bootSignals(): void
    {
        if (!function_exists('pcntl_async_signals')) {
            return;
        }

        pcntl_async_signals(true);
        pcntl_signal(SIGINT, function () {
            $this->running = false;
        });
        pcntl_signal(SIGTERM, function () {
            $this->running = false;
        });
    }

    private function renderStatus(): void
    {
        $status = (new RuntimeStatus(
            $this->socketPath,
            $this->gatewaySocketPath,
            $this->port,
            $this->goCommand,
            $this->workerCommand
        ))->snapshot();

        $output = new BufferedOutput();
        $output->writeln('');
        RuntimeStatusRenderer::render($output, $status);

        $content = rtrim($output->fetch(), "\n");
        if ($this->statusSection) {
            $this->statusSection->overwrite($content);
            return;
        }
        $this->output->writeln($content);
    }

    private function receiveFrame($client): ?Frame
    {
        $header = fread($client, 12);
        if ($header === false || strlen($header) !== 12) {
            return null;
        }

        $parts = Frame::readHeader($header);
        $length = ($parts[1] * 4) + $parts[2];
        $payload = '';

        while ($length > 0) {
            $chunk = fread($client, $length);
            if ($chunk === false || $chunk === '') {
                return null;
            }
            $payload .= $chunk;
            $length -= strlen($chunk);
        }

        return Frame::initFrame($parts, $payload);
    }
}
