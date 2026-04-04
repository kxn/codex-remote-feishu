# 实施变更总表

## 1. 文档定位

这份文档不是新设计本身，而是“从当前代码基线迁移到已冻结设计”所需变更的总清单。

它解决三个问题：

- 把变化按 `wrapper / server / bot / install` 分层列全
- 把“必须改”和“明确先不做”分开
- 给后续编码提供一个不会漏项的执行 checklist

相关文档：

- [app-server-redesign.md](./app-server-redesign.md)
- [relay-protocol-spec.md](./relay-protocol-spec.md)
- [feishu-product-design.md](./feishu-product-design.md)
- [install-deploy-design.md](./install-deploy-design.md)

---

## 2. 总体结论

从当前实现到目标设计，变化不是“补几个 bug”，而是五类结构性调整：

1. `wrapper` 不再承接产品层语义，只做 agent adapter。
2. `server` 从“转发 + 少量状态”升级成唯一的产品状态中心。
3. `bot` 不再消费原生协议，不再自己猜文本边界和 turn 状态。
4. 输入、queue、图片、reaction、thread 选择全部上收到 `server`。
5. 安装部署从 repo 内临时脚本，升级为正式配置模型和集成策略。

如果只改其中一两层，系统仍会保留老问题：

- 本地 VS Code 和飞书抢 turn
- thread / cwd 路由不稳定
- 文本切分继续靠猜
- 图片、reaction、queue 无法统一建模
- 安装配置在不同启动方式下继续失效

---

## 3. 必须变更的协议面

### 3.1 Native -> Wrapper 观测面必须补全

`wrapper` 必须稳定观测并保留下面这些原生信号：

- `thread/start`
- `thread/resume`
- `turn/start`
- `turn/steer`
- `turn/interrupt`
- `thread/loaded/list`
- `thread/read`
- `thread/started`
- `thread/name/updated`
- `thread/tokenUsage/updated`
- `turn/started`
- `turn/completed`
- `item/started`
- `item/completed`
- `item/*/delta`
- approval / request-user-input / tool call request

这里最重要的新要求有两个：

- `turn/steer` 不能再被忽略
- 本地客户端直接发出的请求，不能再只在 wrapper 内部消化，必须翻译成 canonical signal 暴露给 server

### 3.2 Wrapper -> Server canonical event 面必须变化

必须具备的 canonical event：

- `agent.hello`
- `agent.welcome`
- `agent.event.batch`
- `threads.snapshot`
- `thread.discovered`
- `thread.focused`
- `local.interaction.observed`
- `turn.started`
- `turn.completed`
- `item.started`
- `item.delta`
- `item.completed`
- `request.started`
- `request.resolved`
- `agent.command.ack`
- `agent.native.frame`

其中这几个是当前设计新增或语义明显收紧的重点：

- `local.interaction.observed`
  - 由本地 `turn/start` 或 `turn/steer` 触发
  - 用于“尽早暂停远端 autosend”
- `turn.started.initiator`
  - 用于确认当前 turn 是 `local_ui` 还是 `remote_surface`
- `thread.focused`
  - 只接受真实的 focus 变化，不允许被后台输出隐式改写
- `agent.native.frame`
  - 只进 debug/replay，不进 bot 主链路
- `trafficClass = internal_helper`
  - helper/internal native traffic 不再靠 wrapper 内部 suppress 掉生命周期事件
  - 而是显式标注给 server

### 3.3 Server -> Wrapper command 面必须收缩

公共 canonical command 只保留：

- `prompt.send`
- `turn.interrupt`
- `request.respond`
- `threads.refresh`

明确不再允许把这些直接暴露为公共协议动作：

- 原生 `thread/start`
- 原生 `thread/resume`
- 原生 `turn/start`
- 原生 `turn/interrupt`

原因：

- server 只下发产品语义
- adapter 自己决定该走 `thread/start`、`thread/resume` 还是直接 `turn/start`

### 3.4 `prompt.send` 载荷必须结构化

`prompt.send` 不能再只有纯文本。

必须改成：

```ts
prompt: {
  inputs: Array<
    | { type: "text"; text: string }
    | { type: "local_image"; path: string; mimeType?: string | null }
    | { type: "remote_image"; url: string; mimeType?: string | null }
  >;
}
```

理由：

- 飞书图片需要和之后的文本一起提交
- server 需要对 queue item 冻结完整输入，而不是只冻文本

### 3.5 v1 明确先不做的协议能力

下面这些不是这轮必须实现的内容，但要在协议上明确为“未开放”，避免后面偷偷混进去：

