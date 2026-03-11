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
            ->addOption('port', 'p', InputOption::VALUE_OPTIONAL, 'Realtime listen port', (string)RuntimeConfig::realtimePort())
            ->addOption('socket', null, InputOption::VALUE_OPTIONAL, 'Control endpoint', RuntimeConfig::socketPath())
            ->addOption('gateway-socket', null, InputOption::VALUE_OPTIONAL, 'Gateway admin endpoint', RuntimeConfig::gatewaySocketPath())
            ->addOption('go-command', null, InputOption::VALUE_OPTIONAL, 'Go runtime command', $this->inferGoCommand())
            ->addOption('without-go', null, InputOption::VALUE_NONE, 'Start php master only without Go runtime')
            ->addOption('check', null, InputOption::VALUE_NONE, 'Check runtime configuration only')
            ->addOption('watch', null, InputOption::VALUE_NONE, 'Watch runtime status in terminal')
            ->addOption('status-interval', null, InputOption::VALUE_OPTIONAL, 'Runtime status refresh interval seconds', (string)RuntimeConfig::watchStatusInterval())
            ->addOption('no-status', null, InputOption::VALUE_NONE, 'Disable runtime status table')
            ->addOption('max-jobs', null, InputOption::VALUE_OPTIONAL, 'Worker max jobs', RuntimeConfig::workerMaxJobs());
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        if ($input->getOption('worker')) {
            return (new Worker())->run((int)$input->getOption('max-jobs'));
        }

        $io = new SymfonyStyle($input, $output);
        $port = (int)$input->getOption('port');
        $socket = $input->getOption('socket') ?: RuntimeConfig::socketPath();
        $gatewaySocket = $input->getOption('gateway-socket') ?: RuntimeConfig::gatewaySocketPath();
        $withoutGo = (bool)$input->getOption('without-go');
        $goCommand = $withoutGo ? '' : ($input->getOption('go-command') ?: '');
        $workerCommand = RuntimeConfig::workerCommand();
        $statusInterval = max(1, (int)$input->getOption('status-interval'));
        $showStatus = (bool)$input->getOption('watch') && !$input->getOption('no-status');

        if (!$withoutGo && !$goCommand) {
            $io->error([
                'Go runtime binary not found for current platform.',
                'Build binaries first: ./runtime/build.sh',
                'Or set runtime.go_command in use.toml manually.',
            ]);
            return BaseCommand::FAILURE;
        }

        App::banner([
            'Mode' => 'runtime-master',
            'Socket' => $socket,
            'Gateway' => $gatewaySocket,
            'Port' => (string)$port,
            'Go' => $goCommand,
            'Worker' => $workerCommand,
        ]);

        if ($input->getOption('check')) {
            $io->success('Runtime configuration is valid.');
            return BaseCommand::SUCCESS;
        }

        return (new Master(
            $socket,
            $gatewaySocket,
            $port,
            $goCommand,
            $workerCommand,
            $output,
            $showStatus,
            $statusInterval
        ))->run();
    }

    protected function inferGoCommand(): string
    {
        return RuntimeConfig::resolvedGoCommand();
    }

}
