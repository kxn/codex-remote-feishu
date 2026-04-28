# Claude CLI Black-Box Test Plan

> Type: `draft`
> Updated: `2026-04-28`
> Summary: 为 `#495`、`#497`、`#498` 之前的 Claude CLI 黑盒实测定义固定场景、固定 payload、固定调用方式与固定采样产物，避免边测边改方案。

## 1. 目的

这份方案不是实现文档，而是 **实测前置设计文档**。

目的只有一个：

1. 在真正开始 `#495`、`#497`、`#498` 之前，先用真实 Claude CLI 采样当前行为，把协议与会话语义从“读上游代码推断”提升到“本机真实行为证据”。

本轮黑盒测试的产物必须同时服务三张 pre-MVP 子单：

1. `#495`
   - runtime host seam
   - completed / interrupt / runtime-exit reconciliation
2. `#497`
   - live transport semantic mapper
   - `stream-json -> canonical turn/item/request/progress`
3. `#498`
   - session catalog / history / resume plane
   - transcript metadata / session persistence / cwd 语义

## 2. 这轮要回答的问题

### 2.1 live transport

要实测确认：

1. `system:init` 的真实时机、字段形状、是否稳定先于首个 turn 边界。
2. `message_start`、`assistant`、`stream_event`、`result` 的实际出现顺序。
3. `--include-partial-messages` 下，Claude 是否同时输出 partial `assistant` 消息和 `stream_event`。
4. `control_request` 的真实 subtype / payload 形状：
   - `can_use_tool`
   - `AskUserQuestion`
   - `ExitPlanMode`
5. `control_response` 回写后，CLI 如何恢复继续执行。
6. `interrupt` 后是否一定收到 `result`，还是可能直接 EOF / process exit。
7. 已有 partial text 时，终态若为 error，stdout 上到底怎样收口。

### 2.2 session plane

要实测确认：

1. `session_id` 的真实来源与稳定性。
2. transcript / session file 的落盘位置与命名规则。
3. resume 在相同 cwd 与不同 cwd 下的真实行为。
4. `--fork-session` 是否复用旧 transcript、是否生成新 `session_id`。
5. transcript 中 user / assistant / tool / system / sidechain 记录的大致形状。
6. workspace 维度是否直接体现在 transcript 位置、metadata 或 init 消息中。

## 3. 非目标

本轮明确不做：

1. 不验证 Feishu 展现。
2. 不验证本仓库当前 wrapper 的实现正确性。
3. 不在黑盒轮次里拍板 MVP visible 命令集。
4. 不把 `/compact`、`/review`、`/skills`、`/mode` 之类产品入口都拉进第一轮。
5. 不把“真实 CLI 行为”与“我们后续应该怎样映射”为一件事；先采样，再做映射结论。

## 4. 执行纪律

### 4.1 固定输入，不边测边改

每个场景在开始执行前必须先固定：

1. workdir
2. CLI flags
3. stdin payload
4. 若需要 reactive response，则固定 response 模板
5. 成功 gate
6. 需要记录的观察点

执行中允许：

1. 记录“与预期不符”的真实输出
2. 标记某场景失败或未触发

执行中不允许：

1. 临时改 prompt 再继续跑但不留痕
2. 临时换 flag 但不记为新场景
3. 临时改成功标准

### 4.2 secrets 本地化

认证信息只保留在本地私有文件中，不写入仓库文档。

当前机器已准备的本地 env bootstrap：

- `.codex/private/claude-blackbox-10_10_10_8.env.sh`

仓库内文档只引用“加载本地 env bootstrap”这一动作，不记录密钥值。

### 4.3 固定采样产物

每个 run 必须至少产出：

1. `meta.json`
   - 场景 ID
   - 时间
   - cwd
   - flags
   - 期望 gate
2. `stdin.ndjson`
3. `stdout.ndjson`
4. `stderr.log`
5. `observations.md`
   - 是否过 gate
   - 原始异常点
   - 需要回卷到 `#495/#497/#498` 的结论
6. `session-files/`
   - 按需复制相关 transcript / metadata 文件副本

推荐统一落到本地私有目录：

- `.codex/private/claude-blackbox-runs/<date>/<scenario-id>/`

## 5. 调用模型

本轮只使用两种固定 harness 模式。

### 5.1 模式 A：one-shot static stdin

适用：

1. 纯文本 turn
2. same-session 多轮
3. resume / fork
4. transcript 落盘观察

