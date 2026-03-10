<?php

declare(strict_types=1);

namespace Core\Runtime;

use Core\Bootstrap;
use Core\Plugin\PluginProvider;

class RuntimeServiceProvider implements PluginProvider
{
    public static function getCommands(): array
    {
        return [
            RuntimeCommand::class,
        ];
    }

    public static function register(Bootstrap $bootstrap): void
    {
    }

    public static function boot(Bootstrap $bootstrap): void
    {
        if (isset($bootstrap->command)) {
            foreach (self::getCommands() as $command) {
                $bootstrap->command->add(new $command());
            }
        }
    }

    public static function apps(): array
    {
        return [];
    }
}
