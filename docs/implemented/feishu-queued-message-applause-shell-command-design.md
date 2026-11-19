# Feishu Queued Message APPLAUSE Shell Command Design

> Type: `implemented`
> Updated: `2026-08-16`
> Summary: 已落地排队消息 `APPLAUSE` reaction 到 Codex `thread/shellCommand` 的注入、互斥、恢复、未知结果和同机临时文件生命周期。

## 1. 文档定位

本文是 `APPLAUSE` reaction 入口的实现说明。它描述当前产品语义、queue item 状态、relay/app-server 命令边界、Feishu 投影和验证要求；当前实现以本文与 canonical 状态机文档为准。

相关现状文档：

- [Feishu reaction 与 queue 设计](../general/feishu-product-design.md)
- [relay 协议中的 `turn.steer`](../general/relay-protocol-spec.md)
- [Codex app-server 状态机审计](../inprogress/codex-app-server-state-machine-audit.md)
- [远程 surface 状态机](../general/remote-surface-state-machine.md)

## 2. 背景与目标

当前排队中的用户文本会在原消息上显示 `OneSecond`。用户给排队消息加 `ThumbsUp` 时，现有实现会把该 queue item 升级为 `turn.steer`，并在 wrapper 返回 `accepted=true` 后由机器人补一个 `ThumbsUp`。

新增 `APPLAUSE` 入口用于另一种动作：把排队消息的文本和绑定附件引用包装成 user shell command，通过 Codex app-server 的 `thread/shellCommand` 注入当前正在执行的 turn。它不是 `turn.steer` 的别名，也不替换现有点赞 steering。

目标流程：

```text
queued + OneSecond
    -- 用户给主文本加 APPLAUSE -->
shell-commanding（暂时离开普通队列）
    -- shell command 已被接收 -->
shell-commanded + 机器人补 APPLAUSE
    -- 内容进入当前 turn，当前 turn 继续执行 -->
其余 queue item 保持原顺序
```

目标包括：

- 保持 `ThumbsUp -> turn.steer` 的行为和现有成功/失败语义不变。
- 仅消费用户对 queued 主文本消息添加的 `APPLAUSE`。
- APPLAUSE payload 同时保留 queue item 的原始文本和绑定附件的结构化引用；附件只说明文件类型、runtime 路径和 MIME type，不把图片转换成额外的文字分析。
- `APPLAUSE` 成功后在原消息上由机器人补同一个 `APPLAUSE`，表示“已接收并启动”，不是表示最终 turn 已完成。
- 让 `ThumbsUp` 和 `APPLAUSE` 对同一个 queue item 互斥，避免同一条输入既 steer 又注入 shell command。
- 让冲突和不适用场景静默 no-op；让真正的启动失败可以恢复队列并给出可理解的反馈。

## 3. 非目标与适用范围

- 不修改 `ThumbsUp` steering，不把普通点赞改成 shell command。
- 不让 Feishu 用户通过消息内容直接执行任意 shell。队列文本只能写入同机临时文件，再由固定读取命令输出；不提供 inline/`echo` fallback。
- 带绑定图片的 queue item 也支持 shell command 注入；图片通过 payload 中的结构化附件引用保留，不在 shell command 中伪造视觉分析或把图片静默丢失。
- 不在 v1 为 Claude 或 OpenCode 伪造 `thread/shellCommand` 能力。没有明确声明该能力的 backend 必须 fail closed。
- 不把 `APPLAUSE` 作为 turn 完成 reaction。它只表示 shell command 已被接收/开始处理。
- 不接入 reaction deleted 事件，也不依赖读取整条消息的 reaction 列表作为状态机事实。

## 4. 上游协议事实与安全边界

当前 Codex app-server 的 `thread/shellCommand` 参数至少包含：

```json
{
  "threadId": "thread-id",
  "command": "printf '%s\\n' 'quoted text'"
}
```

shell command 读取的 payload 文件使用稳定的结构化文本格式，示例：

