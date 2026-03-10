<?php

declare(strict_types=1);

namespace Core\Runtime;

class WsFallbackListener
{
    public function auth(WsEvent $event): void
    {
        if (!self::enabled()) {
            return;
        }

        $payload = $event->payload();
        $app = (string)($payload['app'] ?? 'runtime');
        $token = (string)($payload['token'] ?? 'guest');

        $event->setResponse([
            'client_id' => $app . ':' . $token,
            'client_type' => 'test',
            'allow_subscribe' => ['topics' => ['system.*']],
            'allow_publish' => ['topics' => ['system.*']],
            'meta' => [
                'fallback' => true,
            ],
        ]);
    }

    public function subscribe(WsEvent $event): void
    {
        if (!self::enabled()) {
            return;
        }
        $event->allow();
    }

    public function publish(WsEvent $event): void
    {
        if (!self::enabled()) {
            return;
        }
        $event->allow();
    }

    public function online(WsEvent $event): void
    {
        if (!self::enabled()) {
            return;
        }
        $event->setResponse(['status' => 'online']);
    }

    public function offline(WsEvent $event): void
    {
        if (!self::enabled()) {
            return;
        }
        $event->setResponse(['status' => 'offline']);
    }

    public function ping(WsEvent $event): void
    {
        if (!self::enabled()) {
            return;
        }
        $event->setResponse(['status' => 'pong']);
    }

    public function message(WsEvent $event): void
    {
        if (!self::enabled()) {
            return;
        }
        $event->allow();
    }

    private static function enabled(): bool
    {
        return RuntimeConfig::wsFallback();
    }
}
