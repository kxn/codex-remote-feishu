# 本地自升级流程

> Type: `general`
> Updated: `2026-04-15`
> Summary: 说明 repo 构建产物触发本地自升级时的完整时序、内嵌 upgrade shim 的释放与启动方式、与 `/upgrade dev` 的边界，以及自动回滚规则。

## 1. 这份文档回答什么问题

这份文档只回答一件事：当前仓库里执行“本地编译一个新版本，然后让已安装实例升级到这个版本”时，整条链路到底是怎么完成的。

重点覆盖：

- `./upgrade-local.sh` 做了什么
- `codex-remote local-upgrade` 做了什么
- 真正负责切换 live binary 的 helper 是谁
- helper 从哪里来，释放到哪里，怎么启动
- 自动回滚在什么条件下触发
- 多实例 / workspace 绑定下，升级到底打到哪个实例

如果以后只是想解释“当前本地自升级怎么工作”，优先看这份文档，不必重新从代码追起。

## 2. 范围与边界

这份文档描述的是当前实现里的“repo 构建产物 -> 本地已安装实例”的自升级事务。

它不展开：

- WebSetup 里的普通安装流程
- GitHub Release 下载与安装
- `/upgrade dev` 通过固定 `dev-latest` manifest 拉取滚动开发构建的细节
- 飞书内 `/upgrade latest` 的 release 升级交互细节

但要注意，当前本地升级、`/upgrade latest` 的 release 升级、以及 `/upgrade dev` 的滚动开发构建升级，在底层都会复用同一个 upgrade-helper 事务模型，只是目标 binary 的来源不同。

## 3. 参与者与角色

### 3.1 `./upgrade-local.sh`

这是源码仓库 helper。它负责：

- 在工作区干净时先 `git pull --ff-only`
- 构建新的 `./bin/codex-remote`
- 解析当前 repo 绑定到哪个已安装实例
- 把构建产物复制到该实例的固定 local-upgrade artifact 路径
- 用刚构建出的 binary 调 `local-upgrade`

它本身不直接停服务，也不直接覆盖 live binary。

### 3.2 fixed local-upgrade artifact

这是“本地升级源文件”的固定投递点，默认位于：

```text
<stateDir>/local-upgrade/codex-remote
```

Windows 下文件名为 `codex-remote.exe`。

`./upgrade-local.sh` 会把 repo 构建产物先复制到这里，然后 `local-upgrade` 再从这里读取升级源。

### 3.3 `versionsRoot/<slot>/...`

这是升级目标 binary 的正式 staging 位置。`local-upgrade` 会把 artifact 导入到：

```text
<versionsRoot>/<slot>/codex-remote
```

其中 `slot` 默认按 binary fingerprint 自动派生为 `local-<12 hex>`，也可以显式传 `--slot`。

### 3.4 live installed binary

这是当前真正被 daemon/service 使用的稳定入口路径，也就是 install-state 里的：

- `CurrentBinaryPath`

helper 真正切换版本时，覆盖的是这条路径。

### 3.5 upgrade helper shim

真正执行“停旧服务 -> 切换 stable binary -> 拉起新服务 -> 观察健康 -> 必要时回滚”的，是一个独立 helper 进程。

这个 helper 不再复用主 `codex-remote` binary 本体，而是一个构建时内嵌在主程序里的 tiny shim。需要执行升级事务时，当前进程会把它释放到：

```text
<stateDir>/upgrade-helper/codex-remote-upgrade-shim-<timestamp>
```

同时会在旁边写入 sidecar：

```text
<stateDir>/upgrade-helper/codex-remote-upgrade-shim-<timestamp>.remote.json
```

sidecar 至少绑定当前事务对应的 `install-state.json`。这样即使当前服务被停止，shim 也能继续活着把后续事务跑完。

### 3.6 rollback bundle

升级前，会先把当前 live binary 和 live config 备份到：

```text
<stateDir>/upgrade-backups/<slot>/
```

里面至少包括：

- 当前 live binary 的副本
- `config.json` 等 live config 快照

升级失败时，helper 会用这份 bundle 做自动回滚。

## 4. 升级目标实例是怎么选出来的

`./upgrade-local.sh` 和 `codex-remote local-upgrade` 都遵循当前的 workspace 绑定解析规则。

优先级是：

1. 显式传入的 `--instance` / `--base-dir` / `-state-path`
2. 当前 repo 下的 `.codex-remote/install-target.json`
3. 没有 workspace binding 时，再按 repo 祖先 / 平台默认目录去找可用实例

所以“本地升级哪个实例”并不是拍脑袋决定的，而是明确绑定到当前 repo 解析出的 install target。

repo 里常用的辅助解析入口是：

- `scripts/install/repo-install-target.sh`

