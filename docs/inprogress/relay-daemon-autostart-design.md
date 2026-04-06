# Relay Daemon Autostart Design

> Type: `inprogress`
> Updated: `2026-04-06`
> Summary: 迁移到 `docs/inprogress` 并统一文档元信息头，保留待实现的 relay 自动拉起设计。

## 1. 背景

当前 `relay-wrapper` 的行为是：

1. 启动 `codex.real`
2. 直接连接 `relayd`
3. 如果 `relayd` 没起来，wrapper 失败退出

这对开发和部署都不够友好。用户希望它更像 `adb`：

- wrapper 初次启动时，如果发现本机 relay 不可达，可以自动在后台拉起
- 如果发现本机 relay 版本和当前 wrapper 不一致，可以自动替换成当前版本
- 但这个“自举能力”只允许发生在**首次建立 relay 连接之前**
- 一旦 wrapper 已经成功连上过 relay，之后如果 relay 断了：
  - 连不上时只重连，不再尝试后台拉起
  - 连上但版本不对时直接退出

这样设计的核心目的有两个：

1. 首次使用和升级时尽量自动恢复到正确版本
2. 已进入稳定会话后，不要因为短暂断连或其他进程干扰，反复重启 relay，破坏正在运行的会话

## 2. 目标与非目标

### 2.1 目标

- wrapper 首次启动时自动确保本机存在一个**可连接且版本兼容**的 relay
- 多个 VS Code / 多个 wrapper 同时启动时，不发生竞争性重复启动
- 当旧 relay 版本残留时，允许新 wrapper 接管并替换
- 一旦 wrapper 已进入稳定连接，不再做“自动拉起 relay”的动作
- 对代理环境保持现有规则：
  - wrapper 自己访问 localhost 时不走代理
  - `codex.real` 需要恢复原始代理
  - daemon 是否走系统代理取决于 `FEISHU_USE_SYSTEM_PROXY`

### 2.2 非目标

- 不做完整的 daemon 进程管理器
- 不做跨机器的服务发现
- 不做高可用双 daemon
- 不允许 wrapper 在“已稳定连接后”自动杀掉当前 relay 并重新拉起

## 3. 现状与问题

当前代码中的关键现状：

- `relay-wrapper` 只会创建 `relayws.Client` 并无限重连
- `relayd` 只能通过 `install.sh start` 或手工 `codex-remote daemon` 启动
- `relay.agent.v1` 的 `welcome` 只有：
  - `protocol`
  - `serverTime`
- wrapper 无法知道对端 daemon 的：
  - 版本
  - 指纹
  - pid
- `install.sh` 的 pid 文件是脚本行为，daemon 自身并不会写自己的 runtime state

这意味着 wrapper 目前缺少三样关键能力：

1. 无法精确判定“连不上”是 daemon 没启动、端口被占、还是对端版本旧
2. 无法在不依赖 shell 脚本的情况下安全地启动/替换 daemon
3. 无法在多 wrapper 并发启动时做正确仲裁

## 4. 设计原则

### 4.1 首次自举，后续保守

- **首次尚未建立成功连接前**：允许 wrapper 负责把 relay 调整到正确状态
- **首次成功连接之后**：wrapper 只做重连，不再做自启动或替换

### 4.2 最新 wrapper 获胜

如果旧版本 daemon 仍在运行，而一个新版本 wrapper 启动：

- 新 wrapper 在初始自举阶段可以替换旧 daemon
- 旧 wrapper 在之后的重连中如果发现版本不匹配，应直接退出

这意味着升级时是“新版本接管，旧版本自然淘汰”的策略。

### 4.3 不杀未知进程

wrapper 只能终止它能明确识别为 `codex-remote daemon` 的进程。

如果端口上是未知服务，或识别信息不完整，则：

- 不自动 kill
- 直接报错退出

### 4.4 锁和身份都必须在程序内完成

不能把“谁是当前 daemon”“是否正在启动”“pid 是谁”依赖在 shell 脚本上。

需要把以下 runtime 事实沉到 Go 代码里：

- daemon 单实例锁
- daemon identity / pid 落盘
- wrapper 启动期的 manager 锁

## 5. 版本身份模型

只比较 `Version` 不够，因为开发环境里经常都是 `dev`。

因此需要区分两层身份：

### 5.1 人类可读版本

- `version`
- 例如：`1.0.0`、`1.2.3+abc123`、`dev`