- server 侧 canonical `turn.steer`
- auto detach
- bot 直接消费 native JSON-RPC
- bot 侧自己做 item/turn 分类
- 远端 block update / replace

---

## 4. Wrapper 层变更总表

对应当前主要代码面：

- [proxy.rs](/data/dl/fschannel/wrapper/src/proxy.rs)
- [ws_client.rs](/data/dl/fschannel/wrapper/src/ws_client.rs)
- [classifier.rs](/data/dl/fschannel/wrapper/src/classifier.rs)

### 4.1 必须保留的职责

- 连接真实 Codex app-server
- 解析原生 JSONL / JSON-RPC / notification
- 翻译为 canonical event
- 接收 canonical command 并翻译回原生命令
- 维护最小翻译状态
  - active turn
  - observed focused thread
  - thread cwd / template
  - request-response 关联

### 4.2 必须下移给 server 的职责

这些逻辑必须从 wrapper 里拿掉，或者至少不再由 wrapper 决策：

- attach / detach 语义
- 飞书用户是谁
- queue 语义
- staged image
- selection prompt
- reaction 取消
- 文本怎么分块发飞书
- 本地优先之外的产品提示文案

### 4.3 必须新增或补正的行为

1. 观察到本地 `turn/start` 时，立即上报 `local.interaction.observed(action=turn_start)`。
2. 观察到本地 `turn/steer` 时，立即上报 `local.interaction.observed(action=turn_steer)`。
3. 远端 `prompt.send` 触发的 turn，后续 `turn.started` 必须标记 `initiator=remote_surface`。
4. 本地客户端直接触发的 turn，后续 `turn.started` 必须标记 `initiator=local_ui`。
5. 对 thread 的 `cwd`、模板参数、协作模式要保留，不允许因为远端 prompt 而丢失。
6. 初始同步必须支持 `threads.refresh -> thread/loaded/list + thread/read`。
7. reconnect 时只能在“同进程 + 同 instanceId”下恢复，不得隐式合并新旧实例。
8. 不得在 wrapper 启动时主动 `startThread`。
   - 只在 server 明确发来 `prompt.send(createThreadIfMissing=true)` 时，才内部决定是否需要 `thread/start`。
9. `ephemeral` / `persistExtendedHistory` / `outputSchema`
   - 只允许影响模板复用和 canonical `trafficClass`
   - 不允许继续作为“吞掉 runtime lifecycle event”的依据。
10. native helper/internal turn 的 `turn.started` / `item.*` / `turn.completed`
   - 必须继续上送 server
   - 但要显式带上 `trafficClass=internal_helper`
   - 由 server 决定是否进入主状态机和普通 render feed

### 4.4 必须删除的错误假设

- “attach 了就只有飞书会用这个 instance”
- “本地交互一定会产生新的 `turn.started`”
- “后台输出能反推出当前 focus thread”
- “只要当前 turn 结束，远端就一定可以立刻发下一条”

---

## 5. Server 层变更总表

对应当前主要代码面：

- [relay-server.ts](/data/dl/fschannel/server/src/relay-server.ts)
- [session-registry.ts](/data/dl/fschannel/server/src/session-registry.ts)

### 5.1 状态主键必须变化

从旧的“按 userId / session 粗粒度建模”切到下面两套主键：

- instance 侧
  - `instanceId`
  - `workspaceKey`
- 用户 surface 侧
  - `surfaceSessionId`

这意味着 server 状态模型至少要新增：

- `InstanceRecord`
- `SurfaceConsoleRecord`
- `ThreadRecord`
- `QueueItemRecord`
- `StagedImageRecord`
- `PendingRequestRecord`
- `SelectionPromptRecord`

### 5.2 attachment 语义必须变化

attach 的对象始终是 instance，不是 thread。

attach 后：

- 默认 pin 到“attach 当下的 focused thread”
- 若当前没有 observed focus，进入 `unbound`
- 后续本地切 thread，不得偷偷改掉飞书默认输入目标

### 5.3 远端 prompt 路由必须冻结

server 收到飞书文本时，必须在入队那一刻冻结：

- `threadId`
- `cwd`
- `routeModeAtEnqueue`

后续即使用户又切 thread，已入队项也不得被改写。

### 5.4 queue 必须上收到 server

当前设计要求 server 成为 queue 的唯一权威方。

必须由 server 管：

- queued text
- active queue item
- staged images
- queue discard / cancel
- selection prompt 上下文
- typing / thumbsdown 的回写时机

明确不由 server 管：

- 本地 VS Code draft
- 本地 VS Code 自带 queue
- 本地中断后 editor 如何恢复 composer

### 5.5 本地优先仲裁必须正式建模

