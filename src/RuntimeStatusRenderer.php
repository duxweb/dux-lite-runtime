<?php

declare(strict_types=1);

namespace Core\Runtime;

use Symfony\Component\Console\Helper\Table;
use Symfony\Component\Console\Output\OutputInterface;

class RuntimeStatusRenderer
{
    public static function render(OutputInterface $output, array $status): void
    {
        $overview = new Table($output);
        $overview->setHeaders(['Item', 'Value'])->setRows($status['overview'] ?? []);
        $overview->render();

        $queue = $status['queue'] ?? [];
        if ($queue) {
            $output->writeln('');
            $table = new Table($output);
            $table->setHeaders(['Dispatcher', 'Num', 'Type', 'Driver', 'Weights', 'Pending', 'Running'])->setRows($queue);
            $table->render();
        }

        $scheduler = $status['scheduler'] ?? [];
        if ($scheduler) {
            $output->writeln('');
            $table = new Table($output);
            $table->setHeaders(['Scheduler', 'Cron', 'Callback', 'Desc'])->setRows($scheduler);
            $table->render();
        }

        $websocket = $status['websocket'] ?? [];
        if ($websocket) {
            $output->writeln('');
            $table = new Table($output);
            $table->setHeaders(['WebSocket', 'Value'])->setRows($websocket);
            $table->render();
        }
    }
}