### 5.2 机器可比较指纹

- `buildFingerprint`
- 用于判断 wrapper 和 daemon 是否来自**同一构建**
- 优先级高于 `version`

推荐比较规则：

1. 若双方都有 `buildFingerprint`，按 `buildFingerprint` 精确匹配
2. 否则退回 `version`
3. 两者都缺失时，视为“legacy / unknown”

其中需要区分两种情况：

- `legacy relay.agent.v1`
  - 握手成功，但 `welcome.server` 缺失
  - 如果本地 runtime 文件还能明确给出 pid，则允许在 bootstrap phase 替换
- `unknown listener`
  - 连的是别的服务，或 websocket/协议握手不成立
  - 不允许自动 kill

## 6. 协议扩展

当前 `relay.agent.v1` 需要补足服务端身份信息。

### 6.1 `hello`

保留现有 `hello.instance.version`，并新增：

- `hello.instance.buildFingerprint`
- `hello.instance.binaryPath` 可选，仅用于日志/诊断
- `hello.probe`
  - `true` 表示当前连接只做 relay 兼容性探测
  - daemon 必须只返回 `welcome`，不能把它注册成实例，也不能下发初始化 command

示意：

```json
{
  "type": "hello",
  "hello": {
    "protocol": "relay.agent.v1",
    "probe": true,
    "instance": {
      "instanceId": "inst-123",
      "version": "1.0.0",
      "buildFingerprint": "sha256:abcd..."
    }
  }
}
```

### 6.2 `welcome`

`welcome` 需要从“仅协议确认”扩展为“服务端身份确认”：

```json
{
  "type": "welcome",
  "welcome": {
    "protocol": "relay.agent.v1",
    "serverTime": "2026-04-05T10:00:00Z",
    "server": {
      "product": "codex-remote",
      "version": "1.0.0",
      "buildFingerprint": "sha256:abcd...",
      "pid": 12345,
      "startedAt": "2026-04-05T09:59:58Z"
    }
  }
}
```

补充握手顺序要求：

- daemon 在普通实例连接上必须先发送 `welcome`，再触发任何后续 command
- probe 连接只允许收到 `welcome`
- wrapper 的 probe 逻辑需要兼容旧 daemon 的历史行为，也就是在 `welcome` 前可能先收到 `command`

### 6.3 兼容性判定

wrapper 连接到 relay 后，只认下面三种结果：

1. `welcome` 正常，且 `buildFingerprint/version` 与本 wrapper 兼容
2. `welcome` 正常，但身份不兼容
3. 无法完成握手

补充：

- 若 `welcome` 缺少 `server` 身份字段，则视为 legacy relay
- legacy relay 只允许在 bootstrap phase、且能从本地 runtime 文件定位 pid 时被替换

不会再用“端口通了就当可用”这种宽松判定。

## 7. 本地运行时文件

建议在统一 runtime 目录下管理 daemon 运行时状态：

```text
~/.local/state/codex-remote/
  relay-manager.lock
  relayd.lock
  codex-remote-relayd.pid
  codex-remote-relayd.identity.json
```

说明：

- `relay-manager.lock`
  - wrapper 启动期短时持有
  - 用来串行化“检查/kill/start”决策
- `relayd.lock`
  - daemon 生命周期内持有
  - 用来保证单实例
- `codex-remote-relayd.pid`
  - daemon 自身写入，而不是 shell 脚本写入
- `codex-remote-relayd.identity.json`
  - daemon 自身写入
  - 至少包含：
    - `pid`
    - `version`
    - `buildFingerprint`
    - `binaryPath`
    - `startedAt`
    - `configPath`

## 8. 组件划分

建议增加一个明确的本地管理层，而不是把逻辑塞进 `relayws.Client` 里：

### 8.1 `internal/runtime/identity`

负责：

- 计算当前二进制的 `version`
- 计算当前二进制的 `buildFingerprint`
- 读取/写入 daemon identity file

### 8.2 `internal/runtime/lock`

负责：

- 跨平台文件锁
- 区分“短时 manager 锁”和“daemon 生命周期锁”

### 8.3 `internal/runtime/daemonmgr`

负责 wrapper 启动期的本地 relay 管理：

- probe 当前 relay
- 判定兼容/不兼容/不可达
- 在锁内做 kill/start/wait

### 8.4 `relayws.Client`

保持为“纯连接传输层”，但需要补两个能力：