至少需要这些状态：

- `routeMode = pinned | follow_local | unbound`
- `dispatchMode = normal | handoff_wait | paused_for_local`
- `activeTurnOrigin = local_ui | remote_surface | unknown | null`
- `localPriorityUntil`

仲裁规则必须明确：

1. 观测到 `local.interaction.observed`
   - 立即进入 `paused_for_local`
2. 本地 turn 完成
   - 进入 `handoff_wait`
3. 窗口内再出现新的本地交互
   - 继续本地优先
4. 窗口结束仍无新的本地交互
   - 才恢复远端 autosend

补充一条硬约束：

- `local.interaction.observed(interactionClass=internal_helper)`
  - 不得触发 `paused_for_local`
  - 不得让飞书 queue 进入 handoff 逻辑

### 5.6 server 必须新增 renderer planner

server 不能再把“怎么切文本”丢给 bot。

必须在 server 内完成：

- item 强边界切分
- assistant text 软边界切分
- fenced code block 识别
- 文件列表代码块渲染
- 前导短句 / 尾注拆分
- committed block 规划

### 5.7 server 对 bot 的异步事件面必须稳定

bot 应只接收用户层 render/control 事件，例如：

- `snapshot.updated`
- `notice`
- `thread.selection.changed`
- `request.opened`
- `request.closed`
- `pending.input.state`
- `turn.state`
- `block.committed`

server 不应再把原始 `item.delta`、`turn.started`、`thread/started` 直接暴露给 bot。

补充约束：

- 对 `trafficClass=internal_helper` 的 canonical event
  - server 可以记录
  - 但默认不应产出普通 `block.committed`
  - 也不应污染 attach/use-thread 面向用户的 thread 视图

### 5.8 必须补上的失败处理

server 必须能明确区分：

- wrapper 不在线
- instance 不存在
- instance 被他人占用
- 当前没有 active turn 但用户点了 stop
- 没有 selected thread 且当前 route 不可用
- wrapper `command.ack = rejected`
- turn failed / interrupted

不能再只表现为“飞书没反应”。

---

## 6. Bot 层变更总表

对应当前主要代码面：

- [bot-service.ts](/data/dl/fschannel/bot/src/bot-service.ts)
- [commands.ts](/data/dl/fschannel/bot/src/commands.ts)
- [formatter.ts](/data/dl/fschannel/bot/src/formatter.ts)
- [relay.ts](/data/dl/fschannel/bot/src/relay.ts)

### 6.1 bot 的职责必须收缩

bot 只负责：

- 接飞书事件
- 翻译成 `SurfaceAction`
- 调 server 控制面
- 消费 `relay.render.v1`
- 映射成飞书文本 / 卡片 / reaction

bot 不再自己维护：

- attachment
- queue
- 当前 thread
- 正在运行哪个 turn
- 文本边界推断
- tool / final / progress 的分类规则

### 6.2 交互动作面必须变化

bot 需要稳定支持这些 action：

- `surface.menu.list_instances`
- `surface.menu.stop`
- `surface.menu.status`
- `surface.message.text`
- `surface.message.image`
- `surface.message.reaction.created`
- `surface.button.attach_instance`
- `surface.button.show_threads`
- `surface.button.use_thread`
- `surface.button.follow_local`
- `surface.button.detach`

其中第一版交互原则：

- `list` 返回在线 instance
- attach 支持按钮和数字序号两种路径
- `use-thread` 也支持按钮和数字序号
- stop 通过菜单触发

### 6.3 bot 展示层必须变化

必须具备：

1. 只展示文本类输出。
2. 中间工作过程只展示 server 已规划好的文本块。
3. 代码块 / 文件列表使用单独 block 呈现。
4. 每个完整 assistant text block 为一个飞书消息，不再按 delta 发。
5. `Typing` reaction 只在 queue item 进入 dispatching 后打上。
6. 处理完成或失败后，必须取消 `Typing`。
7. 被 stop 或 reaction 取消丢弃的 pending 输入，要打 `THUMBSDOWN`。
8. 所有面向用户的文案统一改成中文。

### 6.4 bot 需要支持的产品细节

- instance 标题优先显示 `workspaceKey`
- thread 标题格式优先显示 `实例短名 · thread 标题`
- attach / use-thread / follow-local 后要有明确提示
- 本地优先触发后要有 notice
- handoff 恢复远端队列后要有 notice
- 图片消息要先下载到本地临时路径，再告诉 server `surface.message.image`

### 6.5 明确先不做的 bot 能力

- 常驻独立控制面板
- bot 自己直接长驻持有所有 instance 状态
- auto detach
- 直接在 bot 侧决定消息该进哪个 thread

