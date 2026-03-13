<?php

declare(strict_types=1);

namespace Core\Runtime;

use Spiral\Goridge\RPC\RPC;

class GatewayService
{
    private ?RPC $rpc = null;

    public function __construct(
        private readonly string $socketPath
    ) {
    }

    public static function make(): self
    {
        return new self(RuntimeConfig::gatewaySocketPath());
    }

    public function publish(string $topic, array $payload = [], array $meta = []): array
    {
        return (array)$this->rpc()->call('Gateway.Publish', $this->toRpcObject([
            'topic' => $topic,
            'payload' => $payload,
            'meta' => $meta,
        ]));
    }

    public function pushClient(string $clientId, array $payload = [], array $meta = []): array
    {
        $response = (array)$this->rpc()->call('Gateway.PushClient', $this->toRpcObject([
            'client_id' => $clientId,
            'payload' => $payload,
            'meta' => $meta,
        ]));

        if (($response['ok'] ?? false) !== true && !($response['error'] ?? '')) {
            $response['error'] = 'gateway push client failed';
        }

        return $response;
    }

    public function kick(string $clientId): array
    {
        return (array)$this->rpc()->call('Gateway.Kick', $this->toRpcObject([
            'client_id' => $clientId,
        ]));
    }

    public function clients(): array
    {
        return (array)$this->rpc()->call('Gateway.Clients', $this->toRpcObject([
            'scope' => 'all',
        ]));
    }

    public function topics(): array
    {
        return (array)$this->rpc()->call('Gateway.Topics', $this->toRpcObject([
            'scope' => 'all',
        ]));
    }

    private function rpc(): RPC
    {
        if ($this->rpc) {
            return $this->rpc;
        }
        return $this->rpc = RPC::create(RuntimeConfig::goridgeRpcUri($this->socketPath));
    }

    private function toRpcObject(array $payload): object
    {
        $object = new \stdClass();
        foreach ($payload as $key => $value) {
            $object->{$key} = $this->normalizeValue($value);
        }
        return $object;
    }

    private function normalizeValue(mixed $value): mixed
    {
        if (!is_array($value)) {
            return $value;
        }

        if (array_is_list($value)) {
            return array_map(fn (mixed $item) => $this->normalizeValue($item), $value);
        }

        return $this->toRpcObject($value);
    }
}
