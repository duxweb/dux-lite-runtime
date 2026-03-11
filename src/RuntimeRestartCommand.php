<?php

declare(strict_types=1);

namespace Core\Runtime;

use Core\Command\Attribute\Command;
use Symfony\Component\Console\Command\Command as BaseCommand;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\OutputInterface;
use Symfony\Component\Console\Style\SymfonyStyle;
use Symfony\Component\Process\Process;

#[Command]
class RuntimeRestartCommand extends BaseCommand
{
    protected static $defaultName = 'runtime:restart';
    protected static $defaultDescription = 'Restart runtime service in background';

    protected function configure(): void
    {
        $this->setName('runtime:restart')->setDescription('Restart runtime service in background');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $io = new SymfonyStyle($input, $output);

        $this->stopCurrentRuntime();
        usleep(500000);
        $this->startRuntime();

        $pid = $this->waitForPid();
        if (!$pid) {
            $io->warning('Runtime restart command sent, but new pid file was not detected yet.');
            return BaseCommand::SUCCESS;
        }

        $io->success('Runtime restarted. PID: ' . $pid);
        return BaseCommand::SUCCESS;
    }

    private function stopCurrentRuntime(): void
    {
        $pid = $this->currentPid();
        if (!$pid) {
            return;
        }

        if ($this->isWindows()) {
            Process::fromShellCommandline('taskkill /PID ' . $pid . ' /T /F', base_path())->run();
            return;
        }

        if (function_exists('posix_kill')) {
            @posix_kill($pid, SIGTERM);
            return;
        }

        Process::fromShellCommandline('kill ' . $pid, base_path())->run();
    }

    private function startRuntime(): void
    {
        if ($this->isWindows()) {
            Process::fromShellCommandline('cmd /C start "" /B php dux runtime --no-status', base_path())->run();
            return;
        }

        Process::fromShellCommandline('sh -c "php dux runtime --no-status > /dev/null 2>&1 &"', base_path())->run();
    }

    private function waitForPid(): int
    {
        $start = microtime(true);
        while ((microtime(true) - $start) < 10) {
            $pid = $this->currentPid();
            if ($pid > 0) {
                return $pid;
            }
            usleep(200000);
        }
        return 0;
    }

    private function currentPid(): int
    {
        $file = RuntimeConfig::pidFile();
        if (!is_file($file)) {
            return 0;
        }
        return (int)trim((string)file_get_contents($file));
    }

    private function isWindows(): bool
    {
        return \DIRECTORY_SEPARATOR === '\\';
    }
}
