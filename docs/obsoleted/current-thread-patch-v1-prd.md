# Current Thread Patch V1 PRD

> Type: `obsoleted`
> Updated: `2026-11-21`
> Summary: 用户明确要求完全删除直接改写 Codex 会话历史及回滚功能；本文仅保留历史设计，不再实施。
> Superseded By: `docs/draft/feishu-agent-team-product-design.md`

2026-11-21：命令、菜单、回调、会话文件改写、事务状态与回滚实现均已删除，不保留兼容入口。

## 1. 文档定位

本文定义“当前 thread patch”能力的 V1 产品边界。

它只回答首版产品问题：

1. V1 到底修什么，不修什么。
2. 用户从哪里触发。
3. 正常路径和失败路径分别应该看到什么。
4. 这个能力需要哪些强门槛和事务保护。

本文不展开完整实现设计，但会给出会影响产品边界的存储与事务约束。完整技术计划见下列相关文档。

相关文档：

- [codex-session-patcher-research-2026-04.md](./codex-session-patcher-research-2026-04.md)
- [current-thread-patch-v1-tech-plan.md](./current-thread-patch-v1-tech-plan.md)
- Tracking issue: `#464 [Feature] 当前 thread patch V1：latest assistant turn 事务化修补`
- `#189 [Feature] 当前 thread 本地补丁事务（latest_turn / entire_thread）`

## 2. 背景

当前存在一个高频但边界很明确的需求：

- 当前会话的最近一轮 assistant 输出已经落盘
- 但这轮输出的文本内容不适合继续留在上下文里
- 典型场景是拒绝回复、明显错误的占位回复、需要人工修补的最后一轮 assistant 文本

如果只是离线改本地文件，问题不大；但放到本仓库里，还要同时满足：

- 当前 instance 可能已绑定到一个或多个 surface
- wrapper child 可能正在线运行
- surface 可能仍有 queued / dispatching / running 流量
- patch 后必须能继续在同一 thread 上工作
- 用户侧不能被“instance offline / online”噪音刷屏

所以 V1 的正确目标不是“通用 patch 引擎”，而是“一个非常窄、但事务语义正确的当前 thread 修补能力”。

## 3. V1 产品目标

V1 只追求一件事：

在当前 attached thread 完全空闲的前提下，允许受控地替换最新 assistant turn 的可见文本，并在 patch 后继续在同一 thread 上对话。

V1 必须满足：

1. patch 对象明确且唯一
2. patch 过程可回滚
3. patch 期间不会并发写坏当前 thread
4. patch 后同一 thread 仍可继续使用
5. 正常路径下对上层尽量透明，不向普通用户暴露离线/上线噪音

## 4. 非目标

V1 明确不做这些事：

- 不支持 `entire_thread`
- 不支持任意历史 turn 选择
- 不做 AI 自动改写
- 不做可视化 diff 编辑器
- 不同时覆盖 Claude / OpenCode 等其他 backend
- 不支持 VS Code 侧入口或 VS Code surface 使用该能力
- 不处理云端或远端持久层
- 不做普通用户默认暴露入口

## 5. V1 核心定义

## 5.1 当前 thread

V1 的“当前 thread”固定指：

- 当前 surface 已 attached 到某个 instance
- 且该 surface 当前已显式绑定 / 使用中的 thread

V1 不接受跨 instance、跨 workspace、跨 surface 指定其他 thread 的 patch 请求。

## 5.2 patch 目标

V1 的 patch 目标固定为：

- 当前 thread 的最新一个 assistant turn

这里的“最新 assistant turn”指：

- 当前 thread 中最近一次已经完整结束、且已持久化的 assistant 输出轮次

V1 不对“用户最后一条消息”或“未完成中的 turn”做 patch。

补充约束：

- 一次事务只处理一个最新 turn
- 但该 turn 内可以存在多个需要 patch 的候选点
- 这些候选点在同一事务内一次性提交，不拆成多个独立 patch 事务

## 5.3 patch 内容

V1 的 patch 内容固定为：

- 替换最新 assistant turn 的可见文本

如果底层持久化后端里还存在与该 turn 明确关联、且会影响后续上下文恢复的 reasoning / thinking 片段，V1 可以一并清理；但这属于存储兼容实现细节，不作为用户可配置项暴露。

