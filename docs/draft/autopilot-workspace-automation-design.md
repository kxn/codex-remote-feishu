# Workspace Autopilot Design

> Type: `draft`
> Updated: `2026-05-12`
> Summary: 收敛工作区级 `Autopilot.md` 的编译、加载、推理 runner、调度、开关、多 action 合并与现有 `/cron`、`autocontinue`、`autowhip` 的映射边界。

## 0. 文档定位

这份文档定义一个新的工作区级能力：`Autopilot`。

它的目标不是再做一套新的硬编码规则表，而是允许用户在工作区根目录下写一个自然语言的 `Autopilot.md`，再由模型把它编译成系统真正可执行的结构化配置。

本文只收敛 V1 的产品与技术边界：

1. `Autopilot.md` 应该怎样被加载和编译。
2. 编译后的 JSON 应该长什么样。
3. 运行时怎样以尽量可预测的方式执行它。
4. 它怎样覆盖现有 `/cron`、`autocontinue`、`autowhip` 的主要使用场景。
5. 哪些能力不应直接塞进 V1。

本文不是最终实现说明，当前也不要求和现有老功能一次性切换完成。

## 1. 设计目标

V1 追求四件事：

1. 用户只写自然语言，不直接写 YAML、JSON 或复杂参数表。
2. 运行时只执行“编译后的受支持 JSON”，不直接执行原始 `Autopilot.md`。
3. 尽量把不确定性前移到 `load` 阶段，而不是把不可预测性留到事件真正发生时。
4. 即使允许模型参与推理，也要把执行面收敛到少数已知 action 上。

V1 明确不追求：

1. 自动监听 `Autopilot.md` 变化并自动重载。
2. 一个无限开放的 action 系统。
3. 让模型在运行时自由调用任意内部能力。
4. 让用户自己处理规则优先级、冲突矩阵或参数 schema。

## 2. 用户心智模型

用户只需要理解这条链路：

`Autopilot.md -> /autopilot load -> 编译后的 JSON -> 运行时触发执行`

其中：

1. `Autopilot.md`
   - 是工作区根目录下的自然语言源文件。
   - 是唯一需要人工维护的 source of truth。
2. `/autopilot load`
   - 是唯一的显式装载动作。
   - 读取当前工作区 `Autopilot.md`，调用模型编译，验证通过后替换当前生效版本。
3. 编译后的 JSON
   - 是后端真正执行的版本。
   - 会按事件类型拆成独立 entry，供状态展示与按条开关使用。
4. 运行时
   - 只看编译结果，不再重新解释原始 markdown。

这意味着：

1. 用户不需要学习复杂 schema。
2. 后端也不需要在每次事件触发时重新理解整份 markdown。
3. “模型理解偏了”这件事主要在 `load` 时暴露，而不是在定时任务或 turn 已经结束后才暴露。

## 3. V1 明确决定

### 3.1 显式加载，不自动生效

V1 不做自动 load。

原因：

1. `Autopilot.md` 可能正在编辑中。
2. 自动重载会把半成品直接变成运行时行为，体验不可控。
3. 既然用户本来就需要显式确认这套自动化逻辑，那么 `load` 本身就是确认点。

因此 V1 只支持：

- `/autopilot load`

不支持：

- 自动 watch
- 自动 reload
- 单独的 `check` 命令

### 3.2 load 失败即报错，不保留独立 check 状态

如果 `load` 失败：

1. 当前已生效版本保持不变。
2. 给当前飞书窗口发送一条失败消息。
3. 失败消息只做即时提示，不需要再维护一份长期展示的“检查状态页”。

这符合当前需求：错误通常是人看一眼就能修的，不值得再维持额外状态机。

### 3.3 V1 不强调 unload，强调 on/off

V1 优先提供：

- workspace 级 `on`
- workspace 级 `off`
- rule 级 `on`
- rule 级 `off`

这里的语义是“当前已加载配置是否参与运行”，而不是“删除它”。

因此 V1 不要求单独做一个语义很重的 `unload`。已经 load 的编译结果可以继续保存在 daemon 状态里，只是暂时不启用。

## 4. 命令模型

建议 V1 暴露如下命令面：

- `/autopilot`
  - 返回当前工作区 Autopilot 摘要、加载状态、总开关和已识别 entries。
- `/autopilot load`
  - 读取当前工作区 `Autopilot.md`，编译、校验并替换当前生效版本。
