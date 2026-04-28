# Claude CLI Black-Box Findings 2026-04-28

> Type: `draft`
> Updated: `2026-04-28`
> Summary: 基于本机真实 Claude CLI (`2.1.37`) 与内网 relay (`http://10.10.10.8:8080`) 的黑盒采样结果，收敛 `#495`、`#497`、`#498` 前必须修正的协议认知与拆单前提。

## 1. 测试基线

- 真实 CLI:
  - `claude --version = 2.1.37`
  - `CLAUDE_BIN` 需显式指向交互 shell 可见的 Claude 可执行文件
- 真实 backend:
  - `ANTHROPIC_BASE_URL=http://10.10.10.8:8080`
  - `ANTHROPIC_AUTH_TOKEN=<local-only>`
  - `ANTHROPIC_MODEL=mimo-v2.5-pro`
  - `ANTHROPIC_SMALL_FAST_MODEL=mimo-v2.5-pro`
- 本轮固定 harness:
  - 方案文档: `docs/draft/claude-cli-blackbox-test-plan.md`
  - 本地 env: `.codex/private/claude-blackbox-10_10_10_8.env.sh`
  - 本地 runner: `.codex/private/claude_blackbox_runner.py`
- 关键修正:
  - 不能假设非交互 shell 上有 `claude`，必须显式带 `CLAUDE_BIN`
  - 不能按裸 stdin prompt 模式采样，必须先发 `initialize`
  - 任何要走 approval / user-interaction bridge 的场景，都必须加 `--permission-prompt-tool stdio`

## 2. 场景收口

| 场景 | 状态 | 关键结论 |
| --- | --- | --- |
| `BB-01` | pass | 基础单轮可稳定收到 `system:init` + `result` |
| `BB-02` | pass | 同进程第二轮前会再次出现 `system:init` |
| `BB-03` | pass | same-cwd `--resume` 可用 |
| `BB-04` | fail / expected finding | cross-cwd `--resume` 返回 `No conversation found with session ID`，会话按 workspace/cwd 落盘 |
| `BB-05` | pass | `--fork-session` 生成新 `session_id` |
| `BB-06` | pass | `can_use_tool` allow 路径成立 |
| `BB-07` | pass | `can_use_tool` deny 路径成立 |
| `BB-08` | pass after harness fix | `AskUserQuestion` 必须带 `--permission-prompt-tool stdio` 才会桥接成外部可响应事件 |
| `BB-09` | partial | `ExitPlanMode` 同样依赖 `--permission-prompt-tool stdio`；deny 样本已稳定，allow 样本会出现重复 reissue |
| `BB-10` | pass | 协议内 `interrupt` 后仍可能收到终态 `result` |
| `BB-11` | pass | 已有 partial text 后，即使后续工具失败也会继续收口 |
| `BB-12` | pass | 外部 `SIGTERM` 为进程级中断，退出码 `143`，无最终 `result` |
| `BB-13` | pass | transcript 真实包含 `user / assistant / tool_use / tool_result / queue-operation` 等记录 |

## 3. 被推翻的旧假设

### 3.1 `AskUserQuestion` 不是“直接来的 control_request”

真实顺序见:

- `BB-08 stdout.ndjson`（local-only sample）

实际行为是两段式:

1. assistant 先发 `tool_use(name=AskUserQuestion, input.questions=...)`
2. 只有在加了 `--permission-prompt-tool stdio` 后，CLI 才继续发:
   - `control_request subtype=can_use_tool`
   - `tool_name=AskUserQuestion`
   - `input.questions=...`

结论:

- 外部 bridge 看到的 `control_request` 不是独立的“request user input 原语”
- 它是 `AskUserQuestion` 这个 tool 在 CLI 内部 permission/interaction 层上的可外接审批点

### 3.2 `ExitPlanMode` 也不是“携带 plan payload 的 control_request”

真实 deny-only 样本见:

- `BB-09-deny-only stdout.ndjson`（local-only sample）

实际顺序:

1. assistant 发 `tool_use(name=ExitPlanMode, input={})`
2. CLI 发 `control_request subtype=can_use_tool`
3. `tool_name=ExitPlanMode`
4. `input={}`，没有 plan 正文，没有 `allowedPrompts`，没有 `feedback`

deny 回写后，CLI 继续生成:

- `user.tool_result(is_error=true, content="blackbox reject plan")`
- `user.text = "[Request interrupted by user for tool use]"`
- 终态 `result subtype=error_during_execution`

结论:

- 当前外部 control bridge 只看得到“是否允许调用 ExitPlanMode”
- 看不到 plan 正文
- 计划正文不能依赖 `control_request.request.input` 获取

### 3.3 `ExitPlanMode` 的 plan 正文来自多 carrier，不是单一路径

在更完整的 allow 样本中，真实顺序见:

- `BB-09 allow stdout.ndjson`（local-only sample）

实际观察到三件事同时成立:

1. plan 正文先以 assistant 普通文本出现
2. 随后的 `ExitPlanMode` tool use 仍然是 `input={}`
3. allow 后生成的 `user.tool_result` 会额外带:
   - `tool_use_result.filePath=<claude-plan-file>`

这意味着:

- plan 展示主来源首先是 assistant 已输出文本
- `control_request(can_use_tool)` 只负责交互审批，不承载 plan 正文
- 若需要更稳的 plan materialization，还要消费 allow 后的 `tool_use_result.filePath`，并把它当成 Claude 本地 plan 文件线索

因此，`ExitPlanMode` 的正确读取顺序不是：

1. `control_request.input.plan`

而应改成：

1. assistant 文本中的计划正文
2. `tool_use_result.filePath` 指向的 Claude plan 文件
3. `control_request` 里的 allow/deny + feedback 语义