如果系统在同一个 turn 里识别出多个候选点：

- 在同一张 patch 卡里按多题展示
- 每个候选点都有自己的预填默认模板输入框
- 用户统一确认后一次性提交
- V1 不支持只跳过其中一部分候选点

## 6. 目标用户与入口

V1 的目标用户不是普通聊天用户，而是：

- 运维者
- 开发者
- 管理员
- 需要修补当前线程状态的高级操作者

因此 V1 首个入口建议是两条等价入口：

- slash 命令（当前命令名为 `/bendtomywill`）
- 菜单项

这两条入口必须汇入同一条 patch 流程，而不是维护两套实现。

底层实现可以仍然落到 daemon command / admin API，但对用户可见的交互应统一为同一张前台 patch 卡。

显式约束：

- 当前只支持聊天/运维侧受控入口
- 如果当前 surface 是 VS Code，V1 一律拒绝执行
- VS Code 侧是否支持 patch，需要后续单独调研，不纳入本期
- replacement text 不通过 slash 参数直接提交
- 只有高权限用户可以发起
- 只有发起者本人可以继续确认、提交和回滚

V1 不要求首版就提供：

- 可视化 diff 页面
- 普通用户默认暴露入口
- 独立的 Web 管理页面

## 7. 用户体验

## 7.1 触发前

交互约束：

- 系统先对当前 thread 最新 assistant turn 做候选点检测
- 入口进入一张前台 patch 卡
- patch 卡打开后占用当前 surface 的前台交互，不允许切出去执行其他同层操作
- patch 卡展示每个候选点命中的片段摘要，而不是整段全文
- 每个候选点展示命中片段前后少量上下文
- 每个候选点默认预填一条模板文本
- 用户可以逐项修改，也可以一路直接确认默认模板
- 所有候选点确认完成后，才进入真正的 patch 事务
- V1 不要求用户手工提供 `thread_id`，目标 thread 由当前 attached surface 上下文自动推断

## 7.2 正常路径

正常路径下，体验应收敛成：

1. 操作者发起 patch 请求
2. 系统先做空闲门槛检查
3. 系统进入短暂维护窗口，并冻结新的状态改写类输入
4. 系统自动备份当前 thread 对应的持久化数据
5. 系统执行 patch
6. 系统恢复当前 thread 的运行上下文
7. 操作者收到一条成功提示
8. 后续下一条正常输入继续在同一 thread 上执行
9. 已经发出的旧消息不回改，只有后续上下文基于新内容继续
10. 正常路径下，上层只感知到一次 patch 成功结果，不感知中间 restart 细节

V1 不要求把中间技术细节默认暴露给普通用户。

## 7.3 失败路径

失败路径下，体验必须满足：

1. 当前 thread 保持可继续操作
2. patch 失败时自动回滚
3. 用户收到单条失败提示
4. 不遗留“看起来在线但无法继续”的半死状态

## 7.4 patch 期间的用户提示

V1 建议只保留受控提示，不传播原始运行噪音。

建议文案分三类：

- 开始
  - `正在修补当前会话，请稍候。`
- 成功
  - `当前会话已修补完成，后续输入会基于新内容继续。`
- 失败
  - `当前会话修补失败，已恢复到修补前状态。`

其他 surface 若在 patch 期间尝试发送会改写状态的新输入，应收到标准化门禁提示，而不是看到底层 child stop/start 细节。

进一步要求：

- patch 期间允许存在短暂内部维护窗口
- 但该维护窗口默认不向上层翻译成 instance offline / online 生命周期提示
- 只有 patch 失败、回滚失败或恢复失败时，才允许把异常显式暴露给用户

## 8. 强门槛

V1 必须要求当前 instance 处于明确空闲态。

至少包括：

- 无 `activeRemote`
- 无 `pendingRemote`
- 无 `pendingSteer`
- 无 queued / dispatching / running 的活动项
- 无未决 request / approval 交互
- 无其他前台 owner-card 流程正在占用该 surface
- 无其他 patch / compact / upgrade 等维护事务正在运行

如果任何门槛不满足，V1 直接拒绝执行，不做强制中断。

额外约束：

- 如果请求来自 VS Code 侧，视为不满足执行前提，直接拒绝

## 9. 事务要求

V1 需要以下事务保护：

## 9.1 单实例互斥