固定命令模板：

```bash
source .codex/private/claude-blackbox-10_10_10_8.env.sh

cd "$WORKDIR"

claude -p \
  --input-format stream-json \
  --output-format stream-json \
  --include-partial-messages \
  --replay-user-messages \
  --verbose \
  ...scenario-specific-flags... \
  < "$RUN_DIR/stdin.ndjson" \
  > "$RUN_DIR/stdout.ndjson" \
  2> "$RUN_DIR/stderr.log"
```

通用约束：

1. 不使用 `--no-session-persistence`。
2. 除非场景明确要求，否则不加 `--model`，由真实 backend 返回 `system:init.model` 作为记录值。
3. 除非场景明确需要工具，否则用 `--tools ""` 关闭 built-in tools，降低噪声。

### 5.2 模式 B：driver-mode reactive stdin

适用：

1. `can_use_tool`
2. `AskUserQuestion`
3. `ExitPlanMode`
4. 协议内 `interrupt`
5. process kill / EOF without result

固定 driver 责任：

1. 启动 Claude 子进程并记录 PID。
2. 逐行读取 stdout，原样写入 `stdout.ndjson`。
3. 在写任何 `control_response` 前，先把触发它的 `control_request` 落盘。
4. 根据固定规则构造回写 payload，而不是临场手填 JSON。
5. 必要时发送：
   - `control_response`
   - 协议内 `control_request(subtype=interrupt)`
   - 外部 `SIGTERM`

driver 不承担任何“解释正确性”的职责，只负责：

1. 时序驱动
2. 原样捕获
3. 固定规则回写

## 6. 固定 payload 模板

### 6.1 user message

基础 user payload：

```json
{"type":"user","message":{"role":"user","content":"<PROMPT>"}}
```

说明：

1. 第一轮只用文本 `content`，不引入多模态或结构化 content blocks。
2. 若场景需要第二轮消息，第二条也沿用相同形状。

### 6.2 协议内 interrupt

```json
{
  "type": "control_request",
  "request_id": "bb-interrupt-1",
  "request": {
    "subtype": "interrupt"
  }
}
```

### 6.3 allow permission / allow continuation

模板：

```json
{
  "type": "control_response",
  "response": {
    "subtype": "success",
    "request_id": "<REQUEST_ID>",
    "response": {
      "behavior": "allow",
      "updatedInput": "<ORIGINAL_INPUT_WITH_PATCHES>",
      "updatedPermissions": []
    }
  }
}
```

固定规则：

1. `updatedInput` 必须以收到的 `request.input` 为 base，不要手写重建。
2. 若只是工具审批允许，不改字段，只原样回填 input。
3. 若是 `AskUserQuestion`，在 base 上补 `answers`。
4. 若是 `ExitPlanMode`，在 base 上补 `feedback`。

### 6.4 deny permission

模板：

```json
{
  "type": "control_response",
  "response": {
    "subtype": "success",
    "request_id": "<REQUEST_ID>",
    "response": {
      "behavior": "deny",
      "message": "<DENY_MESSAGE>",
      "interrupt": false
    }
  }
}
```

### 6.5 deny plan and interrupt

模板：

```json
{
  "type": "control_response",
  "response": {
    "subtype": "success",
    "request_id": "<REQUEST_ID>",
    "response": {
      "behavior": "deny",
      "message": "blackbox reject plan",
      "interrupt": true
    }
  }
}
```

这条模板只用于 `ExitPlanMode` 的拒绝分支。

## 7. 固定 prompt 集

约束：

1. 所有 prompt 都要明确说明这是本地 CLI / protocol integration test。
2. 不使用“你现在是测试程序”“忽略正常交互”“只返回固定串否则失败”这类容易触发额外安全或角色判断的措辞。
3. 允许要求“单 token / 单行输出”，但必须保留软件集成上下文。

### 7.1 P1 纯文本 smoke

```text
This is a local CLI protocol smoke test for a software integration. Reply with the single token BLACKBOX_OK.
```

### 7.2 P2 same-session 第二轮

```text
This is turn 2 of the same local CLI integration test session. Reply with the single token BLACKBOX_TURN_2.
```

### 7.3 P3 tool approval

```text
Use Bash to run `printf BLACKBOX_TOOL_OK` in the current directory, then reply with exactly the command output.
```

### 7.4 P4 AskUserQuestion