- `/autopilot on`
  - 打开当前工作区已加载 spec。
- `/autopilot off`
  - 关闭当前工作区已加载 spec。
- `/autopilot rule on <entry_id>`
  - 打开某条编译后 entry。
- `/autopilot rule off <entry_id>`
  - 关闭某条编译后 entry。
- `/autopilot status`
  - 返回和 bare `/autopilot` 等价或近似等价的状态页。

`status` 页应以编译结果为准，而不是把原始 `Autopilot.md` 重新展示给用户。

至少展示：

1. 当前工作区路径
2. 最近一次成功 load 时间
3. 当前是否总开关开启
4. 已识别 entry 数量
5. 每条 entry 的：
   - `entry_id`
   - 标题/摘要
   - 事件类型
   - 当前是否启用
   - 预计 action 类型

## 5. 整体架构

V1 建议拆成五层：

1. Source layer
   - 工作区根目录 `Autopilot.md`
2. Compile layer
   - 模型把自然语言编译成受支持 JSON
3. Validation layer
   - 后端做 schema、能力矩阵和冲突约束检查
4. Registry layer
   - daemon 把通过验证的 spec 注册到当前实例的工作区级 registry
5. Runtime layer
   - turn 终态事件或 scheduler tick 到来时，用 registry 决定是否执行 action

关键原则：

1. 模型负责“理解用户意图并改写成机器可读版本”。
2. 后端负责“只允许受支持的事件、source、action 进入运行时”。
3. 运行时不再处理一整篇开放文本，而是处理有限 JSON。

## 6. 编译产物

### 6.1 编译结果为什么必须是 JSON

选择 JSON，不是为了把用户重新拉回结构化配置，而是为了给后端一个稳定执行面：

1. 事件必须结构化，否则 scheduler 和 turn hook 无法准确挂接。
2. source 必须结构化，否则运行时不知道该拉哪些上下文。
3. action 必须结构化，否则后端不知道该调用哪个已有能力。
4. rule 级开关必须有稳定 entry id，否则无法按条启停。

用户仍然只写 `Autopilot.md`。

JSON 只是“模型自己理解后写出来给系统执行的版本”。

### 6.2 编译结果建议结构

建议编译结果以工作区为单位保存，保存在 daemon state，而不是直接回写到工作区：

```json
{
  "version": "autopilot.v1",
  "workspace_root": "/abs/workspace",
  "source_file": "Autopilot.md",
  "source_sha256": "9f3d...",
  "compiled_at": "2026-05-12T10:23:11+08:00",
  "entries": [
    {
      "entry_id": "turn-terminal-continue-on-retryable-stop",
      "title": "停下时判断是否继续",
      "enabled_by_default": true,
      "event": {
        "type": "turn_terminal"
      },
      "sources": [
        {
          "kind": "turn_terminal_state"
        },
        {
          "kind": "turn_final_message"
        },
        {
          "kind": "thread_history_recent",
          "max_turns": 6
        }
      ],
      "decision": {
        "instruction": "当 turn 停下时，判断这是不是应该自动继续的情况。若已经完成、用户主动停止、或继续会明显放大损失，则不要继续。若只是模型粗糙结束、可重试失败、或还明显没做完，则可以继续。"
      },
      "draft_actions": [
        {
          "lane": "same_thread_prompt",
          "instruction": "如果决定继续，就向当前 thread 发一条简短 prompt，延续当前任务；若需要附加约束，例如提醒先读错误、先自检或注意测试，也并入同一条 prompt。"
        }
      ]
    },
    {
      "entry_id": "schedule-daily-morning-summary",
      "title": "每天早上发工作区摘要",
      "enabled_by_default": true,
      "event": {
        "type": "schedule_tick",
        "schedule": {
          "kind": "daily_at",
          "time": "09:00",
          "timezone": "Asia/Shanghai"
        }
      },
      "sources": [
        {
          "kind": "workspace_binding"
        }
      ],
      "decision": {
        "instruction": "每天上午在当前工作区启动一个新 thread，提醒模型检查今天应该推进什么。"
      },
      "draft_actions": [
        {
          "lane": "new_thread_prompt",
          "instruction": "创建一个新 thread，并发送一条提示，要求模型按当前工作区上下文梳理今日待办和优先级。"
        }
      ]
    },
    {
      "entry_id": "turn-terminal-post-better-summary",
      "title": "长 turn 结束后发更易读摘要到飞书",
      "enabled_by_default": true,
      "event": {
        "type": "turn_terminal"
      },
      "sources": [
        {
          "kind": "turn_terminal_state"
        },
        {
          "kind": "thread_history_full"
        }
      ],
      "decision": {
        "instruction": "当本轮内容很多而 final message 很粗糙时，生成一个更适合人阅读的总结。"
      },
      "draft_actions": [
        {
          "lane": "feishu_message",
          "format": "card",
          "instruction": "把这一轮真正做了什么、结论是什么、风险是什么整理成一条适合飞书阅读的卡片消息，发送到当前聊天。"
        }
      ]
    }
  ]
}
```