### 3.3 不加 `--permission-prompt-tool stdio` 会把交互工具误判成“协议缺失”

未加该 flag 时:

- `AskUserQuestion` 只会看到 assistant `tool_use`
- CLI 会自己退化成 error tool result，典型内容是 `"Answer questions?"`

这不是“没有交互工具桥接能力”，而是 harness 没把 CLI 的 permission-prompt 通道接出来。

## 4. 新确认的稳定事实

### 4.1 assistant `tool_use` 与 `control_request can_use_tool` 会并存

对 `AskUserQuestion` 与 `ExitPlanMode` 都成立:

1. 先有 assistant `tool_use`
2. 再有 `control_request can_use_tool`

因此 mapper 不能只盯一种 carrier。

### 4.2 `result` 的 subtype / `is_error` 组合不能按直觉解读

在 `BB-09-deny-only` 中:

- `result subtype=error_during_execution`
- 但 `is_error=false`

因此 turn 终态不能只靠 `is_error` 一位判断。

### 4.3 Claude transcript 强绑定 cwd / workspace

`BB-04` 已证实:

- 同一 `session_id` 换 cwd 直接 `resume` 会失败
- transcript 文件位置按 workspace/cwd 路径分桶

这意味着 `#498` 不能把 Claude session 当作全局 thread id 平面来做。

### 4.4 `result` 之外，还要消费 `user.tool_result`

在 `BB-09 allow` 中，`user.tool_result` 不只是“工具已回写”的通知，它还带了额外结构:

- `tool_use_result.plan`
- `tool_use_result.isAgent`
- `tool_use_result.filePath`

目前本机样本里:

- `plan=null`
- `filePath` 有值

这再次说明：

1. 不能只消费 `assistant` 和 `control_request`
2. 也不能只看 `result`
3. Claude 交互工具的正确桥接至少要同时覆盖：
   - `assistant tool_use`
   - `control_request can_use_tool`
   - `user tool_result`
   - `result`

### 4.5 外部工具的真实完成点在 `user.tool_result`，不是 assistant `tool_use`

`BB-06` 已进一步证明：

- `assistant tool_use(Bash)` 只表示“Claude 决定要调用哪个工具，以及入参是什么”
- 真正的执行结果在后续 `user.tool_result` 里，样本中带了：
  - `stdout`
  - `stderr`
  - `interrupted`
  - `isImage`

因此 Claude live transport mapper 里：

1. `assistant tool_use` 更像 canonical `item.started`
2. `user.tool_result` 才更像 canonical `item.completed`
3. 如果继续像上游简化实现那样把 `assistant tool_use` 直接近似成 completed，会丢掉真实工具结果语义

## 5. 对 pre-MVP 子单的直接影响

### 5.1 对 `#495`

`#495` 不能再只定义“把 Claude 进程起起来 + 把 control_request 接出来”。

它至少还包含三层东西:

1. runtime host
2. assistant `tool_use` 可视/可缓存/可历史化
3. permission-prompt bridge

尤其是:

- `AskUserQuestion` / `ExitPlanMode` 的真正入口先是 assistant `tool_use`
- 但真正可外部响应的是后续 `can_use_tool`
- `ExitPlanMode` 的 plan 内容不在 control bridge 里

这说明 `#495` 内部还需要再拆。

### 5.2 对 `#497`

`#497` 的 semantic mapper 不能只做:

- `stream-json -> text/progress/result`

还必须覆盖:

1. assistant `tool_use`
2. `control_request can_use_tool`
3. `user.tool_result`
4. `result subtype/is_error` 的非直觉组合

否则 Claude 输出无法正确翻译成现有前端结构化内容。

### 5.3 对 `#498`

`#498` 的 session/history 平面需要明确:

1. `session_id` 只是 cwd-scoped resume handle
2. transcript 读取要按 Claude 本地落盘路径建索引
3. cross-workspace resume 必须显式 reject 或切换 workspace 后再 resume

## 6. 建议的拆单修正

这轮黑盒的**初步**印象确实像“还要再多拆一张交互工具桥接单”，因为：

1. `AskUserQuestion` / `ExitPlanMode` 都不是单一 carrier
2. `tool_use_result.filePath`、answers、stdout/stderr 这类 sidecar 很容易被遗漏
3. 如果把这部分继续混进 `#495`，范围一定会失真

但把黑盒结果再和当前 canonical carrier 对齐后，当前更稳的结论变成：

1. 不需要再单独新开一张 pre-MVP 子单
2. 这部分应明确并入 `#497`
3. 前提是 `#497` issue body 必须显式拥有：
   - `assistant tool_use <-> control_request <-> user.tool_result <-> result` 关联
   - `plan materialization` 的来源优先级
   - deny / interrupt / repeated reissue 的收口规则

原因：

1. `#494` 已经拥有 canonical request/reply contract
2. 这里剩下的是 Claude native 多 carrier 的**相关性与 materialization**，本质上就是 live transport semantic mapper 的职责
3. 再拆一张只会把同一条 `tool_use_id` 的状态拆散到多个 worker，验证面反而更差

## 7. 当前最重要的产品结论

下一步做 Claude pre-MVP 时，不能再假设:

- “interactive tools 就是 control_request”
- “plan approval 的计划正文会在 control bridge 里”
- “只要 permission bridge 打通，前端就能正确展示 Claude 的结构化交互”

这三条现在都已经被本机实测否定。

真正更稳的前提应该是:

1. live transport 要同时消费 assistant/tool_use + control_request + user/tool_result + result
2. plan/question 可视化不能只依赖 request bridge
3. `#497` 必须显式拥有交互工具桥接与 plan materialization 的 mapper 闭包