---

## 7. 安装与部署层变更总表

对应当前主要入口：

- [setup.sh](/data/dl/fschannel/setup.sh)

### 7.1 配置模型必须拆分

必须拆成：

- wrapper 配置
  - `~/.config/codex-relay/wrapper.env`
- services 配置
  - `~/.config/codex-relay/services.env`

不能再默认假设 VS Code 拉起 wrapper 时会继承 repo 根 `.env`。

### 7.2 编辑器接入策略必须正式建模

只能有两种正式模式：

- `editor_settings`
- `managed_shim`

自动模式只是二选一，不允许继续出现“脚本里隐式混搭”的状态。

### 7.3 安装器必须补充的能力

- 交互式配置
- 非交互式配置
- 安装状态记录
- status
- uninstall / rollback
- repo 模式和 user 模式
- `systemd-user` 和 `repo-pidfile` 两种部署路径

### 7.4 第一版明确先不做

- 直接替换系统真实 `codex`
- 强依赖公网 HTTP 回调
- 引入新的长期运行语言栈

---

## 8. 当前阶段明确不做的产品能力

这些点要明确写成 non-goal，防止后面实现时又偷偷加进去：

- auto detach
- 远端显式 `turn.steer`
- 飞书常驻控制台应用
- 清理本地 VS Code draft / queue
- bot 直接处理 native protocol
- 让后台输出自动改变当前 selected thread

---

## 9. 编码前必须完成的测试清单

后续一旦开始编码，至少要覆盖下面这些场景：

### 9.1 Wrapper / Protocol

- 本地 `turn/start` -> `local.interaction.observed(turn_start)`
- 本地 `turn/steer` -> `local.interaction.observed(turn_steer)`
- 远端 `prompt.send` -> `turn.started(initiator=remote_surface)`
- 本地 turn -> `turn.started(initiator=local_ui)`
- reconnect 同 instanceId 恢复
- wrapper 重启生成新 instanceId

### 9.2 Server / Orchestrator

- attach 后默认 pin 当前 focused thread
- attach 到新 instance 时清空 staged/queued 内容
- `follow_local` 下本地切 thread 会影响新 prompt
- `pinned` 下本地切 thread 不影响新 prompt
- 运行中收到本地交互，远端进入 `paused_for_local`
- 本地 turn 完成后进入 `handoff_wait`
- handoff 窗口结束后恢复远端 queue
- stop 清空飞书 queue 和 staged image
- reaction 取消 pending image / pending text

### 9.3 Bot / Product

- `/list` 只列在线 instance
- 数字回复 attach instance
- 线程列表卡片 -> 数字回复切 thread
- 图片先暂存，跟随下一条文本提交
- queue 中只有 dispatching 项有 Typing
- 完成 / 失败 / interrupted 都能取消 Typing
- 文件列表和代码块按完整 block 发，不按 delta 发
- 所有 notice / 错误文案为中文

### 9.4 安装部署

- 交互式 bootstrap
- 非交互式 bootstrap
- `editor_settings` 模式
- `managed_shim` 模式
- rollback / uninstall

---

## 10. 推荐的实现顺序

为了降低反复测崩的概率，推荐顺序冻结为：

1. 先改 `wrapper <-> server` canonical protocol，不碰 bot 展示。
2. 再让 server 成为唯一状态中心，补 queue / route / local-priority。
3. 再改 bot，只消费新 control/render 面。
4. 最后收敛安装器和 editor integration。

原因：

- 如果先改 bot，不先把 canonical event 补全，后面还会继续丢信息。
- 如果先改产品动作，不先把 queue 和 arbitration 收到 server，行为仍会不稳定。
- 如果先动安装器，不先冻结协议，后面还会再改配置接口。

---

## 11. 这一版最容易漏掉的点

最后单独列一遍，防止实现时再次漏掉：

1. `turn/steer` 必须纳入本地优先仲裁。
2. `local.interaction.observed` 和 `turn.started.initiator` 不是一回事。
3. attach 后默认是 pin 当前 thread，不是默认 follow-local。
4. 飞书 queue item 必须在入队时冻结 thread/cwd。
5. stop 只清飞书侧 queue，不清本地 VS Code 草稿。
6. bot 不再自己猜 final/progress/tool 边界。
7. 文件列表、代码块、尾注切分属于 server renderer，不属于 wrapper。
8. instance 命名以工作目录身份为准，不以固定 `vscode-codex` 为准。
9. wrapper 启动时不要主动 `startThread`。
10. 图片不是立即发 turn，而是先 staged，跟随第一条文本绑定。