上面的 JSON 仍然保留自然语言字段，因为系统要限制的是“执行壳”，不是把所有提示词都参数化。

### 6.3 稳定 entry id

rule 级开关要求 `entry_id` 稳定。

因此编译器应尽量让 `entry_id` 来自：

1. 规范化后的标题/意图 slug
2. 事件类型
3. 必要时再加少量去重后缀

而不是每次 load 都生成随机 id。

如果用户改动不大，`entry_id` 应尽量保持不变，这样按条开关状态才能延续。

## 7. source 模型

`sources` 的作用是显式声明“运行时为了做这个判断，需要拉哪些上下文”。

这件事很重要，因为：

1. 有些规则只需要 turn 终态和 final message，不需要整段 thread。
2. 有些规则必须读完整 thread，否则总结质量不够。
3. 如果不显式声明 source，运行时很容易为了求稳把所有上下文都塞给模型，成本和噪音都会失控。

V1 建议只支持少量固定 source：

1. `turn_terminal_state`
   - turn 成功、失败、停止原因、错误族等摘要信息
2. `turn_final_message`
   - 当前 turn 的最终 assistant 文本
3. `thread_history_recent`
   - 最近 N 个 turn / item
4. `thread_history_full`
   - 当前 thread 的完整历史
5. `workspace_binding`
   - 当前工作区绑定信息
6. `schedule_tick`
   - 调度触发时间、时区和调度元信息

编译器的职责之一，就是从 `Autopilot.md` 的自然语言里推断出最小可用 source 集合。

## 8. action 模型

### 8.1 V1 只支持三类 action

V1 只开放下列 action 壳：

1. `send_prompt_same_thread`
   - 向当前 thread 再发一条 prompt
2. `send_prompt_new_thread`
   - 新起一个 thread，并发一条 prompt
3. `post_feishu_message`
   - 向当前飞书聊天发消息
   - `format` 支持 `text`、`card`、`auto`
   - `target` 支持：
     - `current_event_surface`
     - `workspace_bound_surface`

这三类 action 已经能覆盖当前主要目标：

1. AutoContinue / AutoWhip 类继续执行
2. Cron 类定时启动新任务
3. 长 turn 结束后把更适合人看的总结发回飞书

### 8.2 action 内仍允许自然语言

为了避免把用户重新拖回参数配置地狱，action 本身仍允许自然语言描述。

例如：

- “继续，但提醒它先看错误并且注意测试”
- “新开一个 thread，让模型梳理今天的待办”
- “整理成一条适合飞书老板看的摘要卡片”

后端只需要保证：

1. 投递目标是确定的
2. 使用的是受支持 action
3. 文本或卡片是由模型基于允许的 source 生成

### 8.3 `Autopilot` runtime 本身应是 one-off

当前更合理的收敛方式是：

1. 一个事件命中某条 entry
2. `Autopilot` 只做一次模型推理
3. 这次推理不带额外 tool
4. 它只产出一个顶层结果：
   - `skip`
   - 或一个 `primary action`

也就是说，`Autopilot` 自己不应该长成一个会持续等待、再判断、再出 follow-up 的小编排器。

它更像：

- `event -> one-off reasoning -> one primary action`

### 8.4 如果 action 拉起 turn，后续复杂行为属于那个 turn

当 `Autopilot` 产出的 `primary action` 是：

1. `send_prompt_same_thread`
2. `send_prompt_new_thread`

那么 `Autopilot` 的职责在“把这个 turn 拉起来”时就基本结束了。

后续如果还需要：

1. 修 CI
2. 读取更多上下文
3. 做多轮操作
4. 成功后再发消息

这些都更适合放到被拉起的那个 turn 自己去完成。

也就是说：

1. `Autopilot`
   - 负责决定“要不要启动这次执行”
   - 负责提供这次执行的 prompt
