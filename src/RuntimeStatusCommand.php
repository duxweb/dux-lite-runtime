<?php

declare(strict_types=1);

namespace Core\Runtime;

use Core\Command\Attribute\Command;
use Symfony\Component\Console\Command\Command as BaseCommand;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\OutputInterface;

#[Command]
class RuntimeStatusCommand extends BaseCommand
{
    protected static $defaultName = 'runtime:status';
    protected static $defaultDescription = 'Show runtime status snapshot';

    protected function configure(): void
    {
        $this->setName('runtime:status')->setDescription('Show runtime status snapshot');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        unset($input);

        $status = (new RuntimeStatus(
            RuntimeConfig::socketPath(),
            RuntimeConfig::gatewaySocketPath(),
            RuntimeConfig::realtimePort(),
            RuntimeConfig::resolvedGoCommand(),
            RuntimeConfig::workerCommand()
        ))->snapshot();

        RuntimeStatusRenderer::render($output, $status);

        return BaseCommand::SUCCESS;
    }

}