同一 instance 同时只允许一个 patch 事务。

## 9.2 surface dispatch 冻结

patch 窗口内要冻结新的状态改写类输入，避免在检查通过后又插入新 turn。

## 9.3 自动备份

patch 前必须自动生成可回滚备份。

## 9.4 失败回滚

只要 stop、patch、restart、restore 任一步失败，都必须回到可继续操作状态。

## 9.5 恢复到同一 thread

patch 成功后，wrapper / daemon 必须把 child 恢复到同一 thread，而不是只把 child 拉起来。

## 9.6 唯一运行态方案

V1 的运行态刷新不保留“热刷新”分支。

唯一接受的运行态方案是：

1. 冻结输入
2. 进入事务维护窗口
3. 重写持久化内容
4. 重启 child
5. 自动恢复到同一 thread
6. 释放维护窗口

这个过程中允许内部出现 stop / start / resume，但这些事件默认不应作为用户可见噪音上抛。

## 10. V1 建议产品切面

V1 建议只做以下最小切面：

### 10.1 入口切面

- 一个 admin API
- 一个窄参数模型

建议最小参数：

- `replacements[]`

其余 thread / instance 目标由当前 surface 上下文和当前 attached runtime 自动推断。

其中：

- `replacements[]` 对应当前最新 turn 内识别出的候选点集合
- 不支持只提交其中一部分
- slash / menu 只负责进入流程，不在命令行参数里直接承载 replacement text

### 10.2 存储切面

V1 不能把“SQLite 存 transcript”当成既定事实。

按当前 upstream 和本机安装版本的现实：

- thread / turn 正文仍以 rollout JSONL 为权威来源
- SQLite 当前主要承载 metadata、索引和派生状态
- 但 rollout path 解析和部分 metadata 读取已经存在 SQLite-first 路径

因此 V1 的正确切面应是：

- 当前只对 rollout JSONL 做正文 patch
- 预留 SQLite metadata reconcile hook
- 存储层抽象不能写死成“永远只有 JSONL”

### 10.3 体验切面

V1 只承诺：

- 可触发
- 可回滚
- 可恢复
- 噪音可控

V1 不承诺：

- 用户自己在 UI 上选 turn
- patch 前后可视化对比
- patch 历史浏览

## 11. 验收标准

V1 的完成标准建议收敛为：

1. 能对当前 attached thread 的最新 assistant turn 执行文本替换。
2. 一个 turn 内若识别出多个候选点，会在同一张前台 patch 卡中统一展示和统一提交。
3. 已经发出的旧消息不会被回改。
4. 只有发起者本人可以继续确认和回滚。
5. patch 执行前会拒绝 busy instance，而不是强行抢占。
6. 如果入口来自 VS Code 侧，会直接拒绝，并明确提示该能力当前不支持 VS Code。
7. patch 期间新的状态改写类输入会被门禁，而不会并发写坏 thread。
8. patch 前自动备份，失败时自动回滚。
9. patch 成功后能继续在同一 thread 上执行后续输入。
10. 正常路径下不向普通用户暴露底层 offline / online 噪音。
11. 正常路径下对上层只暴露 patch 结果，不暴露中间 restart 生命周期。
12. 失败路径下不会遗留僵尸 child、半死 queue 或假在线 surface。

## 12. 和 `#189` 的关系

V1 不是否定 `#189`，而是把它拆成一个可先落地的最小单元。

两者关系应当是：

- `#189`
  - 保留更大的“当前 thread patch 事务”方向
  - 后续可继续覆盖 `entire_thread`、多后端、复杂恢复策略
- V1
  - 只解决最小、最高频、最可验证的一段用户价值

如果 V1 落地稳定，再决定是否扩展到：

- `entire_thread`
- AI assisted rewrite
- diff / rollback UI
- 更通用的 patch 引擎

## 13. 建议下一步

当前已经有 `#464` 作为 V1 的独立 issue。

建议下一步是：

- 把 `#464` 提升到 `status:implementable-now`
- 以 [current-thread-patch-v1-tech-plan.md](./current-thread-patch-v1-tech-plan.md) 为主线补齐实现参考
- 进入实现时先做 storage foundation，再接 owner-card 和 runtime transaction

这样可以把 V1 和 `#189` 的更大范围持续隔开，不会在进入实现时再次混线。