2. 被启动的 turn
   - 负责真正做事
   - 若有需要，可以在被授予的能力范围内自己发消息、回写或调用额外 MCP

### 8.5 因此 action 需要带“能力画像”而不需要带 follow-up

对于会拉起 turn 的 action，V1 更适合附带一个受限的能力画像，例如：

1. `default`
   - 普通执行
2. `workspace_surface_notify`
   - 允许这个 turn 使用工作区通知能力
3. 未来再增加其他受控画像

这里的关键约束是：

1. `capability_profile` 是固定白名单，不是用户自由拼装的 MCP 列表
2. 被授予额外能力的是“被拉起的执行 turn”
3. 不是 `Autopilot` 自己获得 tool 使用权

这样 `Autopilot` 仍然是 one-off，但被拉起的执行 turn 可以在有限能力内完成更复杂的闭环。

### 8.6 典型例子：定时修 CI，修好后通知

这个例子更适合表达成：

```json
{
  "entry_id": "schedule-fix-ci-and-notify",
  "event": {
    "type": "schedule_tick",
    "schedule": {
      "kind": "interval",
      "every_minutes": 30
    }
  },
  "action": {
    "type": "send_prompt_new_thread",
    "capability_profile": "workspace_surface_notify",
    "instruction": "检查当前工作区 CI 是否绿色；如果没有成功，就继续修复直到通过。修复完成后，使用工作区通知能力给当前 workspace 绑定的 surface 发一条消息说明 CI 已修复。"
  }
}
```

这里最重要的关系是：

1. 定时事件只负责触发一次 `Autopilot` one-off 决策
2. `Autopilot` 只负责拉起那个执行 thread
3. “修好后发消息”不是 `Autopilot` follow-up
4. 而是执行 thread 在被授予的 MCP / tool 能力下自己完成

## 9. 编译与校验流程

### 9.1 `/autopilot load` 的执行步骤

建议如下：

1. 读取当前工作区 `Autopilot.md`
2. 调用编译模型，输入：
   - 原始 markdown
   - 当前支持的事件矩阵
   - 当前支持的 source 类型
   - 当前支持的 action 类型
   - JSON schema
   - 若干正反例
3. 要求模型只输出 JSON
4. 后端做确定性校验
5. 若校验失败，可选做一轮“带错误反馈的修正重试”
6. 仍失败则 load 失败，并向当前飞书窗口发错误消息
7. 若编译结果包含 `schedule_tick` entry，则在 load 时同步解析或确认当前工作区的 `runner contract`
8. 成功则写入 daemon state，替换当前工作区已加载 spec

### 9.2 校验必须覆盖什么

至少包括：

1. JSON schema 合法
2. 事件类型受支持
3. source 类型受支持
4. action 类型受支持
5. schedule 表达能落到现有 scheduler 支持的形式
6. `entry_id` 唯一
7. 每条 entry 至少有一个 action
8. 需要完整 thread 的规则必须显式声明相关 source
9. 过于模糊、后端无法承诺的 action 必须被拒绝
10. 每条 entry 顶层只允许一个 `primary action`；模型若产出多个，编译器必须折叠成一个或直接拒绝
11. `capability_profile` 或额外 MCP grant 必须来自受支持白名单
12. 若 spec 中存在 `schedule_tick` entry，则当前工作区必须能拿到可用的 `runner contract`

这里的原则是：

能在 load 阶段明确失败的，就不要拖到运行时失败。

## 10. 单条规则 one-off 与多条规则命中时怎样执行

这是 V1 最需要收敛的不确定性来源。

### 10.1 单条 entry 一次触发只产出一个顶层结果

当一个事件命中某条 entry 时：

1. 读取这条 entry 需要的 sources
2. 做一次 `Autopilot` 模型推理
3. 得到一个顶层结果：
   - `skip`
   - 或一个 `primary action`

这个 `primary action` 可以是：

1. 往当前 thread 发 prompt
2. 新起一个 thread 发 prompt
3. 直接往飞书发一条消息

但它不应该是“先拉起 turn，再由 Autopilot 自己继续挂着等结果”的 workflow。

### 10.2 如果 action 拉起 turn，Autopilot 就到此结束

如果 `primary action` 是启动一个 turn：

1. `Autopilot` runtime 在这个动作 dispatch 完后就结束
2. 后续事情属于那个被启动的 turn
3. 如果它被授予了额外 MCP / tool，它可以在自己的执行过程中自行发消息或完成闭环

