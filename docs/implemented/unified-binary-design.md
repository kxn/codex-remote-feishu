# 单一二进制设计

> Type: `implemented`
> Updated: `2026-08-11`
> Summary: 同步统一主 binary 的当前落地事实，并明确旧 `cmd/*` 入口仍作为兼容薄封装保留。

## 1. 文档定位

本文记录单一二进制方案的当前实现边界和历史设计依据。

当前事实以代码为准：

- [cmd/codex-remote/main.go](../../cmd/codex-remote/main.go)
- [internal/app/launcher/role.go](../../internal/app/launcher/role.go)
- [internal/app/launcher/launcher.go](../../internal/app/launcher/launcher.go)
- [internal/app/launcher/launcher_test.go](../../internal/app/launcher/launcher_test.go)

发布与安装入口的当前长期规则见：

- [install-deploy-design.md](../general/install-deploy-design.md)

## 2. 当前事实

当前仓库已经有统一主入口：

- `cmd/codex-remote`

主入口只负责把 `os.Args[1:]`、stdio、版本和分支信息传给 `internal/app/launcher`。

旧入口仍存在，但都是兼容薄封装：

- `cmd/relayd`
  - 固定把 argv 改成 `daemon`
  - 再进入同一套 launcher
- `cmd/relay-wrapper`
  - 在原 argv 前追加 `wrapper`
  - 再进入同一套 launcher
- `cmd/relay-install`
  - 在原 argv 前追加 `install`
  - 再进入同一套 launcher

因此不能再把当前状态描述成“只有三个入口、尚未新增 `cmd/codex-remote`”。更准确的说法是：

- 统一主 binary 已落地。
- 旧 `cmd/*` 入口仍保留，用于兼容旧脚本、本地习惯或过渡路径。
- 旧入口不再拥有独立业务逻辑。

## 3. 当前 launcher role

`internal/app/launcher` 当前识别这些顶层 role：

- `daemon`
- `install`
- `packaged-install`
- `packaged-install-probe`
- `local-upgrade`
- `service`
- `upgrade-helper`
- `wrapper`
- `version`
- `help`

无参数当前默认进入 `daemon` role。

wrapper role 的自动识别不依赖 basename，而依赖 app-server 参数：

- `codex-remote app-server ...` -> wrapper
- `codex-remote claude-app-server ...` -> wrapper
- `codex-remote wrapper app-server ...` -> wrapper
- `codex-remote wrapper claude-app-server ...` -> wrapper

launcher 还允许 Codex 根参数出现在 `app-server` 前，例如：

- `codex-remote -c features.code_mode_host=true app-server ...`
- `codex-remote --config=features.code_mode_host=true app-server ...`
- `codex-remote -C /tmp/work app-server ...`

未知顶层命令不会兜底进入 wrapper。比如：

- `codex-remote resume --thread abc` -> usage / non-zero
- `codex-remote wrapper resume --thread abc` -> usage / non-zero

## 4. 当前 role 边界

统一二进制不等于把三套运行时混在一起启动。

当前实现仍保留 role entry 边界：

- daemon role
  - 进入 `internal/app/daemon`
  - 负责 gateway、daemon service、admin/status API
- wrapper role
  - 进入 `internal/app/wrapper`
  - 负责 app-server 协议代理、环境捕获与子进程启动
- install role
  - 进入 `internal/app/install`
  - 负责安装、packaged install、service、local upgrade 等 installer/runtime 操作

launcher 只做：

- role 判定
- stdio / version / branch 传递
- 顶层 context 与 signal 处理
- role runner 分发

launcher 不承载 Feishu、app-server 翻译、安装向导或业务配置解析。

## 5. 安装与发布边界

当前 release 主产物以 `codex-remote` / `codex-remote.exe` 为稳定入口。

release 包、在线安装脚本、packaged installer 的当前规则不在本文重复维护；以 [install-deploy-design.md](../general/install-deploy-design.md) 为准。

需要特别避免的分叉描述：

- 不要再写“release 仍发布三份独立 binary”作为当前事实。
- 不要把 `setup.sh` / `setup.ps1` 写成 release 包里的最终用户产品入口。
- 不要把旧 `cmd/relayd`、`cmd/relay-wrapper`、`cmd/relay-install` 写成已经删除；它们仍是兼容薄封装。

## 6. 历史背景

单一二进制设计最初要解决的问题是：

- 降低 release 产物复杂度。
- 降低安装器和脚本对多个二进制文件名的耦合。
- 保留 wrapper / daemon / installer 三个运行边界。
- 让后续新增 role 或调试入口时继续沿同一套 launcher 机制扩展。

设计期的核心判断仍然成立：

- wrapper 必须只在 app-server 模式下启动。
- role 识别应优先看参数，不依赖 basename。
- launcher 不应把未知 CLI 调用兜底送进 wrapper。
- 业务逻辑继续放在 `internal/app/**`、`internal/core/**`、`internal/adapter/**`。

但当时的“新增 launcher / 新增 `cmd/codex-remote` / 三个旧入口改薄 shim”已经不再是未完成计划，而是当前已落地事实。

## 7. 当前测试锚点

launcher 行为由 [internal/app/launcher/launcher_test.go](../../internal/app/launcher/launcher_test.go) 固定。

重点覆盖：

- app-server 自动进入 wrapper
- explicit `wrapper app-server` 进入 wrapper
- Codex 根参数在 app-server 前仍能进入 wrapper
- `daemon` / `install` / `packaged-install` / `local-upgrade` / `service` role 分发
- 空参数默认 daemon
- `resume` 等未知命令不会误进 wrapper

如果后续新增 role，应先补 launcher 检测测试，再改生产分发。