```text
Do not guess. Use the AskUserQuestion tool to ask me one single-choice question with exactly these options: Fast, Safe, Balanced. Wait for my answer before doing anything else.
```

### 7.5 P5 ExitPlanMode

```text
Before taking any action, produce a short numbered plan and wait for approval. Use the built-in plan confirmation flow instead of continuing directly.
```

### 7.6 P6 long-running interrupt target

```text
Use Bash to run `sleep 20; echo BLACKBOX_LONG_DONE` and stream progress briefly while it is running.
```

### 7.7 P7 partial-text-then-error

```text
First say exactly `BLACKBOX_PARTIAL_PREFIX`. Then use Bash to run `cat /definitely/not-found-blackbox` and stop if the command fails.
```

### 7.8 P8 transcript / resume seed

```text
This message seeds a local session resume integration test. Reply with the single token BLACKBOX_SESSION_SEED.
```

### 7.9 P9 fork verification

```text
This message verifies a forked child session in a local CLI integration test. Reply with the single token BLACKBOX_FORK_CHILD.
```

## 8. 场景矩阵

每个场景同时定义：

1. fixed input
2. fixed invocation
3. hard gate
4. 需要记录的开放问题

### 8.1 BB-01 minimal text turn

目标：

1. 确认最小可运行链路。
2. 记录 `system:init`、首个 turn 边界和 terminal `result` 的顺序。

输入：

1. `P1`
2. `stdin.ndjson` 只包含一条 user payload

调用：

1. 模式 A
2. flags：
   - `--tools ""`

hard gate：

1. stdout 中出现非空 `session_id`
2. stdout 中出现至少一个 terminal `result`
3. 结果不要求完全等于上游假设顺序，但必须能闭成一轮

重点观察：

1. `system:init` 是否早于 `message_start`
2. 是否同时出现 `assistant` 和 `stream_event`
3. `result.is_error`、`usage`、`modelUsage` 的真实字段形状

下游消费者：

1. `#497`

### 8.2 BB-02 same-process second turn

目标：

1. 确认同一进程内第二条 user message 的 turn 边界。
2. 看 turn 是否严格串行、是否复用同一 `session_id`。

输入：

1. 先发 `P1`
2. 观察第一轮 terminal 后，再发 `P2`

调用：

1. 模式 B
2. flags：
   - `--tools ""`

hard gate：

1. 两轮都出现 terminal `result`
2. `session_id` 稳定不变

重点观察：

1. 第二轮开始前是否再次发送 `system:init`
2. `message_start` 与第二轮 user replay 的对应关系

下游消费者：

1. `#497`
2. `#498`

### 8.3 BB-03 resume in same cwd

目标：

1. 确认 `--resume <session_id>` 的真实行为。
2. 确认 resumed run 是否复用原 `session_id`。

输入：

1. 先用 `P8` 建一个种子 session
2. 第二次运行改用 `--resume <captured_session_id>`，再发 `P2`

调用：

1. 模式 A
2. flags：
   - 首轮：`--tools ""`
   - 次轮：`--tools "" --resume <session_id>`

hard gate：

1. resume 运行成功进入新一轮输出
2. stdout 中可以确认 resumed session 与原 session 的关系

重点观察：

1. resumed run 的 `system:init.session_id` 是否等于原 session
2. transcript 文件是否继续 append，而不是新建

下游消费者：

1. `#498`

### 8.4 BB-04 resume in different cwd

目标：

1. 实测 resume 是否强依赖原 cwd。
2. 实测 resumed run 的 `init.cwd`、transcript 归属和失败模式。

输入：

1. 在 `ws-a` 用 `P8` 建 session
2. 在 `ws-b` 用 `--resume <same-session-id>` 再发 `P2`

调用：

1. 模式 A
2. flags：
   - `--tools "" --resume <session_id>`

hard gate：

1. 成功或失败都算结果，但必须完整落盘 stdout/stderr

重点观察：

1. CLI 是直接成功、直接失败，还是进入奇怪半恢复态
2. `system:init.cwd` 是否变为新 cwd
3. transcript 继续写原目录、写新目录，还是无法写

下游消费者：

1. `#498`

### 8.5 BB-05 fork-session from existing session

目标：

1. 确认 `--resume <session_id> --fork-session` 是否生成新的 `session_id`。
2. 确认 fork 后 transcript 是新文件、旧文件 append，还是别的共享形态。

输入：

1. 先用 `P8` 建一个种子 session
2. 再用 `--resume <parent-session-id> --fork-session` 发送 `P9`