```text
<queued_input_bundle_v1>
{
  "schema": "queued_input_bundle.v1",
  "text": "请结合图片继续处理",
  "attachments": [
    {
      "ref": "att-1",
      "type": "image",
      "path": "/runtime-owned/image.png",
      "mime_type": "image/png"
    }
  ]
}
</queued_input_bundle_v1>
```

payload 约束如下：

- `text` 是 queue item 的原始文本，按 JSON 规则转义，不重新拼接成 shell 语句。
- `attachments` 只包含 runtime 已经生成并校验过的本地文件引用；`ref` 只在本 payload 内稳定，`type` 说明文件类型，`path` 是 runtime 路径，`mime_type` 是探测或入站记录的 MIME type。
- 图片附件仍然是文件引用；`type` 和 `mime_type` 只用于说明文件类型，不把额外分析结果写入 payload，也不把分析结果当作成功条件。
- payload 只是注入到 UserShell 输出中的用户输入上下文，不赋予附件路径或文本任何 shell 语义。

该方法的语义是：使用 thread 配置的 shell 执行 command；它保留 pipe、redirect、quoting 等 shell 语法，而且在当前上游实现中以宿主机 full access 执行。它可以加入已有 active turn，并以 `UserShell` command execution 的 user context fragment 将输出交给模型；这类 command execution 不会作为普通 command item 出现在 thread history 中。

这里有一个不能忽略的安全事实：`thread/shellCommand` 明确运行在 Codex sandbox 之外，不继承 thread 的 sandbox policy。它也不是普通 agent tool call，不会因为 thread 的 `approvalPolicy` 再自动弹出 command approval；app-server 会直接接受请求并通过 `item/*`、`turn/*` 推送执行进度。官方文档要求客户端只把它暴露给明确的 user-initiated command，因此本产品的 `APPLAUSE` reaction 本身就是产品层确认，不能把 Codex approval 框当作第二道防线。

同机不等于可以依赖 thread sandbox：`thread/shellCommand` 仍然是 full access，文件 staging 不能依赖 Codex sandbox 来限制读取范围。

### Shell 识别与读取命令

v1 明确限定 relayd 与 Codex app-server 运行在同一台机器。`thread/shellCommand` 没有 `shell` 参数，因此不能先随便发一条探测命令再根据输出猜测；探测命令本身就会重新遇到 shell 语法问题。应由 Codex app-server/wrapper 暴露它已经选择的实际本地 shell，不能只按 `GOOS`、`$SHELL` 或 `ComSpec` 推断，因为实际选择还可能受用户 login shell、PowerShell 可用性和 fallback 影响。

拿不到可信 shell 结果时不发送 `thread/shellCommand`，让 queue item 走失败恢复；不能猜一个 `cat` 或 `type`。

Shell 解析结果必须缓存，不能每条 `APPLAUSE` 都重新做 shell metadata lookup：

- 以 backend instance/app-server connection 和本地 runtime identity 作为缓存键；缓存值只包含归一化的 shell kind/path，不持久化用户文本或临时文件路径。
- 同一键的并发探测使用 single-flight，只允许一个真实探测请求，其余请求共享结果。
- wrapper 重连、app-server 重启、execution environment 变化、shell 元数据明确变化或探测失败时使缓存失效；没有可信 shell 结果时使用短暂负缓存，避免 reaction 高峰造成探测风暴。
- 已接受的 `thread/shellCommand` 后续结果未知时不能因为缓存失效而自动重新探测并重试，避免重复注入；只有 command 尚未发出且 lookup 失败，才允许失效后重新解析一次。

文件内容本身不需要 shell escape；只有 runtime 生成的临时文件路径需要按已识别的 shell 做 quoting。读取命令映射为：

| shell | 固定读取命令 | 备注 |
| --- | --- | --- |
| POSIX (`sh`/`bash`/`zsh`) | `cat <quoted-path>` | 输出原始文件字节 |
| PowerShell | `Get-Content -Raw -Encoding UTF8 -LiteralPath <quoted-path>` | 不依赖 `cat`/`type` alias，保留多行内容 |
| CMD | `type <quoted-path>` | `call` 是执行 `.bat`/`.cmd`，不是读取文件，不能使用 |

