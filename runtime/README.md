# Dux Runtime

这个目录用于承载 Dux Lite 的 Go Runtime Service 源码。

当前已落地的 V1 Go 侧能力：

- 可编译的 runtime 入口
- 队列轮询 loop
- 调度轮询 loop
- 基于 Unix socket 的 PHP master 控制面 client
- 基于 `DUX_RUNTIME_PHP_WORKER_COMMAND` 的 PHP worker 子进程池
- 可选 realtime HTTP 入口（`/healthz`、`/auth`）

当前仍未完成的部分：

- Go <-> PHP 的 Goridge 协议替换
- 真正的 WebSocket gateway

当前运行前至少需要设置：

- `DUX_RUNTIME_CONTROL_SOCKET`
- `DUX_RUNTIME_PHP_WORKER_COMMAND`（若项目根目录存在 `./dux`，可自动推断）
- 当前平台的 Go runtime 二进制

例如：

```bash
export DUX_RUNTIME_CONTROL_SOCKET=/tmp/dux-lite-runtime.sock
export DUX_RUNTIME_PHP_WORKER_COMMAND='php dux runtime --worker'
```

构建二进制：

```bash
./runtime/build.sh
```

默认会生成：

- `runtime/bin/dux-runtime-darwin-amd64`
- `runtime/bin/dux-runtime-darwin-arm64`
- `runtime/bin/dux-runtime-linux-amd64`
- `runtime/bin/dux-runtime-linux-arm64`
- `runtime/bin/dux-runtime-windows-amd64.exe`
- `runtime/bin/dux-runtime-windows-arm64.exe`

这些二进制属于 `dux-runtime` 扩展包本身，默认放在 `dux-runtime/runtime/bin`。

`php dux runtime` 会优先按当前平台自动查找当前框架目录里的这些二进制，不再回落到 `go run`。
