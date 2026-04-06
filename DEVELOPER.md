# Developer Guide

## 项目定位

当前仓库维护的是公开可发布的 Go 版本：

```text
github.com/kxn/codex-remote-feishu
```

不要再把旧的 `fschannel` 命名、旧的 Node/Rust 结构、或本机绝对路径带回仓库。

## 二进制与职责

- `codex-remote`
  - `daemon`
    - relay websocket 服务端
    - orchestrator
    - Feishu gateway
    - 状态 API
  - `app-server` / wrapper role
    - 真实 `codex` 的包装器
    - native app-server -> canonical protocol 适配
  - `install`
    - 安装器
    - 写配置
    - patch VS Code 入口

兼容期内仓库仍保留：

- `cmd/relayd`
- `cmd/relay-wrapper`
- `cmd/relay-install`

但它们只是薄 shim，统一转发到 `codex-remote` 的 launcher。

## 目录

```text
cmd/
  codex-remote/
  relayd/
  relay-wrapper/
  relay-install/

deploy/
  docker/
  feishu/

docs/

internal/
  adapter/
  app/
  config/
  core/

testkit/
```

## 用户入口

- `setup.sh`
  - macOS / Linux 的交互安装入口
  - 无参数时默认 `-interactive`
- `setup.ps1`
  - Windows PowerShell 的交互安装入口
  - 无参数时默认 `-interactive`
- `install-release.sh`
  - 面向最终用户的在线安装入口
  - 负责下载最新 release 包并启动包内 `setup.sh`
- `install.sh`
  - Linux 开发 / 运维辅助脚本
  - 提供 `bootstrap/start/stop/status/logs/build`

`setup.*` 和 `install-release.sh` 是产品入口，`install.sh` 是仓库内运维辅助，不要混淆。

## 关键文档

- [文档索引](./docs/README.md)
- [架构说明](./docs/general/architecture.md)
- [协议说明](./docs/general/relay-protocol-spec.md)
- [飞书产品行为](./docs/general/feishu-product-design.md)
- [安装与部署](./docs/general/install-deploy-design.md)
- [测试策略](./docs/general/go-test-strategy.md)
- [单一二进制设计](./docs/inprogress/unified-binary-design.md)

如果改了这些内容，文档也要同步：

- wrapper 和 relayd 之间的 canonical protocol
- Feishu 输入输出逻辑
- 安装流程、默认路径、部署方式

## 常用命令

格式化：

```bash
gofmt -w $(find cmd internal testkit -name '*.go' | sort)
```

测试：

```bash
go test ./...
```

构建：

```bash
go build ./cmd/codex-remote
go build ./cmd/...
```

安装器 smoke test：

```bash
./setup.sh -integration editor_settings -feishu-app-id cli_xxx -feishu-app-secret secret_xxx
```

Linux 本地运行：

```bash
./install.sh bootstrap
./install.sh start
./install.sh status
./install.sh logs
./install.sh stop
```

Docker relayd：

```bash
cp deploy/docker/.env.example deploy/docker/.env
docker compose -f deploy/docker/compose.yml --env-file deploy/docker/.env up -d --build
```

release 安装器 smoke test：

```bash
bash scripts/check/smoke-install-release.sh
```

## 安装实现要点

- Linux 默认同时启用 `editor_settings` 和 `managed_shim`
- macOS / Windows 默认只启用 `editor_settings`
- `managed_shim` 会把扩展 bundle 中的 `codex` 重命名为 `codex.real`
- 然后把统一二进制 `codex-remote` 复制到原始 `codex` 路径
- `CODEX_REAL_BINARY` 会自动指向保留下来的 `codex.real`
- `install-release.sh` 必须兼容 `curl | bash`，因此交互 setup 需要显式从 `/dev/tty` 取 stdin

当前配置路径仍沿用统一布局：

- `<baseDir>/.config/codex-remote/config.json`
- `<baseDir>/.local/share/codex-remote`

这是当前 runtime config lookup 的约束，不要随意只改安装器而不改运行时读取逻辑。

## 实链路调试

先清理代理环境，避免本地回环链路被污染：

```bash
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy
```

再按顺序看：

1. 进程和端口

```bash
ps -ef | rg 'codex-remote|relayd|relay-wrapper' | rg -v rg
ss -ltnp | rg '9500|9501'
```

2. relayd 状态接口

```bash
curl --noproxy '*' -sf http://127.0.0.1:9501/v1/status | jq .
```

重点字段：

- `instances[*].Online`
- `instances[*].ObservedFocusedThreadID`
- `instances[*].ActiveThreadID`
- `instances[*].ActiveTurnID`
- `surfaces[*].AttachedInstanceID`
- `surfaces[*].SelectedThreadID`
- `surfaces[*].DispatchMode`
- `surfaces[*].ActiveQueueItemID`
- `surfaces[*].QueuedQueueItemIDs`

3. relayd 日志

重点前缀：

- `surface action:`
- `agent event:`
- `ui command:`
- `relay command ack:`
- `ui event:`
- `gateway apply failed:`

调状态机问题时，不要只看最终失败点，要把单条消息沿这些日志完整串起来。

## 代理环境规则

- 本地 wrapper <-> relayd 通信不应走代理
- 本地 `curl 127.0.0.1` / websocket 调试前应先 `unset` 代理
- `codex-remote` 的 wrapper role 拉起真实 `codex.real` 时，应恢复捕获到的代理环境
- `relayd` 是否使用系统代理，只由 `FEISHU_USE_SYSTEM_PROXY` 控制

## 开发约束

- 协议或状态机问题先看全链路，再改局部
- 不要在 wrapper 层吞掉上游协议信息来“修 UI”
- 产品可见性、队列和 thread 选择应由 orchestrator 决策
- helper/internal traffic 只能靠明确协议标识关联，不能靠猜测
- mock 必须贴近真实协议，不能用静态脚本假装通过
- 公开文档、README、模板文件里不要泄露本机路径

## 发布前自检

```bash
gofmt -w $(find cmd internal testkit -name '*.go' | sort)
go test ./...
bash scripts/check/no-local-paths.sh
bash scripts/check/no-legacy-names.sh
bash scripts/check/smoke-install-release.sh
```

## GitHub Actions

- `CI`
  - 检查公开文档是否泄漏本机路径
  - 检查旧项目名是否回流
  - 检查 `gofmt`
  - 跑 release 安装器 smoke test
  - 构建并运行 `go test ./...`
- `Release`
  - 支持显式指定版本或自动决定下一个语义化版本
  - 构建多平台产物
  - 创建 GitHub Release

本地可预演：

- `make check`
- `make release-artifacts VERSION=v0.1.0`