因此安全边界是“内容不进 shell parser，路径由 runtime 生成并转义”，而不是“完全不需要 quoting”。

不应把队列文本直接拼接成 `echo <queue text>`。adapter 只支持 `payload-file`：先在同机 Codex shell 实际运行的环境中创建随机、权限为 `0600` 的 UTF-8 临时文件，shell command 只携带经过安全 quoting 的文件路径并读取文件内容。文件内容不会进入 shell parser，因此不会被当成 pipe、redirect、command substitution、引号或换行处理；命令长度也只与临时文件路径有关。

临时文件必须由可信的 runtime 组件在本功能专用临时根目录下按 `runtimeID` 分目录，以排他方式创建，随机文件名、权限 `0600`，并拒绝符号链接和用户提供的路径。正常路径下，shell payload 文件在 UserShell command 完成、明确拒绝或失败后立即清理；结果未知时保留带 TTL 的临时文件记录，不重试命令。若 payload 引用了 queue item 的图片，图片 staging 文件不能随 payload 文件一起提前删除，必须保留到当前 active turn 完成、中断、恢复收口或 TTL 到期，并由 active-turn 的引用关系负责清理。daemon/wrapper 启动时必须只扫描本功能专用根目录，清理属于旧 runtime 且已过 TTL 的 runtime 目录及其文件，处理上次崩溃遗留物；不能用宽泛的系统临时目录清理代替。文件名、路径和内容不得写入普通日志。由于 `thread/shellCommand` 是 full access，文件 staging 不能依赖 Codex sandbox 来限制读取范围。

文件载荷仍受文件大小、shell stdout 捕获上限、app-server 输出上限和模型上下文窗口限制，不能理解为无限长度通道。实现必须覆盖 pipe、redirect、command substitution、反引号、引号、换行、CRLF、Unicode 和超长文本测试。文件载荷写入使用 UTF-8；PowerShell 显式指定 UTF-8，`cmd` 的最终中文表现仍取决于宿主 code page 和 app-server 的输出解码。

在 relay 中增加 `thread/shellCommand` command model 前，应先由 Codex adapter 做版本/能力探测。没有能力声明或 native 请求契约不匹配时，不发送 command，不给成功 reaction。

参考：