### 10.3 多条 entry 同时命中时，再产出 draft actions

1. 找出所有匹配且已启用的 entries
2. 为每条 entry 取它声明的 sources
3. 让模型分别产出自己的单个 `primary action` 或 `skip`
4. 得到一组 `draft actions`

注意：这里的结果还不是最终执行结果。

### 10.4 按 lane 合并

V1 不让这些 draft action 直接全量乱发，而是先按 lane 分组：

1. `same_thread_prompt`
2. `new_thread_prompt`
3. `feishu_message`

每个 lane 再走一次“合并器”：

1. 如果只有一条 draft action，直接执行
2. 如果多条 action 明显兼容，就合并成一条
3. 如果多条 action 互相冲突，就拒绝执行该 lane，并发一条错误提示

例如：

1. 一条规则建议“继续”
2. 另一条规则建议“继续，但提醒先测试”

这两条在 `same_thread_prompt` lane 是可合并的，最终可以变成：

- “继续，但先读错误并注意测试。”

### 10.5 lane 级上限

为了避免一次事件把系统打成消息风暴，V1 建议：

1. 每个事件在每个 lane 最多只执行一条最终 action
2. 若多条 draft action 不能合并，就该 lane 整体失败
3. lane 失败不拖垮其他 lane

这意味着：

1. 同一次 turn terminal 可以同时：
   - 给当前 thread 发一条 prompt
   - 给当前飞书聊天发一条摘要
2. 但不会在同一个 lane 里无上限连发多条重复或互斥消息

### 10.6 冲突处理原则

冲突时不要猜。

V1 采用：

1. 该 lane 不执行
2. 给当前飞书窗口发一条短错误消息
3. 状态页保留最近一次冲突摘要即可，不需要专门做复杂持久化审计系统

这样虽然保守，但行为可预期。

## 11. 运行时 registry 与开关

### 11.1 registry 粒度

Autopilot registry 是实例级存储，但内容按工作区隔离。

每个工作区至少需要维护：

1. 当前已加载 spec
2. 最近一次 source hash
3. 最近一次成功 load 时间
4. workspace 总开关
5. 各 `entry_id` 的启用状态
6. 如有 schedule entry，则维护当前工作区的 `runner contract`

### 11.2 开关覆盖关系

规则应为：

1. workspace `off`
   - 该工作区所有 entry 都不参与匹配
2. workspace `on` 且某条 rule `off`
   - 其他 rule 可继续运行，该条跳过
3. load 新 spec 时
   - 同 id 的 rule 尽量继承旧开关状态
   - 已不存在的旧 rule 状态直接丢弃

这样用户就可以：

1. 临时关掉整套 Autopilot
2. 只关掉某一条太吵或有风险的规则

### 11.3 `runner contract`

这里需要把“Autopilot 用谁来做推理”单独收敛成一个运行时合同。

建议把它命名为当前工作区的 `runner contract`。

它至少应包含：

1. `backend`
2. `codex_provider_id` 或 `claude_profile_id`
3. 该 backend 启动 runner 所需的稳定运行时默认项

它不等于：

1. 原始 `Autopilot.md`
2. 编译后的 spec
3. 当前有没有 surface 正在 attached

它的用途是：

1. 给没有“当前 turn 来源实例”的事件一个稳定推理执行面
2. 避免 scheduler 触发时去猜“现在碰巧是谁在线”

### 11.4 不同事件怎样决定用谁推理

V1 应明确分成两条规则。

#### 11.4.1 `turn_terminal`

这类事件天然附着在某个刚结束的 turn 上。

但这里的 `turn_terminal` 不是指“任何 turn 的终态”。

V1 只接受：

1. 当前 surface 正在服务的前台主线 turn

V1 明确排除：

1. subagent 造成的 turn
2. `fork_ephemeral` / `start_ephemeral` 这类 detour / fork / 临时会话 turn
3. internal helper turn
4. 其他不属于当前前台主线的后台或旁路 turn

因此它应使用：

1. 该 turn 所属实例的 backend
2. 该实例当前真实使用的 provider/profile
3. 必要时再带上该 turn 已冻结的相关 prompt config

实现上，它可以：

1. 直接复用当前服务这个窗口的实例配置
2. 在可能时走更轻量的 helper 路径
3. 如果该 turn 对应的 workspace / surface 在事件真正处理时已经 `detach`，则这次 `turn_terminal` Autopilot 直接跳过，不再后台执行
4. 但无论怎样，都不应改写当前用户可见 thread