如果以后要查某个 workspace 当前会打到哪个实例，也优先从这个绑定模型看，而不是猜 stable 或猜某个端口。

## 5. 从 `./upgrade-local.sh` 发起时的完整时序

### 5.1 第一步：同步并构建 repo 产物

`./upgrade-local.sh` 默认要求工作区干净；如果不干净，会直接拒绝继续，除非显式传 `--allow-dirty`。

通过检查后，它会：

1. `git pull --ff-only`
2. 准备 host 平台的内嵌 upgrade shim 资产
3. 构建 `./bin/codex-remote`

这一步产出的 `./bin/codex-remote` 的主要用途是：

- 作为本次升级的 source build
- 携带当前 host 平台的内嵌 upgrade shim 资产，供后续 `local-upgrade` 释放

### 5.2 第二步：解析 repo 绑定实例

脚本会调用 `scripts/install/repo-install-target.sh --format shell`，拿到：

- 目标实例 id
- `install-state.json` 路径
- log 路径
- admin URL
- fixed local-upgrade artifact 路径

这一步决定了本次升级打到哪个实例。

### 5.3 第三步：把构建产物投递到固定 artifact 路径

脚本把 `./bin/codex-remote` 复制到：

```text
<stateDir>/local-upgrade/codex-remote
```

这只是把“待升级 binary”投递进去，还没有切换 live binary。

### 5.4 第四步：执行 `local-upgrade`

脚本随后运行：

```bash
./bin/codex-remote local-upgrade -state-path <targetStatePath>
```

注意这里是“刚构建出的 repo binary”在执行 `local-upgrade` 子命令。

当前实现中，不再去解析“helper 来自哪个主 binary”。`local-upgrade` 会直接从当前执行体里释放内嵌的 upgrade shim，并为它写 sidecar。  
所以在 `./upgrade-local.sh` 这条路径里，repo build 输出和 helper 仍然相关，但关系已经变成“主 binary 携带 shim 资产”，而不是“主 binary 自己被复制成 helper”。

## 6. `local-upgrade` 在内部做了什么

`codex-remote local-upgrade` 本身不做 stop/start 切换。它的职责是“准备事务并拉起 helper”。

具体顺序如下：

1. 解析目标 `statePath`
2. 读取 install-state
3. 确认 fixed local-upgrade artifact 文件存在
4. 检查当前是否已有进行中的 `PendingUpgrade`
5. 把 artifact 导入到 `versionsRoot/<slot>/codex-remote`
6. 计算目标 binary identity，并准备 rollback candidate
7. 写回 install-state：
   - `RollbackCandidate`
   - `PendingUpgrade.phase=prepared`
   - 目标 source / slot / version / targetBinaryPath
8. 把内嵌 upgrade shim 释放到 state 目录下的 `upgrade-helper/`，并写 sidecar
9. 以独立进程方式启动：
   - 直接执行 shim，自身通过 sidecar 找到 `install-state.json`
10. 把 helper 的 transient unit 名称等信息再写回 `PendingUpgrade`

到这里为止，`local-upgrade` 这条命令就结束了。真正的切换动作在后面那个 helper 里。

## 7. helper 是怎么启动的

### 7.1 为什么必须单独起 helper

因为这是“自己升级自己”的事务。

如果还让当前 daemon 或当前 live binary 直接负责 stop/switch/start：

- 它在停旧服务时可能把自己也停掉
- 它在覆盖当前正在使用的 binary 时容易撞上文件占用问题
- 它无法在新版本启动失败后继续完成回滚

所以当前实现明确把 upgrade 事务交给独立 helper。

### 7.2 Linux `systemd_user` 模式

如果目标实例的 lifecycle manager 是 `systemd_user`，并且运行在 Linux，helper 会通过：

```bash
systemd-run --user --no-block --collect --quiet --service-type=exec ...
```

启动成一个 transient user unit。

这样即使旧的 daemon service 被 stop，helper 也不会跟着一起被 systemd 收掉。

### 7.3 其他模式

非 `systemd_user` 场景下，helper 走 detached process 启动。

无论哪种方式，stdout/stderr 都会追加写到目标实例的 daemon log 路径，便于排查 upgrade 事务。

## 8. helper 的切换与观测流程

helper shim 入口是一个独立 binary，本身不再接受 `upgrade-helper -state-path ...` 这种子命令参数，而是：

```bash
<stateDir>/upgrade-helper/codex-remote-upgrade-shim-<timestamp>
```

它会先读同路径旁边的 sidecar，再进入已经处于 `PendingUpgrade.phase=prepared` 的事务。

完整流程是：

1. 读取 install-state，并确认：
   - `PendingUpgrade` 存在
   - phase 是 `prepared`
   - rollback candidate 已存在