- 对 `welcome` 的完整回调
- 能区分“首次成功连接”和“后续重连”

### 8.5 `internal/app/wrapper`

wrapper 自己负责：

- 首次启动前的 `EnsureRelayReady`
- 成功后进入 steady state
- 后续重连的严格策略

## 9. Wrapper 启动状态机

### 9.1 阶段划分

wrapper 的生命周期明确分成两段：

1. `bootstrap phase`
   - 还没有成功连上过 relay
   - 允许自启动/替换 daemon
2. `steady phase`
   - 已成功连上过 relay
   - 不再允许自启动 daemon

### 9.2 启动顺序

建议调整为：

1. 读取统一配置
2. 计算当前 wrapper 身份
3. 执行 `EnsureRelayReady`
4. 成功后再启动 `codex.real`
5. 启动正常的 `relayws.Client`

这样可以避免：

- `codex.real` 已经开始产生本地事件，但 relay 还没准备好
- wrapper 半工作半失败

### 9.3 `EnsureRelayReady` 逻辑

伪代码：

```text
probe relay
if compatible:
  success

acquire relay-manager.lock
re-probe relay
if compatible:
  success

if relay unreachable:
  start daemon from current wrapper binary
  wait until compatible
  success

if relay reachable but incompatible:
  verify peer is codex-remote daemon
  stop old daemon
  start daemon from current wrapper binary
  wait until compatible
  success

otherwise:
  fail
```

### 9.4 “连不上”定义

下列情况都算“unreachable”：

- TCP connect refused
- 连接超时
- websocket upgrade 失败
- 未收到合法 `welcome`

但“端口上有未知服务”不算可自动处理的 unreachable，而是：

- `unknown_listener`
- 不自动 kill
- wrapper 启动失败

## 10. Steady Phase 重连状态机

一旦 wrapper 已完成首次成功连接，状态切换为 `steady`。

此后规则固定：

### 10.1 断线后连不上

- 不拉起 daemon
- 不杀任何进程
- 只继续指数退避重连

原因：

- 这通常意味着用户主动停服务、端口暂时异常或机器状态变化
- 此时再自动拉服务，容易和用户的显式操作打架

### 10.2 断线后又连上，但版本不一致

- 立即把它视为 fatal
- wrapper 退出
- 同时结束 `codex.real`

原因：

- 这说明 daemon 已被另一版本接管
- 继续运行会让同一台机器上同时存在“旧 wrapper + 新 daemon”的组合
- 这类状态最容易产生协议漂移和 UI 状态错乱

### 10.3 断线后连上且版本一致

- 正常恢复
- 继续当前会话

## 11. 多个 VS Code 同时启动时的竞争处理

### 11.1 同版本并发启动

期望行为：

1. 多个 wrapper 同时发现 relay 不可达
2. 只有一个拿到 `relay-manager.lock`
3. 只有它会真正启动 daemon
4. 其他 wrapper 等锁释放后 re-probe
5. 都连到同一个 daemon

### 11.2 不同版本并发启动

期望行为：

1. 旧版本 wrapper 和新版本 wrapper 同时启动
2. 谁先拿到锁谁先启动自己的 daemon
3. 另一个在拿到锁后 re-probe
4. 若发现对方 daemon 版本不兼容，则按“新启动期允许替换”的规则处理
5. 最终只保留一个 daemon

这个策略的实际含义是：

- 最后接管成功的版本获胜
- 已经进入 steady 的旧 wrapper 会在后续重连时因为版本不匹配而退出

### 11.3 为什么不能只靠端口绑定

只靠端口绑定不够，因为仍会有：

- 两个 wrapper 同时判断“没启动”
- 两边几乎同时拉 daemon
- 其中一个 bind 失败
- 另一个虽然成功，但前者不清楚最终结果

因此必须有显式的 manager 锁。

## 12. Daemon 启停规则

### 12.1 启动

wrapper 启动 daemon 时：

- 使用当前 wrapper 对应的同版本 binary
- 以 `daemon` role 启动
- `stdin` 指向 `os.DevNull`
- `stdout/stderr` 重定向到统一 log
- 创建独立 session / detached process
- 传入 `CODEX_REMOTE_CONFIG=<统一 config.json>`

这里有一个必须满足的实现细节：

- daemon 必须真正脱离当前 tty 和父会话
- 不能仅仅依赖 shell 里的 `&`
- Unix 上至少需要新 session（例如 `setsid`）
- Windows 上需要 detached/new process group 语义

