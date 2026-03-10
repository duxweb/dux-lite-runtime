<?php

declare(strict_types=1);

namespace Core\Runtime;

use Symfony\Contracts\EventDispatcher\Event;

class WsEvent extends Event
{
    private bool $allowed = true;
    private array $response = [];

    public function __construct(
        private readonly string $name,
        private array $payload = []
    ) {
    }

    public function name(): string
    {
        return $this->name;
    }

    public function payload(): array
    {
        return $this->payload;
    }

    public function allow(bool $allowed = true): void
    {
        $this->allowed = $allowed;
    }

    public function deny(): void
    {
        $this->allowed = false;
    }

    public function allowed(): bool
    {
        return $this->allowed;
    }

    public function setResponse(array $response): void
    {
        $this->response = $response;
    }

    public function response(): array
    {
        return $this->response;
    }
}