2. 把 phase 改成 `switching`
3. 停掉当前 daemon/service
   - 当前实现不会只看 `stop` 命令是否已发出
   - 只有在 helper 确认旧 daemon/service 已真正退出后，才会继续进入 binary 切换
4. 把 `PendingUpgrade.TargetBinaryPath` 复制覆盖到 `CurrentBinaryPath`
5. 把 phase 改成 `observing`
6. 用新的 live binary 重新启动 daemon/service
7. 轮询健康状态，直到成功或超时
8. 成功后把 install-state 更新为：
   - `CurrentVersion = TargetVersion`
   - `CurrentSlot = TargetSlot`
   - `LastKnownLatestVersion = TargetVersion`
   - `PendingUpgrade.phase = committed`

当前观测的健康检查点包括：

- `/healthz`
- `/api/admin/bootstrap-state`
- `/api/admin/runtime-status`
- `/v1/status`

其中除了 core health 外，还要求 gateway 恢复到可接受状态；当前实现给 gateway 额外一个短暂恢复窗口，而不是只看主进程端口活了就算升级成功。

## 9. 回滚是怎么做的

如果 helper 在以下任一步失败：

- stop 当前 daemon 失败
- 覆盖 live binary 失败
- 新 daemon 启动失败
- 健康检查超时或返回异常

就会进入自动回滚。

回滚顺序是：

1. 再次停止当前可能半起状态的 daemon
   - 这里同样走严格 stop gate；如果 helper 不能确认它已经停干净，会直接把 phase 标成 `failed`，不会继续做 rollback copy
2. 恢复升级前备份下来的 live config 快照
3. 把 rollback bundle 里的旧 binary 复制回 `CurrentBinaryPath`
4. 把 install-state 恢复到旧版本信息
5. 标记 `PendingUpgrade.phase = rolled_back`
6. 重新拉起回滚后的 daemon

如果连回滚后的 daemon 都起不来，phase 会进一步进入 `failed`。

也就是说，当前实现不是“升级失败后停在那里等人修”，而是“优先尝试自动恢复到升级前可工作的版本”。

## 10. 当前实现里最容易混淆的几个点

### 10.1 artifact 和 helper 不是一回事

`<stateDir>/local-upgrade/codex-remote` 是升级源 artifact。

`<stateDir>/upgrade-helper/...` 是真正执行升级事务的 helper shim 与 sidecar。

它们可能来自同一个 repo build，但不是同一路径，也不是同一个职责。

### 10.2 `upgrade-local.sh` 不会直接改 live binary

它只负责：

- 拉最新代码
- 构建 repo binary
- 投递 artifact
- 触发 `local-upgrade`

真正改 `CurrentBinaryPath` 的动作发生在 helper 中。

### 10.3 helper 不再来自“当前执行 `local-upgrade` 的 binary 副本”

当前实现里，主 binary 只负责携带并释放内嵌 shim 资产。  
是否更新 shim，不由普通业务发版直接驱动；只有 shim 本身或 sidecar schema 发生变化时，才需要重新准备对应平台的嵌入资产。

### 10.4 `systemd_user` 下 helper 不是原 service 的一部分

它会被放进独立 transient unit，而不是复用 `codex-remote.service` 本身。否则 stop 原 service 时，helper 也会被连带终止。

## 11. 常看路径

假设目标实例的 `install-state.json` 位于 `<stateDir>/install-state.json`，那这次事务常见路径大致是：

```text
repo build output:
./bin/codex-remote

local upgrade artifact:
<stateDir>/local-upgrade/codex-remote

target slot binary:
<versionsRoot>/<slot>/codex-remote

helper shim:
<stateDir>/upgrade-helper/codex-remote-upgrade-shim-<timestamp>

helper sidecar:
<stateDir>/upgrade-helper/codex-remote-upgrade-shim-<timestamp>.remote.json

rollback backup:
<stateDir>/upgrade-backups/<slot>/<current-binary-name>
<stateDir>/upgrade-backups/<slot>/config/

live installed binary:
<CurrentBinaryPath>
```

## 12. 代码锚点

当前实现的主要锚点在：

- `upgrade-local.sh`
- `internal/app/install/local_upgrade_entry.go`
- `internal/app/install/local_upgrade.go`
- `internal/app/install/upgrade_shim.go`
- `internal/app/install/upgrade_helper_launch.go`
- `internal/app/install/upgrade_helper.go`
- `internal/upgradeshim/`
- `internal/app/install/rollback_bundle.go`
- `internal/app/install/repo_target_info.go`
- `scripts/install/repo-install-target.sh`

如果未来实现变更，这份文档应与这些锚点一起同步更新；但在没有行为变更时，升级流程说明应优先维护在本文，而不是让后续排查再次退回到读代码。
