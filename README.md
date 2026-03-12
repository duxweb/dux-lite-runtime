# Dux Lite Runtime

`duxweb/dux-lite-runtime` 是 DuxLite 的运行时扩展包。

它把以下能力收口到一个扩展里：

- Go runtime 二进制
- PHP master 控制进程
- 共享 PHP worker pool
- 队列调度与执行
- 计划任务调度与执行
- WebSocket 网关
- PHP <-> Go Goridge 通讯桥接

当前状态适合发布为测试版或 beta 版，适合先在真实项目中灰度使用。

## Features

- 独立 `runtime` 命令启动运行时
- `runtime --worker` 作为内部 PHP worker 入口
- `runtime:status` 输出当前运行时状态
- Go 侧共享 PHP worker pool
- worker 自动扩容、缩容、回收、重建
- 队列按 PHP `queue.toml` 的 dispatcher 配置执行
- 计划任务支持 5 位 cron 和运行时 6 位 cron
- `/ws` WebSocket 服务
- `/healthz` 健康检查
- `/metrics` 运行时指标
- PHP -> Go 网关推送
- Go -> PHP 事件/鉴权/消息桥接

## Requirements

- PHP 8.2+
- DuxLite `^2.0.13`
- `spiral/goridge`
- Go 环境
  仅在构建二进制时需要，业务使用时不需要 `go run`

按队列后端选择安装：

- Redis 队列需要 `ext-redis` 和 `symfony/redis-messenger`
- AMQP 队列需要 `ext-amqp` 和 `symfony/amqp-messenger`

## Install

```bash
composer require duxweb/dux-lite-runtime
```

如果是本地开发插件，安装后执行一次：

```bash
php dux plugin:refresh
```

## Build Binary

构建当前仓库内置的运行时二进制：

```bash
./runtime/build.sh
```

仓库发布时建议直接提交这些预编译二进制，这样 PHP 用户安装扩展后无需自行安装 Go 或手动构建。

默认会构建：

- macOS amd64
- macOS arm64
- Linux amd64
- Linux arm64
- Windows amd64
- Windows arm64

输出目录：

```text
runtime/bin/
```

业务项目运行时优先使用已编译好的二进制，不依赖本机 `go` 命令。

## Runtime Config

如需覆盖默认值，可在业务项目 `config/use.toml` 或 `config/use.dev.toml` 中增加。

以下参数均为默认参数，非必填：

```toml
[runtime]
# Go runtime 二进制命令，留空时自动按当前平台查找内置二进制
go_command = ""

# Go realtime 服务端口
port = 9504

# Go -> PHP master 控制 endpoint
control_socket = "data/runtime/master.sock"

# PHP -> Go gateway 控制 endpoint
gateway_socket = "data/runtime/gateway.sock"

# runtime master 进程 pid 文件
pid_file = "data/runtime/master.pid"

# PHP worker 启动命令
worker_command = "php dux runtime --worker"

# 单个 PHP worker 最多处理多少个任务后回收重建
worker_max_jobs = 1000

# 超过最小池的空闲 worker 保留秒数
worker_idle_ttl = 300

# 启动时最小 PHP worker 数
min_workers = 4

# 最大 PHP worker 数，0 表示不限制
max_workers = 0

# 没有空闲 worker 时每次扩容数量
scale_up_step = 1

# 单任务超时秒数，超时后当前 worker 会被销毁重建
task_timeout = 30

# 队列轮询间隔
queue_poll_interval = "1s"

# 每次从 PHP 拉取的队列任务上限
queue_pull_limit = 8

# 队列 dispatcher 配置刷新间隔
queue_config_refresh = "10s"

# 计划任务轮询间隔
schedule_poll_interval = "1s"

# 每次从 PHP 拉取的计划任务上限
schedule_pull_limit = 8

# runtime --watch 状态刷新间隔
status_interval = 10

# 是否启用扩展内置 WS fallback listener
ws_fallback = true

# 自定义 WS 鉴权回调，留空时走事件监听
ws_auth_callback = ""

# 是否启用 PHP 文件型队列统计，默认建议关闭
queue_metrics = false
```

Windows 说明：

- Windows 默认不会使用 Unix socket
- `control_socket` 和 `gateway_socket` 会自动退化为本地 TCP endpoint
- 默认使用 `tcp://127.0.0.1:0` 随机端口模式
- runtime 启动后会把实际地址写入 `data/runtime/control.endpoint` 和 `data/runtime/gateway.endpoint`

## Commands

### `runtime`

启动 PHP master 和 Go runtime：

```bash
php dux runtime
```

常用参数：

```bash
php dux runtime --port=9504
php dux runtime --watch
php dux runtime --no-status
php dux runtime --without-go
php dux runtime --check
```

### `runtime --worker`

内部 PHP worker 入口，一般不手动执行：

```bash
php dux runtime --worker
```

### `runtime:status`

查看当前运行时快照：

```bash
php dux runtime:status
```

## Queue Runtime Model

当前队列模型如下：