目标是保证：

- wrapper/VS Code 所在 tty 关闭时，daemon 不会一起被挂掉
- wrapper 退出后，daemon 仍能继续作为后台服务存活

### 12.2 关闭旧 daemon

只有在满足以下条件时才允许 kill：

1. 当前处于 wrapper 的 bootstrap phase
2. 已拿到 `relay-manager.lock`
3. 已确认对端是 `codex-remote daemon`
4. 已确认它和当前 wrapper 身份不兼容

关闭顺序：

1. `SIGTERM`
2. 等待短暂 grace period
3. 仍存活时强制 kill

### 12.3 Daemon 自身单实例

daemon 启动后必须：

- 抢占 `relayd.lock`
- 抢不到则立即退出，并输出“another daemon owns runtime lock”

这样就算 manager 逻辑有 bug，也不会真的跑出多个 daemon。

## 13. 代理环境规则

这个设计必须遵守现有规则，不能破坏：

### 13.1 wrapper 本体

- 启动后立即清理 proxy env
- 访问 localhost / relay 时不走代理

### 13.2 `codex.real`

- 由 wrapper 恢复原始 proxy env 后再启动

### 13.3 daemon

daemon 是否带代理启动，取决于配置：

- `FEISHU_USE_SYSTEM_PROXY=false`
  - wrapper 启动 daemon 时不传 proxy env
- `FEISHU_USE_SYSTEM_PROXY=true`
  - wrapper 启动 daemon 时恢复捕获到的原始 proxy env

否则会出现一个很隐蔽的问题：

- wrapper 自己清掉了代理
- 然后它又去拉 daemon
- daemon 继承的是“已清空的 env”
- 结果飞书长连接无法按预期走代理

## 14. 错误矩阵

### 14.1 首次启动

- relay 可达且兼容
  - 正常启动 wrapper
- relay 不可达，自动拉起成功
  - 正常启动 wrapper
- relay 可达但版本不兼容，替换成功
  - 正常启动 wrapper
- relay 可达但不是本产品
  - wrapper 失败退出
- relay 不可达，自动拉起失败
  - wrapper 失败退出

### 14.2 已稳定连接后

- relay 短暂断线后恢复且兼容
  - 继续运行
- relay 断线后一直连不上
  - 持续重连，不拉起
- relay 恢复但版本不兼容
  - wrapper 退出

## 15. 测试要求

虽然这一版文档不开始编码，但实现时测试必须覆盖下面几类：

### 15.1 单元测试

- 版本/指纹比较规则
- manager 锁竞争
- relay identity 文件读写
- daemon lifecycle lock
- 启动参数与代理 env 选择

### 15.2 传输层测试

- `welcome` 返回 server identity
- wrapper 能识别 incompatible welcome
- steady phase 重连时 mismatch 触发 fatal

### 15.3 进程管理测试

- 首次不可达时自动拉起 daemon
- 首次 mismatch 时自动替换 daemon
- steady phase 不再自动拉起
- 同版本并发启动只会起一个 daemon
- 不同版本并发启动最终只保留一个 daemon

### 15.4 harness / 集成测试

- wrapper A 启动旧 daemon，wrapper B 新版本接管
- A 在后续重连时退出
- wrapper 在 `FEISHU_USE_SYSTEM_PROXY=true/false` 两种配置下拉 daemon，env 符合预期

## 16. 建议实施顺序

建议按下面顺序落地，而不是一次性把所有逻辑堆到 wrapper 里：

1. 给 `relay.agent.v1` 补 server identity
2. 让 daemon 自己写 pid / identity，并持有生命周期锁
3. 引入 `daemonmgr`，先实现“首次不可达 -> 自启动”
4. 再实现“首次 mismatch -> 替换”
5. 最后接 steady phase 的严格重连策略

## 17. 最终语义总结

最终希望实现的用户可感知行为是：

- 第一次打开 VS Code 时，不必手动先起 relay
- 如果机器上残留的是旧 relay，新 wrapper 会自动替换成当前版本
- 一旦当前 wrapper 已稳定运行，后续只会重连，不会擅自再起或再替换 relay
- 如果后来发现 relay 已被别的版本接管，当前 wrapper 直接退出，让状态保持明确

这套规则本质上是：

- **启动期主动修复**
- **运行期严格保守**
- **并发时通过锁确保唯一决策者**