- [Codex `ThreadShellCommandParams`](https://github.com/openai/codex/blob/main/codex-rs/app-server-protocol/schema/json/v2/ThreadShellCommandParams.json)
- [Codex `thread/shellCommand` active-turn 测试](https://github.com/openai/codex/blob/main/codex-rs/app-server/tests/suite/v2/thread_shell_command.rs)

## 5. 产品语义

### 5.1 Reaction 定义

```text
APPLAUSE
```

`APPLAUSE` 同时承担两个可见含义：

1. 用户 reaction：请求把这条排队消息以 user shell command 注入当前 turn。
2. 机器人 reaction：确认该请求已被 relay/app-server 接收并开始处理。

机器人自己的 reaction created 回流继续按现有规则忽略，不会再次进入 action 层。

### 5.2 触发条件

`APPLAUSE` 只有同时满足下列条件才会领取 queue item：

- action 来自用户，而不是 bot 自己的 reaction 回流。
- 入站事件已经通过现有 bot reaction 过滤；机器人补的确认 reaction 不能成为再次处理。
- reaction 类型是 `APPLAUSE`。
- target message 命中当前 surface 的 queued queue item 的主文本 `SourceMessageID`。
- queue item 状态仍是 `queued`。
- queue item 的 frozen execution thread 与当前 active turn 的 thread 相同。
- 当前 surface 有可用 active turn，且该 turn 不是 compact，也没有处于需要用户决策的 request gate。
- attached instance 声明支持 `thread/shellCommand`。
- 同机专用临时目录可安全创建，且 cleanup manager 可用。
- queue item 有可注入的文本；绑定图片可以作为结构化附件引用加入 payload，且每个引用的 runtime 文件仍然有效。

图片消息自身、已 dispatching/running/completed 的消息、其他 surface/thread 的消息、没有 active turn 的消息均不触发；如果图片已经绑定到 queued 主文本 item，则随该主文本一起处理。

### 5.3 与 `ThumbsUp` 的互斥

互斥的事实来源是 queue item 的 action claim，而不是 Feishu 当前显示了哪些 reaction。原因是：

- 当前没有消费 reaction deleted 事件。
- 用户 reaction 与机器人 reaction 的记录是两个不同 operator 的记录。
- reaction created 会经过 per-surface FIFO，但不应再引入一次远端 reaction list 查询来决定执行权。

两个入口共享一个按 queue item 串行化的领取操作：

| queue item action 状态 | `ThumbsUp` | `APPLAUSE` |
| --- | --- | --- |
| `queued`，未领取 | 可以尝试 claim steer | 可以尝试 claim shell command |
| `steering` / `steered` | no-op | no-op |
| `shell-commanding` / `shell-commanded` | no-op | no-op |
| `dispatching` / `running` / `completed` / `failed` / `discarded` | no-op | no-op |

谁先在 orchestrator 中成功 claim，谁赢得这条 queue item。另一种 reaction 后续即使到达，也只做静默 no-op。

这里的“已经点过赞”定义为“点赞动作已成功领取并进入 steering”，不是“Feishu 上曾经出现过一个用户 `ThumbsUp` 记录”。如果点赞因为没有 active turn、backend 不支持或目标已经失效而没有成功 claim，该 reaction 不应把 queue item 锁死；否则会出现用户无意中点了一次点赞后，消息永远无法继续处理的问题。

成功 claim 后才产生状态迁移。单纯收到一个不适用的 reaction 不改变 queue item，不发 notice，也不补任何机器人 reaction。

### 5.4 成功确认时机

机器人补 `APPLAUSE` 的时机与现有 steer 的 `ThumbsUp` 对齐：

- orchestrator 已经把 queue item 从普通队列移出并绑定 pending shell-command command。
- wrapper/app-server 已接受该 command，返回 `command_ack.accepted=true`。
- 这表示请求已经启动，不表示 shell command 的输出已经完成，也不表示当前 turn 最终成功。

成功确认的投影顺序：

1. 清掉原消息上的 `OneSecond`。
2. 将 queue item 标记为 `shell-commanded` 终态，不再参与后续普通 queue dispatch。
3. 在原消息上添加机器人 `APPLAUSE`。
4. 保持当前 active queue item、active turn 和剩余 queue 顺序不变。

不添加 `THINKING`：当前 turn 已经处于处理态，`APPLAUSE` 不是新的普通 dispatch。

## 6. 状态机

### 6.1 Queue item 状态

现有 `steering/steered` 不能直接复用，因为复用会让 command ack、失败恢复和 reaction 投影误走 `ThumbsUp` 路径。建议新增独立的 transient/terminal 状态：

```text
queued
  ├─ claim ThumbsUp  -> steering -> steered
  └─ claim APPLAUSE  -> shell-commanding -> shell-commanded
```

`shell-commanding` 的含义是 command 已由 orchestrator 领取，但 command ack 尚未收回。该 item 从 `QueuedQueueItemIDs` 移出，但不成为 `ActiveQueueItemID`；当前 active queue item 仍然拥有原 turn。

`shell-commanded` 的含义是该 queue item 的文本已经作为 user shell command 请求交给当前 turn，一次性消费完成，不再回到普通 queue。

失败路径：

```text
shell-commanding -- dispatch failure / command rejected --> queued（恢复原位置）
```

恢复时必须恢复原 queue index、`OneSecond` 投影和可再次领取资格。不得把失败的 shell command 标记成 `ThumbsDown`，因为 `ThumbsDown` 的既有语义是用户撤回/丢弃。

### 6.2 原子 claim

`handleReactionCreated` 不应分别实现两套“先检查、再移出队列”的逻辑。应抽出 queue item action claim，至少原子检查：

- item 仍存在且 status 为 `queued`；
- target message 是主文本；
- frozen thread 与当前 active turn 一致；
- 当前 item 没有其他 pending steer/shell-command binding；
- backend capability 和输入类型满足对应 action。

claim 成功后立即写入独立 pending binding，绑定：

- `SurfaceSessionID`
- `InstanceID`
- `QueueItemID`
- 原 queue index
- `SourceMessageID`
- expected `ThreadID`
- expected `TurnID`
- relay `CommandID`

这样 reaction FIFO 中连续到达的 `ThumbsUp` / `APPLAUSE` 不会双重消费同一个 item。

### 6.3 active turn 竞态

`thread/shellCommand` 在上游没有以 `turnId` 作为参数；上游在没有 active turn 时可能自行执行并建立新的 shell command turn。为保持本产品“只把排队消息注入当前 turn”的语义，relay/wrapper 必须在发 native 请求前再次检查：

- expected thread 仍是当前执行 thread；
- expected turn 仍是当前 active turn；
- turn 没有已经完成、被中断或进入 compact/recovery 收口。

检查失败时拒绝这次 command，恢复 queue item，不允许 fallback 成一个新的独立 turn。

### 6.4 重启与未知结果

`shell-commanding` 和 pending binding 属于 command in-flight 状态。daemon/wrapper 在 command 已发出但 ack 丢失时无法仅靠 `thread/shellCommand` 的返回值安全判断是否已经执行，因为上游 shell command 不提供本产品级幂等 key，且 command execution 不进入普通 thread history。

v1 采用 at-most-once 语义：

- command 尚未写入 wrapper 时失败：删除临时文件，恢复 queue，可重试。
- command 已写入但 ack/结果未知：不自动重试，避免同一消息被注入两次；记录 `shell_command_unknown`，保留 item 的 terminal/unknown 记录供诊断，临时文件交给 TTL 清理。
- daemon/wrapper 正常退出时清理当前 runtime 的全部临时文件；重启时只扫描本功能专用目录，清理旧 runtime 且已过 TTL 的临时文件。
- daemon 重启恢复时，发现没有可恢复 binding 的 `shell-commanding` item，不得直接再次发送；应转为 unknown 并通过一次短 notice 告知用户状态不确定。

后续若上游提供稳定 request id 查询或幂等语义，再考虑把 unknown 自动恢复为可重试。

## 7. 失败与用户反馈

建议不要为所有失败都增加 reaction。具体处理如下：

| 场景 | queue item | 机器人 reaction | 用户提示 |
| --- | --- | --- | --- |
| 与已成功 steer/shell claim 冲突 | 不变 | 不添加 | 静默 no-op |
| 非主文本、无 active turn、错误 thread、图片消息自身、backend 不支持 | 不变 | 不添加 | 静默 no-op |
| claim 后 command 尚未发出就失败 | 恢复原位置 | 不添加 `APPLAUSE` | 短文本提示，说明已放回队列 |
| wrapper/app-server 明确拒绝 command | 恢复原位置 | 不添加 `APPLAUSE` | 短文本提示，说明启动失败并已放回队列 |
| command 已接受并开始执行 | `shell-commanded` | 添加 `APPLAUSE` | 不追加提示 |
| command 已发出但结果未知 | `shell-command-unknown` | 不伪造成功 reaction | 一次短文本提示，说明未自动重试 |

真正启动失败时建议使用 reply 到原消息的短文本：

```text
这条排队消息暂时没有启动成功，已放回队列。
```

冲突和不适用场景不提示的理由：这不是系统错误，且用户已经能从“没有收到机器人同款 `APPLAUSE`”看出没有成功确认。给每个竞争 reaction 都回一条 notice 会在群里制造噪声，并可能让用户误以为 reaction 本身被系统消费了。

不建议使用：

- `ThumbsDown`：已有“丢弃/取消”语义，会把启动失败误读为用户主动丢弃。
- `CrossMark`：需要额外定义新的错误 reaction，且用户 reaction 无法由 bot 可靠撤回。
- `APPLAUSE`：失败时不能由 bot 再补同款，否则会把失败伪装成成功。

## 8. Relay command contract

建议在 `internal/core/agentproto` 增加独立 command，而不是把它编码成 `CommandTurnSteer` 或普通 `CommandPromptSend`：

```text
CommandThreadShellCommand = "thread.shell_command"
```

command 至少携带：

- `Target.ThreadID`：native `threadId`。
- `Target.TurnID`：relay 侧 expected active turn，用于 stale-turn guard，不直接下发给 native app-server。
- `Origin.Surface/UserID/ChatID/MessageID`：审计和错误路由。
- `ShellCommand.Text`：待注入的 `queued_input_bundle_v1` 结构化文本，包含原始文本和附件引用；不要让 core 直接暴露任意 shell 字符串。
- `ShellCommand.PayloadRef`：同机临时文件引用；必须绑定 runtime、thread、command ID 和 TTL，不能接受用户提供的任意路径。

wrapper/adapter 在同机专用临时目录 staging payload 文件后，只生成固定的文件读取 command，再发送 native `thread/shellCommand`。优先等待 `UserShell` command execution 完成后清理 payload 文件；仅收到 accepted ack 不足以提前删除。payload 引用的图片文件由 active turn 的附件生命周期单独保留，不能随 payload 文件提前删除。command ack 绑定到独立 `pendingShellCommandBinding`，不进入 `pendingSteerBinding` 或普通 remote turn binding。

能力字段建议增加：

```text
Capabilities.ThreadShellCommand
```

只有 Codex app-server 在完成 native method probe、shell quoting 实现和 active-turn guard 后声明该能力。Claude/OpenCode 默认不声明。

## 9. Feishu 投影与入站

### 9.1 入站

现有 reaction created 解析已经保留任意 `emoji_type` 字符串，因此不需要新增 Feishu webhook schema。orchestrator 增加 `APPLAUSE` 分支，并复用当前：

- bot reaction 回流过滤；
- per-surface FIFO；
- queued 主文本 source message 命中；
- frozen thread 校验。

### 9.2 出站

现有 `PendingInputState` 的 `ThumbsUp`/`ThumbsDown` 布尔字段不能表达新的确认 reaction。实现时建议增加一个明确的 reaction add carrier，例如：

```text
ReactionAdds []string
```

同时保留旧字段，避免在普通 steer/discard 路径引入无关改动。APPLAUSE 成功事件应表达：

```text
QueueOff = true
ReactionAdds = ["APPLAUSE"]
```

projector 把它映射为：

1. remove `OneSecond`；
2. add `APPLAUSE`。

不要在同一事件中设置 `ThumbsUp=true`，也不要删除用户添加的 `APPLAUSE`。

## 10. 实现拆分

建议按以下边界推进：

1. `agentproto`：增加 shell command kind、payload、capability 和 ack/问题关联字段。
2. Codex adapter/wrapper：实现 native `thread/shellCommand` 翻译、active-turn guard、同机专用目录 payload staging、附件引用和图片生命周期转移、正常/异常/重启清理、平台 quoting、native error 归一化和 user-shell event 关联。
3. orchestrator：增加 APPLAUSE reaction handler、统一 queue item action claim、pending shell binding、accepted/rejected/unknown 收口。
4. control/eventcontract：增加 reaction add carrier；保持旧 `ThumbsUp`/`ThumbsDown` 字段兼容。
5. Feishu projector：投影 `APPLAUSE`，保证队列 reaction 与确认 reaction 的操作顺序。
6. canonical 文档：实现完成后同步 [Feishu product design](../general/feishu-product-design.md)、[relay protocol spec](../general/relay-protocol-spec.md) 和 [remote surface state machine](../general/remote-surface-state-machine.md)。

## 11. 测试要求

### Orchestrator

- queued 主文本 + active 同 thread + Codex capability：APPLAUSE 生成独立 shell command，不生成 `turn.steer`。
- command accepted：item 变为 `shell-commanded`，移除 `OneSecond`，补 `APPLAUSE`，active item/turn 不变。
- APPLAUSE claim 后收到 `ThumbsUp`：无额外 command、无 notice。
- `ThumbsUp` claim 后收到 APPLAUSE：无额外 command、无 notice。
- 两种 reaction 连续进入同一 FIFO：只有先成功 claim 的 action 生效。
- 无 active turn、错误 frozen thread、compact/request gate、图片消息自身、OpenCode/Claude 或无 capability：item 不变且静默；图片绑定在主文本 item 时应保留在 payload 引用中。
- command dispatch failure / explicit reject：恢复精确 queue index，重新投影 `OneSecond`，不补 `APPLAUSE`，产生短失败提示。
- ack 丢失/未知结果：不自动重试，不重复注入。
- bot 自己补的 APPLAUSE reaction 回流：不产生第二次 shell command。

### Codex adapter/wrapper

- native 请求字段准确包含 `threadId` 和按已识别 shell 生成、仅引用临时文件路径的固定读取 command。
- payload 文件准确包含 `queued_input_bundle.v1`、原始文本和 `ref/type/path/mime_type` 附件引用；图片引用只表达文件类型和位置，不触发额外的视觉分析流程。
- shell 识别优先使用 execution-environment 元数据；缺少可信 shell 元数据时 fail closed，不通过执行探测命令猜测。
- 同一 execution environment 的连续命令只触发一次 shell metadata lookup；并发 lookup 合并为一个请求，重连或环境变化后重新解析。
- shell metadata lookup 的失败结果有短暂负缓存；command 已发出但结果未知时不因缓存失效而重试。
- payload-file 模式下，shell command 只包含安全转义后的临时文件路径，读取结果与原始 UTF-8 内容一致；payload 文件完成、拒绝、失败和 TTL 到期都会清理，仍被 active turn 引用的图片文件按 active-turn 生命周期清理。
- 文件载荷覆盖 shell metacharacters、换行、引号、CRLF、Unicode 和超长文本，不会突破固定 command 结构；不生成 inline/`echo` command。
- 文件 staging、command 失败、正常退出和重启清理都必须有测试；超过现有 queue/input 上限时明确失败并恢复队列。
- 临时文件仅对实际 execution environment 可见，权限不是 `0600` 或路径跨 environment 时拒绝执行。
- `thread/shellCommand` 不产生 Codex command approval 请求，也不继承 thread sandbox；`command/exec` 的 sandbox/approval 测试不能替代这条 UserShell 测试。
- native method 只允许加入 expected active turn；不能在无 active turn 时意外新建 shell-only turn。
- native method reject、local environment unavailable、transport error 都能归一化到正确 ack/problem。
- `UserShell` command execution 的 started/output/completed 关联不污染普通 thread history 投影。

### Feishu projector/gateway

- `QueueOff + ReactionAdds=[APPLAUSE]` 投影为先 remove `OneSecond`、再 add `APPLAUSE`。
- 旧 `ThumbsUp` 投影测试行为不变。
- `APPLAUSE` 入站可解析，bot reaction 不回流触发。
- 不生成 `ThumbsDown` 或 `CrossMark` 作为失败替代。

## 12. 决策摘要

- 新 reaction：`APPLAUSE`。
- `ThumbsUp` steer：保留原行为。
- 互斥依据：queue item action claim，不查 Feishu reaction 列表。
- 冲突/不适用：静默 no-op。
- 真正启动失败：恢复原队列位置，并给短文本提示。
- 成功确认：wrapper/app-server command accepted 后补机器人 `APPLAUSE`。
- 失败 reaction：不新增，不复用 `ThumbsDown`。
- backend：v1 仅支持声明并实现 `thread/shellCommand` 的 Codex app-server。
- 权限：`APPLAUSE` 是产品层的明确用户确认；`thread/shellCommand` 本身不提供 sandbox 或二次 approval 保护。
- 性能：shell 元数据按 backend connection + local runtime 缓存，并合并并发探测，不按每条消息重复查询。
- 载荷：只写同机 execution environment 的 UTF-8 临时文件，shell command 只读取文件；payload 以 `queued_input_bundle.v1` 注入原始文本和附件类型/路径/MIME 引用；不提供 inline/`echo` fallback。
- 安全：队列文本只能作为纯文本输出，不能成为任意 shell 语法；文件路径也不能由用户提供。