1. PHP 负责入队
2. PHP 输出 dispatcher 配置
3. Go 周期性向 PHP 拉取 dispatcher 列表
4. Go 根据 dispatcher 的并发数做调度
5. Go 把任务分发到共享 PHP worker pool
6. PHP worker 执行业务任务
7. Go 将 ack / fail 回传给 PHP

特点：

- 共用一个 PHP worker pool
- 不为每个 dispatcher 单独维护一组 PHP worker
- dispatcher 并发由 Go 侧调度控制
- worker 数量由共享池动态伸缩控制

### Dynamic Worker Pool

共享池行为：

- 启动时先拉起 `min_workers`
- 没有空闲 worker 时按 `scale_up_step` 扩容
- 超过 `worker_idle_ttl` 的空闲 worker 会缩回
- 但不会低于 `min_workers`
- 达到 `worker_max_jobs` 的 worker 会回收重建
- 执行异常或超时的 worker 会立刻销毁并重建

说明：

- `min_workers` 不会因为空闲被缩掉
- 但最小池中的具体进程也会因 `max_jobs` 或异常被重建

## Scheduler Runtime Model

计划任务模型如下：

1. PHP 负责生成计划任务列表
2. Go 按轮询周期向 PHP 拉取到期任务
3. Go 将任务投递给共享 PHP worker pool
4. PHP worker 执行计划任务
5. Go 将执行结果回传给 PHP

cron 兼容策略：

- 5 位 cron：按分钟级兼容 PHP 现有写法
- 6 位 cron：仅 runtime 模式支持秒级调度

这意味着：

- PHP 原生调度仍保持分钟级
- Go runtime 额外提供秒级能力

## WebSocket

Go runtime 提供 WebSocket 服务：

- `ws://host:port/ws`

辅助接口：

- `http://host:port/healthz`
- `http://host:port/metrics`

PHP 与 Go 的职责：

- Go 负责连接、topic、广播、在线状态
- PHP 负责鉴权、订阅授权、发布授权、业务消息处理

桥接方式：

- Go -> PHP：通过 Goridge 控制协议调用 `Ws.*`
- PHP -> Go：通过 `GatewayService`

PHP 侧已封装：

- `publish`
- `pushClient`
- `kick`
- `clients`
- `topics`

## Built-in WS Fallback

扩展包可直接注册 fallback listener：

- `runtime.ws.auth`
- `runtime.ws.subscribe`
- `runtime.ws.publish`
- `runtime.ws.online`
- `runtime.ws.offline`
- `runtime.ws.ping`
- `runtime.ws.message`

启用：

```toml
[runtime]
ws_fallback = true
```

适合：

- 本地调试
- 默认兜底行为
- 最小可运行测试

业务项目可自行监听这些事件实现正式逻辑。

## Status And Ops

### Runtime Status

`runtime:status` 会显示：

- socket
- gateway socket
- port
- WS / health / metrics URL
- Go command
- worker command
- queue dispatcher 数量
- scheduler 数量
- min / max / current worker pool
- scaled up / scaled down / recycled
- WebSocket 在线与 topic 统计

### Health

```text
GET /healthz
```

返回服务是否存活。

### Metrics

```text
GET /metrics
```

返回 JSON 结构的运行时指标，便于后台状态页或外部运维系统接入。

## Queue Metrics Files

DuxLite 核心里还有一个 PHP 文件型队列统计：

```text
data/queue/metrics/{run_id}/{worker}.json
```

它是 PHP 队列统计写入的，不是 Go 直接写的。

如果你使用 runtime 模式，建议关闭：

```toml
[runtime]
queue_metrics = false
```

关闭后不会继续写这些本地统计文件。

## Publish Recommendation

当前推荐发布方式：

- Composer 包版本标记为 `0.x`
- README 明确标记为 beta
- 先在真实业务中灰度
- 二进制随扩展仓库一起发布

目前已适合测试版发布，不建议直接定义为 `1.0 stable`。

## Test Result

2026-03-10 在 `/Volumes/Web/dux-ai` 做过一轮真实压测。

测试条件：

- runtime port: `9504`
- queue workers: `main(10) + minor(10)`
- scheduler pull limit: `8`
- queue backend: `redis`
- batch: `stress-500`

命令：

```bash
php dux queue:stress --worker=main --priority=medium --count=500 --batch=stress-500
```

结果：

- dispatched: `500`
- executed: `500`
- failed: `0`
- duplicate job id: `0`
- elapsed: about `78s`
- throughput: about `6.41 tasks/s`
- worker recycled: `0`
- worker broken: `0`
- runtime stop residual process: `0`

另外，Go 侧测试已通过：

```bash
go test ./...
```

其中包含共享 worker pool 的扩容、缩容、最大值约束测试。

## Known Limits

- 目前更适合作为 beta 使用
- 高强度长时间 soak test 还需要继续补
- WebSocket 大规模广播场景仍建议继续压测
- `runtime:status` 的实时池状态依赖 realtime `/metrics`
- 纯 PHP 队列和 runtime 模式目前仍共存，部分统计逻辑还保留兼容代码

## License

MIT
