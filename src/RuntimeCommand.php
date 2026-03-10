<?php

declare(strict_types=1);

namespace Core\Runtime;

use Core\App;
use Core\Command\Attribute\Command;
use Symfony\Component\Console\Command\Command as BaseCommand;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Output\OutputInterface;
use Symfony\Component\Console\Style\SymfonyStyle;

#[Command]
class RuntimeCommand extends BaseCommand
{
    protected static $defaultName = 'runtime';
    protected static $defaultDescription = 'Start Dux runtime master or internal worker mode';

    protected function configure(): void
    {
        $this
            ->setName('runtime')
            ->setDescription('Start Dux runtime master or internal worker mode')
            ->addOption('worker', null, InputOption::VALUE_NONE, 'Run internal worker mode')
            ->addOption('socket', null, InputOption::VALUE_OPTIONAL, 'Control socket path', (string)(getenv('DUX_RUNTIME_CONTROL_SOCKET') ?: data_path('runtime/master.sock')))
            ->addOption('go-command', null, InputOption::VALUE_OPTIONAL, 'Go runtime command', $this->inferGoCommand())
            ->addOption('without-go', null, InputOption::VALUE_NONE, 'Start php master only without Go runtime')
            ->addOption('check', null, InputOption::VALUE_NONE, 'Check runtime configuration only')
            ->addOption('max-jobs', null, InputOption::VALUE_OPTIONAL, 'Worker max jobs', (int)(getenv('DUX_RUNTIME_WORKER_MAX_JOBS') ?: 1000));
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        if ($input->getOption('worker')) {
            return (new Worker())->run((int)$input->getOption('max-jobs'));
        }

        $io = new SymfonyStyle($input, $output);
        $socket = $input->getOption('socket') ?: data_path('runtime/master.sock');
        $withoutGo = (bool)$input->getOption('without-go');
        $goCommand = $withoutGo ? '' : ($input->getOption('go-command') ?: '');
        $workerCommand = getenv('DUX_RUNTIME_PHP_WORKER_COMMAND') ?: 'php dux runtime --worker';

        if (!$withoutGo && !$goCommand) {
            $io->error([
                'Go runtime binary not found for current platform.',
                'Build binaries first: ./runtime/build.sh',
                'Or set DUX_RUNTIME_GO_COMMAND manually.',
            ]);
            return BaseCommand::FAILURE;
        }

        App::banner([
            'Mode' => 'runtime-master',
            'Socket' => $socket,
            'Go' => $goCommand,
            'Worker' => $workerCommand,
        ]);

        if ($input->getOption('check')) {
            $io->success('Runtime configuration is valid.');
            return BaseCommand::SUCCESS;
        }

        return (new Master($socket, $goCommand, $output))->run();
    }

    protected function inferGoCommand(): string
    {
        $command = getenv('DUX_RUNTIME_GO_COMMAND') ?: '';
        if ($command) {
            return $command;
        }

        [$os, $arch, $ext] = $this->currentPlatform();
        $candidates = [
            "/runtime/bin/dux-runtime{$ext}",
            "/runtime/bin/dux-runtime-{$os}-{$arch}{$ext}",
            "/runtime/bin/{$os}/{$arch}/dux-runtime{$ext}",
            "/runtime/dux-runtime{$ext}",
            "/runtime/dux-runtime-{$os}-{$arch}{$ext}",
        ];

        foreach ($this->runtimeRoots() as $root) {
            foreach ($candidates as $item) {
                $file = $root . $item;
                if (is_file($file)) {
                    return $this->relativeRuntimeCommand($root, $item);
                }
            }
        }

        return '';
    }

    protected function currentPlatform(): array
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

    protected function runtimeRoots(): array
    {
        $roots = [
            dirname(__DIR__),
            base_path(),
        ];

        $items = [];
        foreach ($roots as $root) {
            $root = rtrim((string)$root, DIRECTORY_SEPARATOR);
            if (!$root || isset($items[$root])) {
                continue;
            }
            $items[$root] = $root;
        }

        return array_values($items);
    }

    protected function relativeRuntimeCommand(string $root, string $path): string
    {
        $appRoot = rtrim(base_path(), DIRECTORY_SEPARATOR);
        $root = rtrim($root, DIRECTORY_SEPARATOR);
        if ($root === $appRoot) {
            return '.' . $path;
        }
        return $root . $path;
    }
}
