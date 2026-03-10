# Dux Runtime Extension

`duxweb/dux-runtime` 是 DuxLite 的 runtime 扩展包。

提供：

- `runtime` / `runtime --worker` PHP 命令
- Go runtime 二进制与构建脚本
- PHP master / Go runtime / PHP worker 执行链路

安装到业务项目后：

```bash
composer require duxweb/dux-runtime
```

构建 Go 二进制：

```bash
./vendor/duxweb/dux-runtime/runtime/build.sh
```

默认 worker 启动命令：

```bash
php dux runtime --worker
```
