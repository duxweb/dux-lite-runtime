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
            RuntimeStatusCommand::class,
        ];
    }

    public static function register(Bootstrap $bootstrap): void
    {
        \Core\App::di()->set(GatewayService::class, GatewayService::make());
        \Core\App::di()->set('runtime.gateway', \Core\App::di()->get(GatewayService::class));

        if (!RuntimeConfig::wsFallback()) {
            return;
        }

        \Core\App::event()->addListener('runtime.ws.auth', [new WsFallbackListener(), 'auth']);
        \Core\App::event()->addListener('runtime.ws.subscribe', [new WsFallbackListener(), 'subscribe']);
        \Core\App::event()->addListener('runtime.ws.publish', [new WsFallbackListener(), 'publish']);
        \Core\App::event()->addListener('runtime.ws.online', [new WsFallbackListener(), 'online']);
        \Core\App::event()->addListener('runtime.ws.offline', [new WsFallbackListener(), 'offline']);
        \Core\App::event()->addListener('runtime.ws.ping', [new WsFallbackListener(), 'ping']);
        \Core\App::event()->addListener('runtime.ws.message', [new WsFallbackListener(), 'message']);
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