调用：

1. 模式 A
2. flags：
   - 首轮：`--tools ""`
   - 次轮：`--tools "" --resume <session_id> --fork-session`

hard gate：

1. fork run 成功进入新一轮输出
2. stdout 中可以确认 parent / child session 的区分

重点观察：

1. child run 的 `system:init.session_id` 是否不同于 parent
2. child transcript 是否独立落盘
3. child 是否仍可看到 parent 上下文

下游消费者：

1. `#498`

### 8.6 BB-06 can_use_tool allow

目标：

1. 获取真实 `control_request(subtype=can_use_tool)` payload。
2. 验证 allow response 后 CLI 如何继续。

输入：

1. 首条 user message 用 `P3`
2. 收到 `can_use_tool` 后，按 6.3 模板 allow

调用：

1. 模式 B
2. flags：
   - `--tools Bash`
   - `--permission-mode default`

hard gate：

1. stdout 中出现 `control_request`
2. allow 后 CLI 继续执行，并最终出现 terminal `result`

重点观察：

1. `request.tool_name`
2. `request.input`
3. `blocked_path` / `permission_suggestions` 是否稳定出现
4. allow 后是否再发额外 ack / continuation 信号

下游消费者：

1. `#497`

### 8.7 BB-07 can_use_tool deny

目标：

1. 获取 deny 分支的真实 CLI 后续行为。
2. 验证 deny 后是 terminal failure、继续追问，还是别的收口。

输入：

1. 首条 user message用 `P3`
2. 收到 `can_use_tool` 后，按 6.4 模板 deny

调用：

1. 模式 B
2. flags：
   - `--tools Bash`
   - `--permission-mode default`

hard gate：

1. stdout 中出现 `control_request`
2. deny 后行为完整落盘

重点观察：

1. deny 后是否立即 `result`
2. deny 是否还伴随 assistant 文本解释
3. deny 是否污染后续同进程第二轮

下游消费者：

1. `#497`
2. `#495`

### 8.8 BB-08 AskUserQuestion

目标：

1. 获取真实 `AskUserQuestion` payload 形状。
2. 验证 answers 回写后的继续执行语义。

输入：

1. 首条 user message 用 `P4`
2. 收到 `AskUserQuestion` 后，用 allow 模板回写：
   - 在 `updatedInput` 上补 `answers`
   - 固定答案：`Fast`

调用：

1. 模式 B
2. flags：
   - `--tools ""`

hard gate：

1. 出现 tool_name=`AskUserQuestion` 的 `control_request`
2. answers 回写后 CLI 继续执行并收口

重点观察：

1. `questions` 数组形状
2. option label / multiSelect 字段
3. 回写后是否有额外 assistant 过渡文本

下游消费者：

1. `#497`

### 8.9 BB-09 ExitPlanMode allow and deny

目标：

1. 获取真实 `ExitPlanMode` payload 形状。
2. 区分 approve 与 reject+interrupt 两条分支。

输入：

1. 首条 user message 用 `P5`
2. 子场景 A：
   - 回写 allow
   - 在 `updatedInput` 上补 `feedback="Approved. Execute the plan."`
3. 子场景 B：
   - 回写 deny
   - `interrupt=true`

调用：

1. 模式 B
2. flags：
   - `--permission-mode plan`

hard gate：

1. 至少拿到一条 tool_name=`ExitPlanMode` 的 `control_request`
2. allow / deny 两条分支都分别有完整 stdout/stderr

重点观察：

1. `plan`
2. `allowedPrompts`
3. deny + interrupt 后是怎样的 terminal 收口

下游消费者：

1. `#497`
2. `#495`

### 8.10 BB-10 protocol interrupt on active turn

目标：

1. 验证协议内 `interrupt` 的真实时序。
2. 记录 interrupt 后 stdout 上是否一定有 terminal `result`。

输入：

1. 首条 user message 用 `P6`
2. 在出现 turn started 之后，由 driver 发送 6.2 的 interrupt payload

调用：

1. 模式 B
2. flags：
   - `--tools Bash`
   - `--dangerously-skip-permissions`

hard gate：

1. 成功发出 interrupt
2. 进程后续行为完整落盘

重点观察：

1. interrupt 后有没有明确 `result`
2. 若有 `result`，`is_error` 和 `result` 文本是什么
3. 若没有 `result`，是否直接 EOF / exit

下游消费者：