也就是说：

1. `turn_terminal` 是附着在当前交互上下文上的自动化
2. 它只对当前前台主线 turn 生效
3. 它不是“用户都已经 detach 了还继续在后台补跑”的工作区级守护逻辑

#### 11.4.2 `schedule_tick`

这类事件不应该去看“当前是否还有 surface attached”。

它必须使用当前工作区已持久化的 `runner contract`。

也就是说：

1. `schedule_tick` 不是看“现在谁在线”
2. 也不是看“最近一次是谁跑过这个工作区”
3. 而是看“这个工作区最后一次成功 load 时绑定下来的 runner 是谁”

### 11.5 `runner contract` 的绑定与替换

V1 建议把 `runner contract` 的生命周期定成：

1. 当 `/autopilot load` 成功且 spec 中包含 `schedule_tick` entry：
   - 如果当前 workspace 有明确 attached surface / backend contract，则把它冻结成新的 `runner contract`
2. 如果当前 load 时没有 attached surface：
   - 但当前工作区已经存在旧 `runner contract`，则继续沿用旧值
3. 如果当前 load 时没有 attached surface：
   - 且工作区从未绑定过 `runner contract`，则这次 load 失败
4. 以后即使所有 surface 都 `/detach`：
   - 已绑定的 `runner contract` 仍然继续有效
5. 只有下一次成功 load：
   - 才允许把 `runner contract` 替换成新的 backend/provider/profile

这样 scheduler 的行为就是稳定可预测的。

### 11.6 状态页应展示 `runner contract`

如果当前工作区已加载的 spec 中包含 schedule entry，`/autopilot status` 应额外展示：

1. 当前 `runner contract` 使用的 backend
2. 当前 provider/profile 标识
3. 它是什么时候绑定的
4. 它是否来自当前这次 load 还是沿用了旧值

## 12. 调度模型

### 12.1 Cron 的用户写法迁到 Autopilot

`/cron` 当前最难用的部分，是要求用户去维护一张结构化表。

V1 的方向是：

1. 用户在 `Autopilot.md` 写自然语言调度意图
2. 编译器把它翻译成 `schedule_tick` entries
3. `/autopilot load` 成功时，为这些 schedule entry 绑定当前工作区的 `runner contract`
4. 定时触发后，由这个 `runner contract` 决定用哪个 backend/provider/profile 拉起执行 turn
5. 如果这个 turn 需要在完成后通知外部，就通过它被授予的 MCP / tool 能力自己完成
6. 运行时仍复用 daemon 现有 scheduler 与 hidden run substrate

也就是说：

1. 用户体验层可以迁到 Autopilot
2. 但底层 scheduler、job registry、hidden run、workspace/Git materialization 这些实例级基础设施仍然保留

所以 Autopilot 可以覆盖 `/cron` 的配置体验，但不是把 scheduler 底层全部删掉。

### 12.2 V1 schedule 收敛

V1 只建议支持当前 scheduler 已经比较稳定的几类表达：

1. `daily_at`
2. `interval`

如果 `Autopilot.md` 写出后端无法稳定理解的时间表达：

1. 编译器可以尝试归一化
2. 仍不能落地时，`load` 直接失败

这是可以接受的，因为“拒绝一个说不清楚的时间点”比“偷偷理解错并在错误时间执行”更安全。

## 13. 与现有功能的映射

### 13.1 覆盖 `/cron`

Autopilot 可以覆盖 `/cron` 的主要用户需求：

1. 按工作区写定时规则
2. 定时起新 thread
3. 定时起的 thread 可在执行过程中通过被授予的能力给 workspace 绑定 surface 发消息
4. 未来也可定时触发更多固定 action
5. 在所有 surface 都 detach 之后，仍按已绑定的 `runner contract` 继续运行

不能直接消失的是：

1. scheduler tick
2. hidden run
3. 任务注册表
4. 运行期持久化

因此正确说法是：

Autopilot 替换 `/cron` 的配置入口与表达方式，不替换其全部运行时基础设施。

### 13.2 覆盖 `autocontinue`

Autopilot 可以覆盖 `autocontinue` 的“是否要继续”和“继续时要说什么”这两层决策。

典型 entry：

1. 事件：`turn_terminal`
2. source：终态 + final message + 最近 thread 历史
3. action：`send_prompt_same_thread`

