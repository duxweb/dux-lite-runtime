<?php

declare(strict_types=1);

use Core\App;
use Core\Runtime\RuntimeCommand;
use Symfony\Component\Console\Tester\CommandTester;

function makeRuntimeCommand(?string $goCommand = null, ?array $platform = null, ?array $roots = null): RuntimeCommand
{
    return new class($goCommand, $platform, $roots) extends RuntimeCommand {
        public function __construct(
            private ?string $goCommand = null,
            private ?array $platform = null,
            private ?array $roots = null
        ) {
            parent::__construct();
        }

        protected function inferGoCommand(): string
        {
            if ($this->goCommand !== null) {
                return $this->goCommand;
            }
            return parent::inferGoCommand();
        }

        protected function currentPlatform(): array
        {
            if ($this->platform !== null) {
                return $this->platform;
            }
            return parent::currentPlatform();
        }

        protected function runtimeRoots(): array
        {
            if ($this->roots !== null) {
                return $this->roots;
            }
            return parent::runtimeRoots();
        }
    };
}

function runtimeTestCreateApp(string $basePath): void
{
    if (!is_dir($basePath)) {
        mkdir($basePath, 0777, true);
    }
    if (!is_file($basePath . '/.env')) {
        file_put_contents($basePath . '/.env', '');
    }
    App::create(basePath: $basePath, debug: true, timezone: 'UTC');
}

function runtimeTestCallPrivate(object $object, string $method, mixed ...$args): mixed
{
    $reflection = new ReflectionMethod($object, $method);
    return $reflection->invoke($object, ...$args);
}

it('infers runtime binary path from current platform build output', function (): void {
    $basePath = sys_get_temp_dir() . '/dux-runtime-' . uniqid();
    mkdir($basePath . '/runtime/bin', 0777, true);

    runtimeTestCreateApp($basePath);

    $command = makeRuntimeCommand(null, ['darwin', 'arm64', ''], [$basePath]);
    [$os, $arch, $ext] = runtimeTestCallPrivate($command, 'currentPlatform');
    $binary = "/runtime/bin/dux-runtime-{$os}-{$arch}{$ext}";
    file_put_contents($basePath . $binary, 'binary');

    runtimeTestCreateApp($basePath);
    $inferred = runtimeTestCallPrivate($command, 'inferGoCommand');

    expect($inferred)->toBe('.' . $binary);
});

it('fails when runtime binary is missing and without-go is not enabled', function (): void {
    $basePath = sys_get_temp_dir() . '/dux-runtime-' . uniqid();

    runtimeTestCreateApp($basePath);

    $tester = new CommandTester(makeRuntimeCommand('', ['darwin', 'arm64', ''], [$basePath]));
    $code = $tester->execute([]);

    expect($code)->toBe(1);
    expect($tester->getDisplay())->toContain('Go runtime binary not found for current platform.');
});

it('passes configuration check when without-go is enabled', function (): void {
    $basePath = sys_get_temp_dir() . '/dux-runtime-' . uniqid();
    mkdir($basePath . '/data/runtime', 0777, true);

    runtimeTestCreateApp($basePath);

    $tester = new CommandTester(new RuntimeCommand());
    $code = $tester->execute([
        '--without-go' => true,
        '--check' => true,
    ]);

    expect($code)->toBe(0);
    expect($tester->getDisplay())->toContain('Runtime configuration is valid.');
});