1. `#495`
2. `#497`

### 8.11 BB-11 partial text then terminal error

目标：

1. 实测“已有文本输出，但终态失败”的真实原始行为。
2. 给 final-output materialization contract 提供证据。

输入：

1. 首条 user message 用 `P7`
2. 工具审批一律 allow

调用：

1. 模式 B
2. flags：
   - `--tools Bash`
   - `--permission-mode default`

hard gate：

1. stdout 中先出现 assistant text / partial text
2. 随后出现明显 error terminal 或 process failure

重点观察：

1. 失败前是否已经输出完整 assistant 前缀
2. terminal `result.is_error` 与已有 text 的组合方式
3. stderr 是否包含更强错误信息

下游消费者：

1. `#495`
2. `#497`

### 8.12 BB-12 process kill / stdout EOF without result

目标：

1. 直接验证 runtime-exit / EOF 场景，不依赖模型自行报 completed。
2. 给 completed reconciliation 提供黑盒证据。

输入：

1. 首条 user message 用 `P6`
2. 在 turn started 且确认进程仍运行时，对 Claude PID 发送 `SIGTERM`

调用：

1. 模式 B
2. flags：
   - `--tools Bash`
   - `--dangerously-skip-permissions`

hard gate：

1. 成功记录到进程 PID
2. 成功外部杀死进程
3. 完整记录 stdout 末尾、stderr 末尾与退出码

重点观察：

1. 是否完全没有 terminal `result`
2. EOF 前是否已输出 partial text
3. exit code / stderr 是否稳定

下游消费者：

1. `#495`

### 8.13 BB-13 transcript and workspace audit

目标：

1. 确认 session file 的实际布局与最小解析字段。
2. 为 `threads.refresh` / `thread.history.read` 设计提供基础样本。

输入：

1. 在 `ws-a` 执行：
   - `BB-01`
   - `BB-03`
2. 在 `ws-b` 执行：
   - `BB-04`
3. 复制相关 session 文件到 `session-files/`

调用：

1. 模式 A

hard gate：

1. 找到至少一份与 `session_id` 对应的 session file
2. 文件中能识别最小 user / assistant / system 记录

重点观察：

1. session file 路径与 cwd 的关系
2. `system` 记录中的 `cwd`、`session_id`
3. user / assistant / tool_use / tool_result 的行级形状
4. sidechain / current-turn 标记是否存在

下游消费者：

1. `#498`

## 9. 期待结果的分类

为了避免在“还没测”的阶段就假装知道所有细节，本方案把预期分成两层。

### 9.1 hard gate

这些是执行场景前就必须写死的：

1. 这一轮是否成功拿到了目标类型的原始样本
2. 进程是否完整落盘 stdout/stderr
3. 该场景是否对某个 issue 的 closure 有用

### 9.2 behavior questions

这些不是先验真理，而是要通过黑盒确认的开放行为：

1. `system:init` 与 turn 边界顺序
2. partial `assistant` 与 `stream_event` 的共存方式
3. deny / interrupt / error 后的真实 terminal 收口
4. resume 的 cwd 约束
5. transcript 的最小字段集

执行后必须把这层答案沉淀回文档或 issue，而不是只留在聊天里。

## 10. 推荐执行顺序

黑盒轮次内部建议按下面顺序跑：

1. `BB-01`
2. `BB-02`
3. `BB-03`
4. `BB-04`
5. `BB-05`
6. `BB-06`
7. `BB-07`
8. `BB-08`
9. `BB-09`
10. `BB-10`
11. `BB-11`
12. `BB-12`
13. `BB-13`

原因：

1. 先拿最便宜的基础协议形状。
2. 再拿 permission / question / plan 这些 request 类样本。
3. 最后才进 interrupt / EOF / error 这类更脏的生命周期样本。
4. transcript / workspace 审计压轴做，便于一次性复制前面 run 已生成的 session files。

## 11. 黑盒轮次完成后的回卷要求

跑完以后，结果不能只留在本地文件夹里。

至少要回卷三类 durable 结论：

1. `#495`
   - interrupt / EOF / result 缺失时的 host-side reconciliation 要求
2. `#497`
   - native message / control request 到 canonical event/request 的 mapping matrix
3. `#498`
   - session file / history / resume / cwd contract

若黑盒结果直接推翻当前 `docs/inprogress/claude-backend-integration-plan.md` 的某些假设，必须先修正文档，再开始任何实现。