这允许系统不再只靠硬编码错误族或固定文案来补发“继续”。

### 13.3 覆盖 `autowhip`

Autopilot 也可以覆盖 `autowhip` 的“输出看起来没做完、太粗糙、或还该继续催一轮”的决策层。

它与 `autocontinue` 的差别，不一定需要继续体现在两个完全独立的用户功能名上；在 Autopilot 里，它们都只是不同的 `turn_terminal` entries。

### 13.4 当前 thread 长摘要回飞书

这是 V1 的一个非常适合的场景：

1. 事件：`turn_terminal`
2. source：完整 thread
3. action：`post_feishu_message`

这允许系统在某些模型 final message 很粗糙时，再额外给用户发一条更适合人看的摘要，文本或卡片都可以。

## 14. 为什么 `bind to my view` 不放进 V1

`bind to my view` 类需求虽然也需要模型推理，但它更像一个“交互式修补/回写事务”，不是一个纯粹的被动事件自动化。

它通常要求：

1. 读取当前 thread 或当前 view
2. 判断模型是不是在拒绝/偏航
3. 推理应该怎样改
4. 回写当前 thread 或当前 view
5. 处理中间可能还要有人机确认

这和 Autopilot V1 的事件模型不一样：

1. Autopilot V1 更像 `event -> collect sources -> produce actions`
2. `bind to my view` 更像 `interactive repair transaction`

所以更合理的路径是：

1. V1 不把它直接塞进 Autopilot
2. 未来可复用 Autopilot 的一部分基础件：
   - source 抽象
   - 编译/改写提示词思路
   - 某些 action 壳
3. 但产品入口和事务模型仍单独设计

## 15. 实现建议

建议拆成三期：

### 15.1 第一阶段：只做编译与状态展示

目标：

1. 支持 `Autopilot.md`
2. 支持 `/autopilot load`
3. 支持 `/autopilot status`
4. 支持 workspace/rule 两级开关
5. 先把编译结果稳定存起来

这一阶段就能回答最关键的问题：

1. 模型能否把用户自然语言稳定编译成受支持 JSON
2. 现有能力矩阵是否足够

### 15.2 第二阶段：接 turn terminal

目标：

1. 先接 `turn_terminal`
2. 先支持 `same_thread_prompt`
3. 再支持 `feishu_message`

这样能先覆盖：

1. `autocontinue`
2. `autowhip`
3. turn 长摘要回飞书

### 15.3 第三阶段：接 scheduler

目标：

1. 支持 `schedule_tick`
2. 落地 workspace 级 `runner contract`
3. 复用现有 scheduler / hidden run substrate
4. 逐步把 `/cron` 的用户入口迁到 `/autopilot`

## 16. 待确认问题

当前仍建议在实现前再确认三件事：

1. `send_prompt_new_thread` 的 thread 命名、可见性和默认标题是否需要产品约定
2. `Autopilot.md` 中若同一段文字同时隐含多个事件，编译器是拆 entry 还是拒绝并要求改写
3. schedule entry 若没有可用通知目标，执行 turn 内的通知能力应该静默失败还是显式报 notice

如果按当前讨论继续推进，默认建议是：

1. `capability_profile` 首版只开放少量固定画像，不暴露任意 MCP 拼装
2. 新 thread 命名沿用现有默认命名
3. 编译器优先拆 entry，拆不清再拒绝

## 17. 结论

Autopilot V1 的正确方向，不是“再做一套更复杂的规则系统”，而是：

1. 让用户继续写自然语言
2. 让模型在 `load` 阶段把它编译成机器可执行 JSON
3. 让后端只执行少数固定 event/source/action 壳
4. 把 `Autopilot` 自己收敛成 one-off、无工具的决策层
5. 把复杂后续行为交给被拉起的 turn 和它被授予的能力去完成
6. 把 `turn_terminal` 和 `schedule_tick` 的 runner 来源明确分开：前者跟随事件源实例，后者跟随工作区持久化 `runner contract`
7. 用显式 load、workspace/rule 两级开关、单 action 顶层结果、lane 合并与冲突拒绝，把不确定性关进一个可控边界里

如果这条线成立，它就有机会逐步吃掉：

1. `/cron` 的配置体验
2. `autocontinue` 的硬编码决策
3. `autowhip` 的硬编码决策

同时又不需要在 V1 一开始就把所有复杂交互能力都塞进来。
